package variable

import (
	"bytes"
	"fmt"
	"github.com/bytedance/sonic"
	"log/slog"
	"sort"
	"sync"

	lferrors "loopforge/pkg/errors"
)

// VarEntry is one variable slot with metadata.
type VarEntry struct {
	Value       any
	Description string
	Visitable   bool
}

// VarStore is a thread-safe variable bag for one agent run (or session).
type VarStore struct {
	mu   sync.RWMutex
	vars map[string]*VarEntry
}

// New returns an empty VarStore.
func New() *VarStore {
	return &VarStore{vars: make(map[string]*VarEntry)}
}

// setConfig carries optional fields for Set/Define.
type setConfig struct {
	visitable   *bool
	description *string
}

// SetOption configures Set or Define.
type SetOption func(*setConfig)

// WithVisitable sets the Visitable flag explicitly.
func WithVisitable(v bool) SetOption {
	return func(c *setConfig) {
		c.visitable = &v
	}
}

// WithDescription sets the description text.
func WithDescription(d string) SetOption {
	return func(c *setConfig) {
		c.description = &d
	}
}

func applySetOpts(e *VarEntry, opts []SetOption) {
	var c setConfig
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	if c.visitable != nil {
		e.Visitable = *c.visitable
	}
	if c.description != nil {
		e.Description = *c.description
	}
}

// Define declares a key with Value=nil (shows as <unset> when visitable).
func (s *VarStore) Define(key, description string, opts ...SetOption) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := &VarEntry{Description: description, Visitable: true}
	applySetOpts(e, opts)
	s.vars[key] = e
}

// Set assigns value to key (creates entry if missing). Default visitable=true.
func (s *VarStore) Set(key string, value any, opts ...SetOption) {
	if key == "" {
		return
	}
	slog.Debug("VarStore.Set wait lock", "key", key)
	s.mu.Lock()
	slog.Debug("VarStore.Set acquired lock", "key", key)
	defer s.mu.Unlock()
	e, ok := s.vars[key]
	if !ok {
		e = &VarEntry{Visitable: true}
		s.vars[key] = e
	}
	e.Value = value
	applySetOpts(e, opts)
}

// AgentSet is like Set but refuses const_ keys (for var_set).
func (s *VarStore) AgentSet(key string, value any, opts ...SetOption) error {
	if IsConst(key) {
		return fmt.Errorf("%w: key %q", lferrors.ErrReadOnly, key)
	}
	s.Set(key, value, opts...)
	return nil
}

// Get returns (value, true) if the key exists.
func (s *VarStore) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.vars[key]
	if !ok {
		return nil, false
	}
	return e.Value, true
}

// GetEntry returns the full entry if present.
func (s *VarStore) GetEntry(key string) (*VarEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.vars[key]
	if !ok {
		return nil, false
	}
	cp := *e
	return &cp, true
}

// Delete removes a key.
func (s *VarStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.vars, key)
}

// Keys returns sorted keys.
func (s *VarStore) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.vars))
	for k := range s.vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// All returns a shallow copy of entries (values are shared).
func (s *VarStore) All() map[string]*VarEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*VarEntry, len(s.vars))
	for k, v := range s.vars {
		cp := *v
		out[k] = &cp
	}
	return out
}

// Len returns the number of keys.
func (s *VarStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.vars)
}

// VisitableEntry pairs a key with its VarEntry for iteration.
type VisitableEntry struct {
	Key   string
	Entry *VarEntry
}

// Visitable returns all entries whose Visitable flag is true (including unset ones),
// sorted by key. Each element is a shallow copy.
func (s *VarStore) Visitable() []VisitableEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.vars))
	for k, e := range s.vars {
		if e != nil && e.Visitable {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]VisitableEntry, 0, len(keys))
	for _, k := range keys {
		cp := *s.vars[k]
		out = append(out, VisitableEntry{Key: k, Entry: &cp})
	}
	return out
}

// IsSet reports whether the key exists and has a non-nil value.
func (s *VarStore) IsSet(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.vars[key]
	return ok && e.Value != nil
}

// SetMany sets multiple keys with default visitable=true.
func (s *VarStore) SetMany(pairs map[string]any) {
	for k, v := range pairs {
		s.Set(k, v)
	}
}

// Merge copies entries from other into s (other wins on conflict).
func (s *VarStore) Merge(other *VarStore) {
	if other == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	for k, v := range other.vars {
		cp := *v
		s.vars[k] = &cp
	}
}

// PromptBlock renders visitable variables for system prompt injection.
func (s *VarStore) PromptBlock() string {
	type row struct {
		key  string
		val  any
		desc string
	}
	s.mu.RLock()
	rows := make([]row, 0, len(s.vars))
	for k, e := range s.vars {
		if e == nil || !e.Visitable {
			continue
		}
		rows = append(rows, row{key: k, val: e.Value, desc: e.Description})
	}
	s.mu.RUnlock()

	sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
	if len(rows) == 0 {
		return ""
	}
	var b bytes.Buffer

	b.WriteString("## Shared Variables\n")
	b.WriteString("The following variables are shared across agents in this session. ")
	b.WriteString("They persist through agent transfers and reflect the current state of the conversation.\n\n")

	b.WriteString("### How to use\n")
	b.WriteString("- Variables marked `<unset>` have been declared but not yet assigned. ")
	b.WriteString("Call `var_set` to assign a value when you have determined the correct one.\n")
	b.WriteString("- Variables prefixed with `const_` are **read-only** — they are injected by the system and cannot be modified via `var_set`.\n")
	b.WriteString("- All other variables can be updated at any time using `var_set` (you may set several in one call; only include keys that change).\n")
	b.WriteString("- You do not need to call any tool to read variables — their current values are shown below.\n\n")

	b.WriteString("### Current values\n")
	b.WriteString("```\n")
	for _, r := range rows {
		b.WriteString(r.key)
		b.WriteString(" = ")
		if r.val == nil {
			b.WriteString("<unset>")
		} else {
			// Marshal outside RLock: custom MarshalJSON must never run while holding
			// VarStore's mutex (e.g. re-entering PromptBlock/Get would deadlock).
			raw, err := sonic.Marshal(r.val)
			if err != nil {
				b.WriteString(fmt.Sprintf("%q", fmt.Sprint(r.val)))
			} else {
				b.Write(raw)
			}
		}
		if r.desc != "" {
			b.WriteString("  # ")
			b.WriteString(r.desc)
		}
		if IsConst(r.key) {
			b.WriteString(" (read-only)")
		}
		b.WriteByte('\n')
	}
	b.WriteString("```\n")
	return b.String()
}

// StoreSnapshot is JSON-serializable state for persistence.
type StoreSnapshot struct {
	Vars []VarSnapshot `json:"vars"`
}

// VarSnapshot is one variable in a snapshot.
type VarSnapshot struct {
	Key         string `json:"key"`
	Value       any    `json:"value"`
	Description string `json:"description"`
	Visitable   bool   `json:"visitable"`
}

// Snapshot exports the full store.
func (s *VarStore) Snapshot() *StoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.vars))
	for k := range s.vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	snap := &StoreSnapshot{Vars: make([]VarSnapshot, 0, len(keys))}
	for _, k := range keys {
		e := s.vars[k]
		if e == nil {
			continue
		}
		snap.Vars = append(snap.Vars, VarSnapshot{
			Key:         k,
			Value:       e.Value,
			Description: e.Description,
			Visitable:   e.Visitable,
		})
	}
	return snap
}

// Import builds a VarStore from a snapshot.
func Import(snap *StoreSnapshot) *VarStore {
	out := New()
	if snap == nil {
		return out
	}
	out.mu.Lock()
	defer out.mu.Unlock()
	for i := range snap.Vars {
		v := snap.Vars[i]
		if v.Key == "" {
			continue
		}
		out.vars[v.Key] = &VarEntry{
			Value:       v.Value,
			Description: v.Description,
			Visitable:   v.Visitable,
		}
	}
	return out
}

// MarshalJSON implements json.Marshaler via Snapshot.
func (s *VarStore) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}
	return sonic.Marshal(s.Snapshot())
}

// UnmarshalJSON implements json.Unmarshaler via Import.
func (s *VarStore) UnmarshalJSON(data []byte) error {
	var snap StoreSnapshot
	if err := sonic.Unmarshal(data, &snap); err != nil {
		return err
	}
	n := Import(&snap)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vars = make(map[string]*VarEntry, len(n.vars))
	for k, v := range n.vars {
		cp := *v
		s.vars[k] = &cp
	}
	return nil
}

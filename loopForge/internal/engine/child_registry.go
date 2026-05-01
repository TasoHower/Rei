package engine

import (
	"sync"

	"github.com/TasoHower/rei/loopForge/pkg/runtime/outcome"
)

// ChildRegistry tracks all active child Agent runs for a parent run,
// providing thread-safe registration, unregistration, and bulk cancellation.
type ChildRegistry struct {
	mu       sync.Mutex
	children map[string]*SpawnHandle // key = child RunID
}

// NewChildRegistry creates an empty registry.
func NewChildRegistry() *ChildRegistry {
	return &ChildRegistry{children: make(map[string]*SpawnHandle)}
}

// Register adds a child handle to the tracking table.
func (r *ChildRegistry) Register(id string, h *SpawnHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.children[id] = h
}

// Unregister removes a child handle (called on child goroutine exit).
// Returns the handle for metrics rollup, or nil if not found.
func (r *ChildRegistry) Unregister(id string) *SpawnHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.children[id]
	delete(r.children, id)
	return h
}

// TerminateAll snapshots all active children, releases the lock,
// then calls Shutdown on each. This prevents deadlock when a child
// goroutine concurrently calls Unregister during Shutdown.
func (r *ChildRegistry) TerminateAll(reason outcome.TerminationReason) int {
	r.mu.Lock()
	snapshot := r.children
	r.children = make(map[string]*SpawnHandle)
	r.mu.Unlock()
	for _, h := range snapshot {
		h.Shutdown(reason)
	}
	return len(snapshot)
}

// ActiveCount returns the current number of active children.
func (r *ChildRegistry) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.children)
}

// ChildInfo is a snapshot entry for observability.
type ChildInfo struct {
	RunID string     `json:"run_id"`
	Depth int        `json:"depth"`
	State ChildState `json:"state"`
}

// Snapshot returns a copy of all currently tracked children for observability.
func (r *ChildRegistry) Snapshot() []ChildInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	infos := make([]ChildInfo, 0, len(r.children))
	for id, h := range r.children {
		infos = append(infos, ChildInfo{
			RunID: id,
			Depth: h.Ref.Depth,
			State: h.State(),
		})
	}
	return infos
}

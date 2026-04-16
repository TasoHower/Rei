package variable

import (
	"github.com/bytedance/sonic"
	"strings"
	"sync"
	"testing"
)

func TestConstAgentSet(t *testing.T) {
	s := New()
	if err := s.AgentSet("const_x", "v"); err == nil {
		t.Fatal("expected error for const_ key")
	}
	s.Set("const_x", "v")
	v, ok := s.Get("const_x")
	if !ok || v != "v" {
		t.Fatalf("programmatic set: got %v %v", v, ok)
	}
}

func TestPromptBlockUnsetAndReadonly(t *testing.T) {
	s := New()
	s.Define("order_id", "订单号")
	s.Set("const_tier", "pro", WithDescription("等级"))
	s.Set("intent", "x", WithDescription("意图"))

	b := s.PromptBlock()
	if b == "" {
		t.Fatal("empty block")
	}
	t.Log(b)
	if want := "<unset>"; !strings.Contains(b, want) {
		t.Fatalf("expected %q in block:\n%s", want, b)
	}
	if want := "(read-only)"; !strings.Contains(b, want) {
		t.Fatalf("expected %q in block:\n%s", want, b)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := New()
	s.Define("a", "da")
	s.Set("b", 42, WithDescription("db"))
	snap := s.Snapshot()
	data, err := sonic.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var snap2 StoreSnapshot
	if err := sonic.Unmarshal(data, &snap2); err != nil {
		t.Fatal(err)
	}
	r := Import(&snap2)
	if r.Len() != 2 {
		t.Fatalf("len got %d", r.Len())
	}
}

func TestVarStoreJSON(t *testing.T) {
	s := New()
	s.Set("k", "v")
	data, err := sonic.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var s2 VarStore
	if err := sonic.Unmarshal(data, &s2); err != nil {
		t.Fatal(err)
	}
	v, ok := s2.Get("k")
	if !ok || v != "v" {
		t.Fatalf("got %v %v", v, ok)
	}
}

// TestIsSet checks declared-but-unset vs assigned states.
func TestIsSet(t *testing.T) {
	s := New()
	s.Define("a", "desc")
	if s.IsSet("a") {
		t.Fatal("Define should leave value unset")
	}
	if s.IsSet("missing") {
		t.Fatal("missing key must not be set")
	}
	s.Set("a", "value")
	if !s.IsSet("a") {
		t.Fatal("after Set, IsSet should be true")
	}
}

// TestDescriptionSetAndGet verifies Define and Set store descriptions correctly.
func TestDescriptionSetAndGet(t *testing.T) {
	s := New()
	s.Define("x", "my description")
	e, ok := s.GetEntry("x")
	if !ok {
		t.Fatal("entry not found after Define")
	}
	if e.Description != "my description" {
		t.Fatalf("expected 'my description', got %q", e.Description)
	}

	s.Set("y", 1, WithDescription("another desc"))
	e2, ok := s.GetEntry("y")
	if !ok {
		t.Fatal("entry not found after Set")
	}
	if e2.Description != "another desc" {
		t.Fatalf("expected 'another desc', got %q", e2.Description)
	}
}

// TestVisitableMethod checks Visitable() returns only visitable=true entries.
func TestVisitableMethod(t *testing.T) {
	s := New()
	s.Define("visible", "v")
	s.Set("hidden", "h", WithVisitable(false))
	s.Set("also_visible", "av")

	entries := s.Visitable()
	if len(entries) != 2 {
		t.Fatalf("expected 2 visitable entries, got %d", len(entries))
	}
	keys := make([]string, len(entries))
	for i, ve := range entries {
		keys[i] = ve.Key
	}
	// Should be sorted
	if keys[0] != "also_visible" || keys[1] != "visible" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

// TestConcurrentSafety exercises Set/Get/Delete from multiple goroutines.
func TestConcurrentSafety(t *testing.T) {
	s := New()
	const n = 200
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "k"
			s.Set(key, i)
			s.Get(key)
			s.IsSet(key)
			s.PromptBlock()
		}(i)
	}
	wg.Wait()
	// No race detector error = pass
}

package transfer

import (
	"testing"
)

func TestRegistry_Register_and_Get(t *testing.T) {
	r := NewRegistry()
	err := r.Register(AgentConfig{
		Name:      "alpha",
		ChatModel: mockFinalChatModel{text: "ok"},
	})
	if err != nil {
		t.Fatalf("Register alpha: %v", err)
	}
	err = r.Register(AgentConfig{
		Name:      "beta",
		ChatModel: mockFinalChatModel{text: "ok"},
	})
	if err != nil {
		t.Fatalf("Register beta: %v", err)
	}

	cfg, ok := r.Get("alpha")
	if !ok || cfg.Name != "alpha" {
		t.Fatal("Get alpha failed")
	}
	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("Get nonexistent should return false")
	}
}

func TestRegistry_DuplicateName(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(AgentConfig{Name: "dup", ChatModel: mockFinalChatModel{text: "ok"}})
	err := r.Register(AgentConfig{Name: "dup", ChatModel: mockFinalChatModel{text: "ok"}})
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestRegistry_EmptyName(t *testing.T) {
	r := NewRegistry()
	err := r.Register(AgentConfig{ChatModel: mockFinalChatModel{text: "ok"}})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRegistry_NilChatModel(t *testing.T) {
	r := NewRegistry()
	err := r.Register(AgentConfig{Name: "bad"})
	if err == nil {
		t.Fatal("expected error for nil ChatModel")
	}
}

func TestRegistry_Names_InsertionOrder(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"charlie", "alpha", "beta"} {
		_ = r.Register(AgentConfig{Name: n, ChatModel: mockFinalChatModel{text: "ok"}})
	}
	names := r.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	expected := []string{"charlie", "alpha", "beta"}
	for i, n := range names {
		if n != expected[i] {
			t.Fatalf("names[%d]=%q, want %q", i, n, expected[i])
		}
	}
}

func TestRegistry_TransferTargetsFor(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(AgentConfig{
		Name:            "triage",
		ChatModel:       mockFinalChatModel{text: "ok"},
		TransferTargets: []string{"math", "writer"},
	})
	_ = r.Register(AgentConfig{Name: "math", ChatModel: mockFinalChatModel{text: "ok"}})
	_ = r.Register(AgentConfig{Name: "writer", ChatModel: mockFinalChatModel{text: "ok"}})

	targets := r.TransferTargetsFor("triage")
	if len(targets) != 2 || targets[0] != "math" || targets[1] != "writer" {
		t.Fatalf("unexpected targets: %v", targets)
	}

	// math has no TransferTargets set → all others
	targets = r.TransferTargetsFor("math")
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets for math, got %d", len(targets))
	}
}

func TestRegistry_Validate_BadTarget(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(AgentConfig{
		Name:            "a",
		ChatModel:       mockFinalChatModel{text: "ok"},
		TransferTargets: []string{"nonexistent"},
	})
	err := r.Validate()
	if err == nil {
		t.Fatal("expected validation error for bad transfer target")
	}
}

func TestRegistry_Validate_OK(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(AgentConfig{Name: "a", ChatModel: mockFinalChatModel{text: "ok"}, TransferTargets: []string{"b"}})
	_ = r.Register(AgentConfig{Name: "b", ChatModel: mockFinalChatModel{text: "ok"}})
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

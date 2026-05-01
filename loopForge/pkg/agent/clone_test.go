package agent

import (
	"testing"

	"github.com/TasoHower/rei/loopForge/pkg/model"
)

func TestClone_SliceIsolation(t *testing.T) {
	original := &Agent{
		Name:        "original",
		Description: "test agent",
		ToolInfos:   []*model.ToolInfo{{Name: "t1"}},
		ExtraTools:  []*model.ToolInfo{{Name: "e1"}},
		CallOptions: []model.CallOption{model.WithTemperature(0.5)},
	}

	other := &Agent{Name: "other"}
	original.AddHandoff(other)

	cloned := original.Clone()

	// Mutate cloned slices — must not affect original.
	cloned.ToolInfos = append(cloned.ToolInfos, &model.ToolInfo{Name: "t2"})
	cloned.ExtraTools = append(cloned.ExtraTools, &model.ToolInfo{Name: "e2"})
	cloned.CallOptions = append(cloned.CallOptions, model.WithTemperature(0.9))
	cloned.AddHandoff(&Agent{Name: "extra"})

	if len(original.ToolInfos) != 1 {
		t.Fatalf("ToolInfos mutated: got %d, want 1", len(original.ToolInfos))
	}
	if len(original.ExtraTools) != 1 {
		t.Fatalf("ExtraTools mutated: got %d, want 1", len(original.ExtraTools))
	}
	if len(original.CallOptions) != 1 {
		t.Fatalf("CallOptions mutated: got %d, want 1", len(original.CallOptions))
	}
	if len(original.Handoffs()) != 1 {
		t.Fatalf("handoffs mutated: got %d, want 1", len(original.Handoffs()))
	}
}

func TestClone_ScalarCopy(t *testing.T) {
	builder := func(SystemPromptBuildContext) (string, error) { return "x", nil }
	original := &Agent{
		Name:                "orig",
		Description:         "desc",
		ModelName:           "model-1",
		SystemInstructions:  "sys",
		MaxSteps:            10,
		SystemPromptBuilder: builder,
	}

	cloned := original.Clone()
	cloned.Name = "cloned"
	cloned.Description = "changed"
	cloned.MaxSteps = 99

	if original.Name != "orig" {
		t.Fatalf("Name mutated: %q", original.Name)
	}
	if original.Description != "desc" {
		t.Fatalf("Description mutated: %q", original.Description)
	}
	if original.MaxSteps != 10 {
		t.Fatalf("MaxSteps mutated: %d", original.MaxSteps)
	}
	if cloned.SystemPromptBuilder == nil {
		t.Fatal("expected SystemPromptBuilder copied")
	}
}

func TestClone_NilSlices(t *testing.T) {
	original := &Agent{Name: "bare"}
	cloned := original.Clone()

	if cloned.ToolInfos != nil {
		t.Fatal("expected nil ToolInfos")
	}
	if cloned.ExtraTools != nil {
		t.Fatal("expected nil ExtraTools")
	}
	if cloned.CallOptions != nil {
		t.Fatal("expected nil CallOptions")
	}
	if cloned.Handoffs() != nil {
		t.Fatal("expected nil handoffs")
	}
}

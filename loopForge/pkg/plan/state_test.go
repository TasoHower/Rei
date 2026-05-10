package plan

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestPlan_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	p := &Plan{
		ID:          "plan-1",
		Title:       "Test Plan",
		Description: "A test plan with steps",
		Steps: []PlanStep{
			{
				ID:          "step-1",
				Description: "Step one",
				AssignedTo:  "self",
				Status:      PlanStepPending,
			},
			{
				ID:          "step-2",
				Description: "Step two",
				AssignedTo:  "spawn",
				DependsOn:   []string{"step-1"},
				Status:      PlanStepCompleted,
				Result:      "42",
			},
			{
				ID:          "step-3",
				Description: "Step three",
				AssignedTo:  "transfer:expert",
				Status:      PlanStepFailed,
			},
		},
		CreatedAt: now,
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Plan
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.ID != p.ID {
		t.Errorf("ID: got %q, want %q", got.ID, p.ID)
	}
	if got.Title != p.Title {
		t.Errorf("Title: got %q, want %q", got.Title, p.Title)
	}
	if got.Description != p.Description {
		t.Errorf("Description: got %q, want %q", got.Description, p.Description)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, now)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("Steps: got %d, want 3", len(got.Steps))
	}

	// Step 0
	if got.Steps[0].ID != "step-1" {
		t.Errorf("Steps[0].ID: got %q", got.Steps[0].ID)
	}
	if got.Steps[0].AssignedTo != "self" {
		t.Errorf("Steps[0].AssignedTo: got %q", got.Steps[0].AssignedTo)
	}
	if got.Steps[0].Status != PlanStepPending {
		t.Errorf("Steps[0].Status: got %q", got.Steps[0].Status)
	}

	// Step 1
	if got.Steps[1].ID != "step-2" {
		t.Errorf("Steps[1].ID: got %q", got.Steps[1].ID)
	}
	if got.Steps[1].AssignedTo != "spawn" {
		t.Errorf("Steps[1].AssignedTo: got %q", got.Steps[1].AssignedTo)
	}
	if len(got.Steps[1].DependsOn) != 1 || got.Steps[1].DependsOn[0] != "step-1" {
		t.Errorf("Steps[1].DependsOn: got %v", got.Steps[1].DependsOn)
	}
	if got.Steps[1].Status != PlanStepCompleted {
		t.Errorf("Steps[1].Status: got %q", got.Steps[1].Status)
	}
	if got.Steps[1].Result != "42" {
		t.Errorf("Steps[1].Result: got %q", got.Steps[1].Result)
	}

	// Step 2
	if got.Steps[2].ID != "step-3" {
		t.Errorf("Steps[2].ID: got %q", got.Steps[2].ID)
	}
	if got.Steps[2].AssignedTo != "transfer:expert" {
		t.Errorf("Steps[2].AssignedTo: got %q", got.Steps[2].AssignedTo)
	}
	if got.Steps[2].Status != PlanStepFailed {
		t.Errorf("Steps[2].Status: got %q", got.Steps[2].Status)
	}
}

func TestPlan_EmptySteps(t *testing.T) {
	data := `{"id":"p1","title":"empty","steps":[]}`
	var p Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal empty steps: %v", err)
	}
	if len(p.Steps) != 0 {
		t.Errorf("Steps: want empty, got %d", len(p.Steps))
	}
}

func TestPlan_MinimalStep(t *testing.T) {
	data := `{"id":"p1","title":"minimal","steps":[{"id":"s1","description":"do something"}]}`
	var p Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("Unmarshal minimal: %v", err)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("Steps: want 1, got %d", len(p.Steps))
	}
	if p.Steps[0].ID != "s1" {
		t.Errorf("Step.ID: got %q", p.Steps[0].ID)
	}
	if p.Steps[0].Description != "do something" {
		t.Errorf("Step.Description: got %q", p.Steps[0].Description)
	}
	// Defaults
	if p.Steps[0].Status != "" {
		t.Errorf("Step.Status should be empty, got %q", p.Steps[0].Status)
	}
	if p.Steps[0].AssignedTo != "" {
		t.Errorf("Step.AssignedTo should be empty, got %q", p.Steps[0].AssignedTo)
	}

	// Verify that team-assigned step serializes and deserializes
	teamData := `{"id":"p2","title":"team test","steps":[{"id":"s1","description":"data task","assigned_to":"team:data-team"}]}`
	var p2 Plan
	if err := json.Unmarshal([]byte(teamData), &p2); err != nil {
		t.Fatalf("Unmarshal team: %v", err)
	}
	if p2.Steps[0].AssignedTo != "team:data-team" {
		t.Errorf("AssignedTo: got %q, want team:data-team", p2.Steps[0].AssignedTo)
	}
}

func TestPlanStepStatus_Values(t *testing.T) {
	tests := []struct {
		status PlanStepStatus
		want   string
	}{
		{PlanStepPending, "pending"},
		{PlanStepInProgress, "in_progress"},
		{PlanStepCompleted, "completed"},
		{PlanStepFailed, "failed"},
		{PlanStepSkipped, "skipped"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("got %q, want %q", tt.status, tt.want)
			}
			data, err := json.Marshal(tt.status)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got PlanStepStatus
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != tt.status {
				t.Errorf("round trip: got %q, want %q", got, tt.status)
			}
		})
	}
}

func TestPlanState_SetPlanAndPlan(t *testing.T) {
	ps := NewPlanState()
	if p := ps.Plan(); p != nil {
		t.Fatal("expected nil plan before SetPlan")
	}

	p := &Plan{
		ID:    "plan-1",
		Title: "Test",
		Steps: []PlanStep{
			{ID: "s1", Description: "first", Status: PlanStepPending},
		},
	}
	if err := ps.SetPlan(p); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}

	got := ps.Plan()
	if got == nil {
		t.Fatal("Plan() returned nil after SetPlan")
	}
	if got.ID != "plan-1" {
		t.Errorf("Plan.ID: got %q", got.ID)
	}
	if len(got.Steps) != 1 {
		t.Errorf("Plan.Steps: got %d, want 1", len(got.Steps))
	}
}

func TestPlanState_SetPlan_RejectDuplicate(t *testing.T) {
	ps := NewPlanState()
	p := &Plan{
		ID:    "p1",
		Title: "test",
		Steps: []PlanStep{{ID: "s1", Description: "step"}},
	}
	if err := ps.SetPlan(p); err != nil {
		t.Fatalf("first SetPlan: %v", err)
	}
	if err := ps.SetPlan(p); err != ErrPlanAlreadySet {
		t.Fatalf("second SetPlan: got %v, want ErrPlanAlreadySet", err)
	}
}

func TestPlanState_UpdateStepStatus(t *testing.T) {
	ps := NewPlanState()
	p := &Plan{
		ID:    "p1",
		Title: "test",
		Steps: []PlanStep{
			{ID: "s1", Description: "step one"},
			{ID: "s2", Description: "step two"},
		},
	}
	ps.SetPlan(p)

	// Update s1 to in_progress
	if err := ps.UpdateStepStatus("s1", PlanStepInProgress, ""); err != nil {
		t.Fatalf("UpdateStepStatus: %v", err)
	}
	got := ps.Plan()
	if got.Steps[0].Status != PlanStepInProgress {
		t.Errorf("s1 status: got %q, want in_progress", got.Steps[0].Status)
	}
	if got.Steps[0].StartedAt == nil {
		t.Error("s1 StartedAt should be set")
	}

	// Update s1 to completed
	if err := ps.UpdateStepStatus("s1", PlanStepCompleted, "done"); err != nil {
		t.Fatalf("UpdateStepStatus: %v", err)
	}
	got = ps.Plan()
	if got.Steps[0].Status != PlanStepCompleted {
		t.Errorf("s1 status: got %q, want completed", got.Steps[0].Status)
	}
	if got.Steps[0].Result != "done" {
		t.Errorf("s1 result: got %q, want done", got.Steps[0].Result)
	}
	if got.Steps[0].CompletedAt == nil {
		t.Error("s1 CompletedAt should be set")
	}
	if got.Steps[0].StartedAt == nil {
		t.Error("s1 StartedAt should still be set from in_progress")
	}

	// s2 should be unchanged (zero value is "" not "pending")
	if got.Steps[1].Status != "" {
		t.Errorf("s2 status: got %q, want zero value", got.Steps[1].Status)
	}
	if got.Steps[1].StartedAt != nil {
		t.Error("s2 StartedAt should be nil")
	}
}

func TestPlanState_UpdateStepStatus_NotFound(t *testing.T) {
	ps := NewPlanState()
	p := &Plan{
		ID:    "p1",
		Title: "test",
		Steps: []PlanStep{{ID: "s1", Description: "step"}},
	}
	ps.SetPlan(p)

	err := ps.UpdateStepStatus("s99", PlanStepCompleted, "")
	if err != ErrStepNotFound {
		t.Fatalf("got %v, want ErrStepNotFound", err)
	}
}

func TestPlanState_UpdateStepStatus_NoPlan(t *testing.T) {
	ps := NewPlanState()
	err := ps.UpdateStepStatus("s1", PlanStepCompleted, "")
	if err != ErrNoPlan {
		t.Fatalf("got %v, want ErrNoPlan", err)
	}
}

func TestPlanState_Snapshot(t *testing.T) {
	ps := NewPlanState()
	p := &Plan{
		ID:    "p1",
		Title: "test",
		Steps: []PlanStep{{ID: "s1", Description: "step", Status: PlanStepPending}},
	}
	ps.SetPlan(p)

	snap := ps.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot returned nil")
	}
	if snap.ID != "p1" {
		t.Errorf("Snapshot.ID: got %q", snap.ID)
	}

	// Mutate original through PlanState
	ps.UpdateStepStatus("s1", PlanStepCompleted, "result")

	// Snapshot should be a copy — still unchanged (zero value)
	if snap.Steps[0].Status != PlanStepPending {
		t.Errorf("Snapshot.Steps[0].Status: got %q, want pending (should be immutable)",
			snap.Steps[0].Status)
	}
	// The PlanState should reflect the updated status
	updated := ps.Plan()
	if updated.Steps[0].Status != PlanStepCompleted {
		t.Errorf("PlanState.Steps[0].Status after update: got %q, want completed",
			updated.Steps[0].Status)
	}
}

func TestPlanState_ConcurrentAccess(t *testing.T) {
	ps := NewPlanState()
	p := &Plan{
		ID:    "p1",
		Title: "concurrent",
		Steps: []PlanStep{
			{ID: "s1", Description: "step one"},
			{ID: "s2", Description: "step two"},
			{ID: "s3", Description: "step three"},
		},
	}
	ps.SetPlan(p)

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ps.Plan()
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ps.UpdateStepStatus("s1", PlanStepInProgress, "")
			_ = ps.UpdateStepStatus("s2", PlanStepCompleted, "42")
			_ = ps.UpdateStepStatus("s3", PlanStepInProgress, "")
		}()
	}

	// Concurrent snapshot
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ps.Snapshot()
		}()
	}

	wg.Wait()

	got := ps.Plan()
	if got.Steps[0].ID != "s1" {
		t.Errorf("step corrupted after concurrent access: ID=%q", got.Steps[0].ID)
	}
	if len(got.Steps) != 3 {
		t.Errorf("steps list corrupted: len=%d", len(got.Steps))
	}
}

func TestPlanState_ResultWithoutStatusChange(t *testing.T) {
	ps := NewPlanState()
	p := &Plan{
		ID:    "p1",
		Title: "test",
		Steps: []PlanStep{{ID: "s1", Description: "step", Status: PlanStepPending}},
	}
	ps.SetPlan(p)

	// Update with same status but with result
	if err := ps.UpdateStepStatus("s1", PlanStepPending, "intermediate"); err != nil {
		t.Fatalf("UpdateStepStatus: %v", err)
	}
	got := ps.Plan()
	if got.Steps[0].Status != PlanStepPending {
		t.Errorf("status changed unexpectedly: %q", got.Steps[0].Status)
	}
	if got.Steps[0].Result != "intermediate" {
		t.Errorf("result: got %q, want intermediate", got.Steps[0].Result)
	}
	if got.Steps[0].StartedAt != nil {
		t.Error("StartedAt should not be set for non-in_progress transition")
	}
}

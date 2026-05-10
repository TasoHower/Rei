package plan

import (
	"errors"
	"sync"
	"time"
)

// PlanState is owned by the Runner and shared across agent hops.
// All exported methods are thread-safe.
type PlanState struct {
	mu   sync.RWMutex
	plan *Plan
}

// ErrPlanAlreadySet is returned when SetPlan is called more than once.
var ErrPlanAlreadySet = errors.New("plan already set, only one plan per run")

// ErrNoPlan is returned when UpdateStepStatus or Snapshot is called before SetPlan.
var ErrNoPlan = errors.New("no plan set")

// ErrStepNotFound is returned when UpdateStepStatus references a non-existent step ID.
var ErrStepNotFound = errors.New("step not found")

// NewPlanState creates an empty PlanState.
func NewPlanState() *PlanState {
	return &PlanState{}
}

// SetPlan stores the plan. Returns ErrPlanAlreadySet if a plan already exists.
func (ps *PlanState) SetPlan(p *Plan) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.plan != nil {
		return ErrPlanAlreadySet
	}
	ps.plan = p
	return nil
}

// Plan returns a deep copy of the current plan, or nil if no plan has been set.
func (ps *PlanState) Plan() *Plan {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.plan == nil {
		return nil
	}
	return ps.plan.clone()
}

// UpdateStepStatus updates the status and optionally the result of a step.
// If the status transitions to/from in_progress, StartedAt/CompletedAt are updated.
// Returns ErrStepNotFound if stepID doesn't exist, or ErrNoPlan if no plan is set.
func (ps *PlanState) UpdateStepStatus(stepID string, status PlanStepStatus, result string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.plan == nil {
		return ErrNoPlan
	}
	for i := range ps.plan.Steps {
		if ps.plan.Steps[i].ID == stepID {
			now := time.Now()
			if status == PlanStepInProgress && ps.plan.Steps[i].Status != PlanStepInProgress {
				ps.plan.Steps[i].StartedAt = &now
			}
			if isTerminal(status) && ps.plan.Steps[i].Status != status {
				ps.plan.Steps[i].CompletedAt = &now
			}
			ps.plan.Steps[i].Status = status
			if result != "" {
				ps.plan.Steps[i].Result = result
			}
			return nil
		}
	}
	return ErrStepNotFound
}

// Snapshot returns a deep copy of the current plan, or nil if no plan has been set.
func (ps *PlanState) Snapshot() *Plan {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.plan == nil {
		return nil
	}
	return ps.plan.clone()
}

// clone creates a deep copy of the Plan.
func (p *Plan) clone() *Plan {
	if p == nil {
		return nil
	}
	steps := make([]PlanStep, len(p.Steps))
	for i, s := range p.Steps {
		depends := make([]string, len(s.DependsOn))
		copy(depends, s.DependsOn)
		var started *time.Time
		if s.StartedAt != nil {
			t := *s.StartedAt
			started = &t
		}
		var completed *time.Time
		if s.CompletedAt != nil {
			t := *s.CompletedAt
			completed = &t
		}
		steps[i] = PlanStep{
			ID:          s.ID,
			Description: s.Description,
			AssignedTo:  s.AssignedTo,
			DependsOn:   depends,
			Status:      s.Status,
			Result:      s.Result,
			StartedAt:   started,
			CompletedAt: completed,
		}
	}
	return &Plan{
		ID:          p.ID,
		Title:       p.Title,
		Description: p.Description,
		Steps:       steps,
		CreatedAt:   p.CreatedAt,
	}
}

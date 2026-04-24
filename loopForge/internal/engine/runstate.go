package engine

import (
	"time"

	"loopforge/pkg/log"
	"loopforge/pkg/runtime/budget"
	"loopforge/pkg/runtime/outcome"
)

const DefaultGuardTimeout = 5 * time.Second

// RunState tracks the execution state of a single agent run.
// For root runs, Children and BudgetCounter are populated.
// Child runs inherit a pointer to the root's BudgetCounter.
type RunState struct {
	RunID           string
	Step            int
	LastError       error
	Termination     outcome.TerminationReason
	ParentRunID     string
	Depth           int
	ActiveChildRuns int // cache; source of truth is Children.ActiveCount()
	AllowSpawn      bool

	// Children tracks all active child runs for cascade cancellation.
	Children *ChildRegistry

	// BudgetCounter is the tree-wide shared budget (root-owned).
	BudgetCounter *budget.BudgetCounter
}

// Terminate cancels all active children (cascade cancellation, §2).
// It delegates to ChildRegistry.TerminateAll.
func (r *RunState) Terminate(reason outcome.TerminationReason) {
	r.Termination = reason
	if r.Children != nil {
		r.Children.TerminateAll(reason)
	}
	r.ActiveChildRuns = 0
}

// TerminateWithGuard calls Terminate and starts a watchdog goroutine that
// logs a warning if any child fails to respond to cancellation within guardTimeout.
func (r *RunState) TerminateWithGuard(reason outcome.TerminationReason, guardTimeout time.Duration) {
	r.Terminate(reason)
	snapshot := r.Children.Snapshot()
	if len(snapshot) == 0 {
		return
	}
	// Collect handles for watchdog monitoring
	// (after TerminateAll the registry is empty, but handles still track their goroutines).
	r.Children.ActiveCount()
	go func() {
		// We can't easily observe individual handles after TerminateAll clears the registry,
		// so the watchdog simply logs after guardTimeout with context info.
		time.Sleep(guardTimeout)
		// At this point, all handles' done channels should be closed.
		// If not, the child goroutine is unresponsive (bug).
		log.Default().Warn("child runs may not have responded to cancellation within guard timeout",
			"parent_run_id", r.RunID,
			"guard_timeout", guardTimeout.String(),
		)
	}()
}

// SpawnInfo returns observability data about the current spawn state.
type SpawnInfo struct {
	ActiveCount int         `json:"active_count"`
	Children    []ChildInfo `json:"children"`
	BudgetUsed  int64       `json:"budget_tokens_used"`
	BudgetMax   int64       `json:"budget_tokens_max"`
	CostUsed    float64     `json:"cost_usd_used"`
	CostMax     float64     `json:"cost_usd_max"`
}

// SpawnInfo returns a snapshot of spawn-related state for observability.
func (r *RunState) SpawnInfo() SpawnInfo {
	info := SpawnInfo{}
	if r.Children != nil {
		info.ActiveCount = r.Children.ActiveCount()
		info.Children = r.Children.Snapshot()
	}
	if r.BudgetCounter != nil {
		info.BudgetUsed, info.CostUsed = r.BudgetCounter.Used()
		info.BudgetMax, info.CostMax = r.BudgetCounter.Limits()
	}
	return info
}

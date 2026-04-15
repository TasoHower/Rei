package engine

import "loopforge/pkg/runtime/outcome"

// RunState is the engine-owned authoritative state for one run (single source of truth).
type RunState struct {
	RunID           string
	Step            int
	LastError       error
	Termination     outcome.TerminationReason
	ParentRunID     string
	Depth           int
	ActiveChildRuns int
}

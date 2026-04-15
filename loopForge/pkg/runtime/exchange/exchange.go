package exchange

import (
	"time"

	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/tool"
)

// RunRef identifies one agent loop execution in the engine.
type RunRef struct {
	RunID       string
	AgentRole   string
	ParentRunID string
	Depth       int
}

// NetworkTurn describes one agent slot output feeding synthesis or the next slot.
type NetworkTurn struct {
	SlotName   string
	RunRef     RunRef
	InputHint  string
	OutputText string
	ToolCalls  []tool.ToolCall
}

// SpawnSpec is the cross-agent contract from parent to child run.
type SpawnSpec struct {
	Task           string
	SystemAddendum string
	SkillIDs       []string
	ToolAllowlist  []string
	LoopOverrides  LoopOverrides
	ModelOverride  string
}

// LoopOverrides allows per-child run loop tuning.
type LoopOverrides struct {
	MaxSteps *int
	Timeout  *time.Duration
}

// SpawnResult is returned to the parent agent as tool result content.
type SpawnResult struct {
	ChildRunRef RunRef
	Status      SpawnStatus
	FinalText   string
	Error       *SpawnError
	Metrics     outcome.RunMetrics
}

// SpawnStatus is the completion status of a child run.
type SpawnStatus string

const (
	SpawnCompleted SpawnStatus = "completed"
	SpawnFailed    SpawnStatus = "failed"
	SpawnRejected  SpawnStatus = "rejected"
)

// SpawnError carries structured failure for spawn results.
type SpawnError struct {
	Code    string
	Message string
}

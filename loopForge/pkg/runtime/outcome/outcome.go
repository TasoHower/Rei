package outcome

import "github.com/TasoHower/Rei/loopForge/pkg/variable"

// RuntimeOutcome is the synchronous or end-of-stream summary for a run.
type RuntimeOutcome struct {
	RunID         string
	FinalText     string
	Termination   TerminationReason
	Metrics       RunMetrics
	ChildRunIDs   []string
	TransferChain []string // ordered agent names visited during transfer handoffs
	// VarStore is the variable state at end of run (nil if variables unused).
	VarStore *variable.VarStore `json:"-"`
}

// TerminationReason explains why the run stopped.
type TerminationReason string

const (
	TerminationCompleted TerminationReason = "completed"
	TerminationMaxSteps  TerminationReason = "max_steps"
	TerminationCancelled TerminationReason = "cancelled"
	TerminationError     TerminationReason = "error"
)

// RunMetrics aggregates usage for observability and cost rollup.
type RunMetrics struct {
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	// Steps counts LLM calls (one Generate/stream round-trip per loop iteration).
	// When using pkg/runner with handoffs, the final RuntimeOutcome sums Steps
	// across all agents in the transfer chain (see LoopState.AccumulatedMetrics).
	Steps int `json:"steps"`
}

package outcome

// RuntimeOutcome is the synchronous or end-of-stream summary for a run.
type RuntimeOutcome struct {
	RunID       string
	FinalText   string
	Termination TerminationReason
	Metrics     RunMetrics
	ChildRunIDs []string
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
	Steps        int     `json:"steps"`
}

package transfer

// OrchestratorOption configures an Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithMaxTransfers sets the maximum number of agent-to-agent transfers allowed
// in a single run. Prevents infinite handoff loops. Default is 10.
func WithMaxTransfers(n int) OrchestratorOption {
	return func(o *Orchestrator) {
		if n > 0 {
			o.MaxTransfers = n
		}
	}
}

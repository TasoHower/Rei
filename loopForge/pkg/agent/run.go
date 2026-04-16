package agent

import (
	"context"
	"fmt"

	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
)

const defaultMaxTransfers = 10

// Runner is the top-level execution entry point for an agent graph. It manages
// the runtime lifecycle including transfer orchestration, metrics tracking, and
// concurrency isolation (per-run Clone of agent templates).
//
// For single-agent use without transfers, Runner works identically to calling
// RunnerAgent.Run directly.
type Runner struct {
	entryAgent   *RunnerAgent
	maxTransfers int
}

var _ Agent = (*Runner)(nil)

// RunOption configures a Runner when passed to NewRunner.
type RunOption func(*Runner)

// NewRunner creates a Runner that starts execution from entryAgent.
func NewRunner(entryAgent *RunnerAgent, opts ...RunOption) *Runner {
	r := &Runner{
		entryAgent:   entryAgent,
		maxTransfers: defaultMaxTransfers,
	}
	for _, o := range opts {
		if o != nil {
			o(r)
		}
	}
	return r
}

// WithMaxTransfers sets the maximum number of agent-to-agent transfers allowed
// in a single run. Prevents infinite handoff loops. Default is 10.
func WithMaxTransfers(n int) RunOption {
	return func(r *Runner) {
		if n > 0 {
			r.maxTransfers = n
		}
	}
}

// Run starts the agent graph in a goroutine and returns a channel that streams
// RuntimeEvents. If the entry agent has handoff targets, the runner
// automatically manages the transfer loop; otherwise it runs the single agent.
func (r *Runner) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
	ch := make(chan *event.RuntimeEvent, 8)

	go func() {
		defer close(ch)
		if len(r.entryAgent.Handoffs()) > 0 {
			r.runTransferLoop(ctx, req, ch)
		} else {
			r.entryAgent.RunLoop(ctx, req, ch, nil, nil)
		}
	}()

	return ch
}

func (r *Runner) runTransferLoop(ctx context.Context, req *request.RuntimeRequest, ch chan<- *event.RuntimeEvent) {
	runID := runIDFrom(req)

	emit := func(step int, payload event.EventPayload) {
		select {
		case ch <- event.Emit(runID, step, payload):
		case <-ctx.Done():
		}
	}

	emitError := func(code, msg string) {
		emit(0, &event.ErrorPayload{Code: code, Message: msg})
		emit(0, &event.QueryEndPayload{Outcome: &outcome.RuntimeOutcome{
			RunID:       runID,
			Termination: outcome.TerminationError,
		}})
	}

	if r.entryAgent == nil {
		emitError("invalid_config", "Runner.entryAgent is nil")
		return
	}

	current := r.entryAgent
	var inheritedMsgs []*model.Message
	accumulated := outcome.RunMetrics{}
	var transferChain []string
	transferCount := 0
	firstAgent := true

	for {
		runner := current.Clone()
		runner.ExtraTools = buildTransferTools(current)
		runner.ToolInterceptor = isTransferTool
		if p := buildTransferPrompt(current); p != "" {
			runner.SystemInstructions += p
		}

		st := &LoopState{
			AccumulatedMetrics: accumulated,
			TransferChain:      transferChain,
			SuppressBookends:   !firstAgent,
		}

		result := runner.RunLoop(ctx, req, ch, inheritedMsgs, st)

		if result == nil {
			return
		}

		transferCount++
		if transferCount > r.maxTransfers {
			emitError("max_transfers", "exceeded maximum transfer count")
			return
		}

		accumulated.InputTokens += result.Metrics.InputTokens
		accumulated.OutputTokens += result.Metrics.OutputTokens
		accumulated.TotalTokens += result.Metrics.TotalTokens
		accumulated.Steps += result.Metrics.Steps
		if accumulated.Model == "" {
			accumulated.Model = result.Metrics.Model
		}

		target := targetAgent(result.ToolCall.Name)
		reason := extractReason(result.ToolCall.Arguments)

		next := findHandoff(current, target)
		if next == nil {
			emitError("invalid_transfer", "target agent not found in handoffs: "+target)
			return
		}

		transferChain = append(transferChain, current.Name)

		emit(0, &event.AgentTransferPayload{
			Phase:     event.TransferStart,
			FromAgent: current.Name,
			ToAgent:   target,
			Reason:    reason,
		})

		inheritedMsgs = append(result.Msgs, &model.Message{
			Role:       model.RoleTool,
			ToolCallID: result.ToolCall.ID,
			Name:       result.ToolCall.Name,
			Content:    fmt.Sprintf("Transfer accepted. The conversation is now handled by %s.", target),
		})
		current = next
		firstAgent = false
	}
}

func findHandoff(a *RunnerAgent, name string) *RunnerAgent {
	for _, h := range a.Handoffs() {
		if h.Name == name {
			return h
		}
	}
	return nil
}

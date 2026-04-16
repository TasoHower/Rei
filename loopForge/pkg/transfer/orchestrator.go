package transfer

import (
	"context"
	"fmt"

	"loopforge/pkg/agent"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
)

const defaultMaxTransfers = 10

// Orchestrator manages multi-agent transfer (handoff) loops.
// It satisfies the agent.Agent interface, so callers see the same streaming
// channel regardless of how many agents participate in handling a request.
type Orchestrator struct {
	EntryAgent   *agent.RunnerAgent
	MaxTransfers int
}

var _ agent.Agent = (*Orchestrator)(nil)

// NewOrchestrator creates an Orchestrator that starts with entryAgent and
// may transfer up to MaxTransfers times across the agents reachable via
// RunnerAgent.Handoffs().
func NewOrchestrator(entryAgent *agent.RunnerAgent, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		EntryAgent:   entryAgent,
		MaxTransfers: defaultMaxTransfers,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// Run starts the orchestration loop in a goroutine and returns a unified event channel.
func (o *Orchestrator) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
	ch := make(chan *event.RuntimeEvent, 8)

	go func() {
		defer close(ch)
		o.orchestrate(ctx, req, ch)
	}()

	return ch
}

func (o *Orchestrator) orchestrate(ctx context.Context, req *request.RuntimeRequest, ch chan<- *event.RuntimeEvent) {
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

	if o.EntryAgent == nil {
		emitError("invalid_config", "Orchestrator.EntryAgent is nil")
		return
	}

	current := o.EntryAgent
	var inheritedMsgs []*model.Message
	accumulated := outcome.RunMetrics{}
	var transferChain []string
	transferCount := 0
	firstAgent := true

	for {
		runner := current.Clone()
		runner.ExtraTools = BuildTools(current)
		runner.ToolInterceptor = IsTransferTool
		if p := BuildTransferPrompt(current); p != "" {
			runner.SystemInstructions += p
		}

		st := &agent.LoopState{
			AccumulatedMetrics: accumulated,
			TransferChain:      transferChain,
			SuppressBookends:   !firstAgent,
		}

		result := runner.RunLoop(ctx, req, ch, inheritedMsgs, st)

		if result == nil {
			return
		}

		transferCount++
		if transferCount > o.MaxTransfers {
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

		target := TargetAgent(result.ToolCall.Name)
		reason := ExtractReason(result.ToolCall.Arguments)

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

// findHandoff looks up a target agent by Name in a's handoff list.
func findHandoff(a *agent.RunnerAgent, name string) *agent.RunnerAgent {
	for _, h := range a.Handoffs() {
		if h.Name == name {
			return h
		}
	}
	return nil
}

func runIDFrom(req *request.RuntimeRequest) string {
	if req.SessionID != "" {
		return req.SessionID
	}
	return "run"
}

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
	Registry     *Registry
	EntryAgent   string
	MaxTransfers int
}

var _ agent.Agent = (*Orchestrator)(nil)

// NewOrchestrator creates an Orchestrator that starts with entryAgent and
// may transfer up to MaxTransfers times across the agents in registry.
func NewOrchestrator(registry *Registry, entryAgent string, opts ...OrchestratorOption) *Orchestrator {
	o := &Orchestrator{
		Registry:     registry,
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

	if o.Registry == nil {
		emitError("invalid_config", "Orchestrator.Registry is nil")
		return
	}

	currentName := o.EntryAgent
	var inheritedMsgs []*model.Message
	accumulated := outcome.RunMetrics{}
	var transferChain []string
	transferCount := 0
	firstAgent := true

	for {
		cfg, ok := o.Registry.Get(currentName)
		if !ok {
			emitError("invalid_config", "agent "+currentName+" not found in registry")
			return
		}

		runner := runnerFromConfig(cfg, o.Registry, currentName)

		st := &agent.LoopState{
			AccumulatedMetrics: accumulated,
			TransferChain:      transferChain,
			SuppressBookends:   !firstAgent,
		}

		result := runner.RunLoop(ctx, req, ch, inheritedMsgs, st)

		if result == nil {
			return
		}

		// Transfer requested.
		transferCount++
		if transferCount > o.MaxTransfers {
			emitError("max_transfers", "exceeded maximum transfer count")
			return
		}

		// Accumulate metrics from the departing agent.
		accumulated.InputTokens += result.Metrics.InputTokens
		accumulated.OutputTokens += result.Metrics.OutputTokens
		accumulated.TotalTokens += result.Metrics.TotalTokens
		accumulated.Steps += result.Metrics.Steps
		if accumulated.Model == "" {
			accumulated.Model = result.Metrics.Model
		}

		target := TargetAgent(result.ToolCall.Name)
		reason := ExtractReason(result.ToolCall.Arguments)

		transferChain = append(transferChain, currentName)

		emit(0, &event.AgentTransferPayload{
			Phase:     event.TransferStart,
			FromAgent: currentName,
			ToAgent:   target,
			Reason:    reason,
		})

		// Append a synthetic tool result to close the dangling tool call.
		// Without this, the next agent sees an assistant message with a
		// pending tool call but no result, which confuses most LLMs.
		inheritedMsgs = append(result.Msgs, &model.Message{
			Role:       model.RoleTool,
			ToolCallID: result.ToolCall.ID,
			Name:       result.ToolCall.Name,
			Content:    fmt.Sprintf("Transfer accepted. The conversation is now handled by %s.", target),
		})
		currentName = target
		firstAgent = false
	}
}

func runnerFromConfig(cfg *AgentConfig, registry *Registry, currentName string) *agent.RunnerAgent {
	return &agent.RunnerAgent{
		Name:               cfg.Name,
		ModelName:          cfg.ModelName,
		SystemInstructions: cfg.SystemInstructions,
		ChatModel:          cfg.ChatModel,
		ToolInfos:          cfg.ToolInfos,
		Executor:           cfg.Executor,
		MaxSteps:           cfg.MaxSteps,
		CallOptions:        cfg.CallOptions,
		ExtraTools:         BuildTools(currentName, registry),
		ToolInterceptor:    IsTransferTool,
	}
}

func runIDFrom(req *request.RuntimeRequest) string {
	if req.SessionID != "" {
		return req.SessionID
	}
	return "run"
}

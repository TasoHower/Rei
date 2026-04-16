package agent

import (
	"context"

	"loopforge/pkg/model"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/tool"
)

// ToolInterceptor, when set on a RunnerAgent, is called before executing each
// tool call. If it returns true the tool call is intercepted: RunLoop returns
// an *InterceptedCall instead of invoking the tool through tool.Invoke.
type ToolInterceptor func(tc model.ToolCallPart) bool

// InterceptedCall is returned from RunLoop when a ToolInterceptor matches a
// tool call. It carries the intercepted call, the conversation so far, and
// accumulated metrics for the current agent's portion of the run.
type InterceptedCall struct {
	ToolCall model.ToolCallPart
	Msgs     []*model.Message
	Metrics  outcome.RunMetrics
}

// LoopState carries accumulated context from previous agents in an orchestrated
// multi-agent run. Pass nil for standalone (non-orchestrated) usage.
type LoopState struct {
	AccumulatedMetrics outcome.RunMetrics
	TransferChain      []string
	SuppressBookends   bool // skip Start/Question (already emitted by first agent)
}

// RunnerAgent is a concrete Agent that drives a tool-calling loop using
// pkg/model only. Model access (HTTP/SDK provider wiring) stays in pkg/model
// adapters; this type does not use agent-sdk-go Agent or Runner.
//
// Function calling: register ToolInfos (schema for the model). For each tool,
// set ToolInfo.Handle to bind the local Go function, and/or set Executor as a
// fallback (see loopforge/pkg/tool).
//
// Transfer (handoff): call AddHandoff to register other RunnerAgent instances
// as allowed transfer targets. The Orchestrator in pkg/transfer reads these
// via Handoffs() and injects the corresponding transfer tools at runtime.
type RunnerAgent struct {
	Name               string
	Description        string // human-readable summary; surfaced in transfer tool descriptions
	ModelName          string
	SystemInstructions string
	ChatModel          model.ToolCallingChatModel
	ToolInfos          []*model.ToolInfo
	Executor           tool.ToolExecutor

	MaxSteps int
	// CallOptions are passed to every Generate (e.g. model.WithTemperature);
	// per-request WithModel is merged from RuntimeRequest.Options.Model.
	CallOptions []model.CallOption

	// ExtraTools are appended to ToolInfos when calling the model but are not
	// passed to tool.ValidateBindings (they carry no Handle). Orchestrators
	// use this to inject transfer tools.
	ExtraTools []*model.ToolInfo

	// ToolInterceptor is checked before executing each tool call. If it
	// returns true, RunLoop returns an *InterceptedCall immediately instead
	// of invoking the tool.
	ToolInterceptor ToolInterceptor

	handoffs []*RunnerAgent
}

// AddHandoff registers one or more agents as allowed transfer (handoff) targets.
// The Orchestrator reads these at runtime to build transfer_to_{name} tools.
func (a *RunnerAgent) AddHandoff(targets ...*RunnerAgent) {
	a.handoffs = append(a.handoffs, targets...)
}

// Handoffs returns the registered transfer targets.
func (a *RunnerAgent) Handoffs() []*RunnerAgent {
	return a.handoffs
}

// Clone creates a shallow copy of the RunnerAgent with independent slice
// headers. The elements themselves (e.g. *ToolInfo, *RunnerAgent in handoffs)
// are shared — they are treated as immutable templates.
func (a *RunnerAgent) Clone() *RunnerAgent {
	c := *a
	if a.ToolInfos != nil {
		c.ToolInfos = make([]*model.ToolInfo, len(a.ToolInfos))
		copy(c.ToolInfos, a.ToolInfos)
	}
	if a.ExtraTools != nil {
		c.ExtraTools = make([]*model.ToolInfo, len(a.ExtraTools))
		copy(c.ExtraTools, a.ExtraTools)
	}
	if a.CallOptions != nil {
		c.CallOptions = make([]model.CallOption, len(a.CallOptions))
		copy(c.CallOptions, a.CallOptions)
	}
	if a.handoffs != nil {
		c.handoffs = make([]*RunnerAgent, len(a.handoffs))
		copy(c.handoffs, a.handoffs)
	}
	return &c
}

var _ Agent = (*RunnerAgent)(nil)

// Run starts the agent loop in a goroutine and returns a channel that streams
// RuntimeEvents. The channel is closed when the run finishes.
func (a *RunnerAgent) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
	ch := make(chan *event.RuntimeEvent, 8)

	go func() {
		defer close(ch)
		a.RunLoop(ctx, req, ch, nil, nil)
	}()

	return ch
}

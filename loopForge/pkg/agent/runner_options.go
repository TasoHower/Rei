package agent

import (
	"loopforge/pkg/model"
	"loopforge/pkg/tool"
)

// RunnerOption configures a RunnerAgent when passed to NewRunnerAgent.
type RunnerOption func(*RunnerAgent)

// NewRunnerAgent builds a RunnerAgent with a required chat model and optional RunnerOption values.
func NewRunnerAgent(chat model.ToolCallingChatModel, opts ...RunnerOption) *RunnerAgent {
	a := &RunnerAgent{ChatModel: chat}
	for _, o := range opts {
		if o != nil {
			o(a)
		}
	}
	return a
}

// WithSystemInstructions sets the system prompt prepended as a system message.
func WithSystemInstructions(s string) RunnerOption {
	return func(a *RunnerAgent) {
		a.SystemInstructions = s
	}
}

// WithName sets an agent display / identity name (e.g. for logging).
func WithName(name string) RunnerOption {
	return func(a *RunnerAgent) {
		a.Name = name
	}
}

// WithModelName records the model identifier for metrics/observability.
// This is the default; RuntimeRequest.Options.Model overrides it per-request.
func WithModelName(m string) RunnerOption {
	return func(a *RunnerAgent) {
		a.ModelName = m
	}
}

// WithToolInfos registers OpenAI-style tool definitions passed to the chat model.
func WithToolInfos(infos []*model.ToolInfo) RunnerOption {
	return func(a *RunnerAgent) {
		a.ToolInfos = infos
	}
}

// WithExecutor sets the fallback tool executor (see loopforge/pkg/tool).
func WithExecutor(ex tool.ToolExecutor) RunnerOption {
	return func(a *RunnerAgent) {
		a.Executor = ex
	}
}

// WithMaxSteps sets the default max loop steps (Run may still override from request options).
func WithMaxSteps(n int) RunnerOption {
	return func(a *RunnerAgent) {
		a.MaxSteps = n
	}
}

// WithCallOptions appends model call options applied on every Generate (e.g. temperature).
func WithCallOptions(opts ...model.CallOption) RunnerOption {
	return func(a *RunnerAgent) {
		a.CallOptions = append(a.CallOptions, opts...)
	}
}

// WithExtraTools appends additional tool definitions that are sent to the model
// but skipped during tool.ValidateBindings (they carry no Handle). Used by
// orchestrators to inject control-flow tools such as transfer_to_{name}.
func WithExtraTools(tools []*model.ToolInfo) RunnerOption {
	return func(a *RunnerAgent) {
		a.ExtraTools = tools
	}
}

// WithToolInterceptor sets a callback checked before each tool execution. If
// the callback returns true, RunLoop returns an *InterceptedCall instead of
// invoking the tool.
func WithToolInterceptor(fn ToolInterceptor) RunnerOption {
	return func(a *RunnerAgent) {
		a.ToolInterceptor = fn
	}
}

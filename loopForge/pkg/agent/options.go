package agent

import (
	"loopforge/pkg/model"
	"loopforge/pkg/mcp/cfg"
	"loopforge/pkg/tool"
)

// Option configures an Agent when passed to New.
type Option func(*Agent)

// New builds an Agent with a required chat model and optional Option values.
func New(chat model.ToolCallingChatModel, opts ...Option) *Agent {
	a := &Agent{ChatModel: chat}
	for _, o := range opts {
		if o != nil {
			o(a)
		}
	}
	return a
}

// WithDescription sets a human-readable summary used in transfer tool
// descriptions when this agent is a handoff target.
func WithDescription(d string) Option {
	return func(a *Agent) {
		a.Description = d
	}
}

// WithSystemInstructions sets the system prompt prepended as a system message.
func WithSystemInstructions(s string) Option {
	return func(a *Agent) {
		a.SystemInstructions = s
	}
}

// WithName sets an agent display / identity name (e.g. for logging).
func WithName(name string) Option {
	return func(a *Agent) {
		a.Name = name
	}
}

// WithModelName records the model identifier for metrics/observability.
// This is the default; RuntimeRequest.Options.Model overrides it per-request.
func WithModelName(m string) Option {
	return func(a *Agent) {
		a.ModelName = m
	}
}

// WithToolInfos registers OpenAI-style tool definitions passed to the chat model.
func WithToolInfos(infos []*model.ToolInfo) Option {
	return func(a *Agent) {
		a.ToolInfos = infos
	}
}

// WithMCPServerProfiles sets MCP server connection profiles. On each Run, the agent
// discovers tools (tools/list) and merges them after ToolInfos; use ToolPrefix on each
// profile to avoid name clashes. Sessions are released when the run finishes.
func WithMCPServerProfiles(profiles ...cfg.MCPServerProfile) Option {
	return func(a *Agent) {
		if len(profiles) == 0 {
			a.MCPServerProfiles = nil
			return
		}
		a.MCPServerProfiles = append([]cfg.MCPServerProfile(nil), profiles...)
	}
}

// WithExecutor sets the fallback tool executor (see loopforge/pkg/tool).
func WithExecutor(ex tool.ToolExecutor) Option {
	return func(a *Agent) {
		a.Executor = ex
	}
}

// WithMaxSteps sets the default max loop steps (Run may still override from request options).
func WithMaxSteps(n int) Option {
	return func(a *Agent) {
		a.MaxSteps = n
	}
}

// WithCallOptions appends model call options applied on every Generate (e.g. temperature).
func WithCallOptions(opts ...model.CallOption) Option {
	return func(a *Agent) {
		a.CallOptions = append(a.CallOptions, opts...)
	}
}

// WithExtraTools appends additional tool definitions that are sent to the model
// but skipped during tool.ValidateBindings (they carry no Handle). Used by
// orchestrators to inject control-flow tools such as transfer_to_{name}.
func WithExtraTools(tools []*model.ToolInfo) Option {
	return func(a *Agent) {
		a.ExtraTools = tools
	}
}

// WithToolInterceptor sets a callback checked before each tool execution. If
// the callback returns true, RunLoop returns an *InterceptedCall instead of
// invoking the tool.
func WithToolInterceptor(fn ToolInterceptor) Option {
	return func(a *Agent) {
		a.ToolInterceptor = fn
	}
}

// WithVariable enables var_set and injects visitable variables into the system
// prompt as a [Variables] block before each model call.
func WithVariable() Option {
	return func(a *Agent) {
		a.Variable = true
	}
}

// WithSystemPromptBuilder sets an optional callback that composes the system
// prompt before {{name}} substitution from VarStore.
func WithSystemPromptBuilder(fn SystemPromptBuilder) Option {
	return func(a *Agent) {
		a.SystemPromptBuilder = fn
	}
}

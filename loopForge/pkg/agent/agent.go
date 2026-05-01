package agent

import (
	"context"
	"sync"
	"time"

	"github.com/TasoHower/Rei/loopForge/pkg/mcp/cfg"
	"github.com/TasoHower/Rei/loopForge/pkg/model"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/exchange"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/outcome"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/request"
	"github.com/TasoHower/Rei/loopForge/pkg/skill"
	"github.com/TasoHower/Rei/loopForge/pkg/tool"
	"github.com/TasoHower/Rei/loopForge/pkg/variable"
)

// ToolInterceptor, when set on an Agent, is called before executing each
// tool call. If it returns true the tool call is intercepted: RunLoop returns
// an *InterceptedCall instead of invoking the tool through tool.Invoke.
type ToolInterceptor func(tc model.ToolCallPart) bool

// InterceptedCall is returned from RunLoop when a ToolInterceptor matches a
// tool call. It carries the intercepted call, the conversation so far, and
// accumulated metrics for the current agent's portion of the run.
type InterceptedCall struct {
	ToolCall    model.ToolCallPart
	Msgs        []*model.Message
	Metrics     outcome.RunMetrics
	VarSnapshot *variable.StoreSnapshot // optional: store state at transfer intercept
}

// LoopState carries accumulated context from previous agents in an orchestrated
// multi-agent run. Pass nil for standalone (non-orchestrated) usage.
type LoopState struct {
	AccumulatedMetrics outcome.RunMetrics
	TransferChain      []string
	SuppressBookends   bool // skip Start/Question (already emitted by first agent)
	// VarStore is shared across transfer hops when set by the Runner.
	VarStore *variable.VarStore
	// ExtraSkills holds dynamically loaded skills (e.g. via load_skill tool) for this run.
	ExtraSkills []skill.SkillSpec
	// CurrentRunRef, when set by the Runner, identifies this loop for spawn depth and RunID.
	CurrentRunRef *exchange.RunRef
	// asyncWG tracks background spawn tasks that should finish before QueryEnd.
	asyncWG sync.WaitGroup
	// asyncSpawnResults collects final text from async spawn children. After
	// WaitAsyncSpawns, RunLoop reads these and feeds them back to the LLM.
	asyncSpawnResults []string
	asyncSpawnMu      sync.Mutex
}

// AddAsyncSpawn increments the async child task counter.
func (s *LoopState) AddAsyncSpawn() {
	if s == nil {
		return
	}
	s.asyncWG.Add(1)
}

// DoneAsyncSpawn decrements the async child task counter.
func (s *LoopState) DoneAsyncSpawn() {
	if s == nil {
		return
	}
	s.asyncWG.Done()
}

// WaitAsyncSpawns blocks until all async child tasks complete or ctx is canceled.
func (s *LoopState) WaitAsyncSpawns(ctx context.Context) {
	if s == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.asyncWG.Wait()
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// CollectAsyncSpawnResult stores the final text of a completed async spawn child.
func (s *LoopState) CollectAsyncSpawnResult(text string) {
	if s == nil || text == "" {
		return
	}
	s.asyncSpawnMu.Lock()
	s.asyncSpawnResults = append(s.asyncSpawnResults, text)
	s.asyncSpawnMu.Unlock()
}

// FlushAsyncSpawnResults returns and clears collected async spawn results.
func (s *LoopState) FlushAsyncSpawnResults() []string {
	if s == nil {
		return nil
	}
	s.asyncSpawnMu.Lock()
	defer s.asyncSpawnMu.Unlock()
	r := s.asyncSpawnResults
	s.asyncSpawnResults = nil
	return r
}

// Agent is a concrete Runnable that drives a tool-calling loop using pkg/model.
// Model access (HTTP/SDK provider wiring) stays in pkg/model adapters.
//
// Function calling: register ToolInfos (schema for the model). For each tool,
// set ToolInfo.Handle to bind the local Go function, and/or set Executor as a
// fallback (see loopforge/pkg/tool).
//
// Transfer (handoff): call AddHandoff to register other Agent instances as
// allowed transfer targets. Use Runner (pkg/runner.NewRunner) to execute the
// agent graph — it automatically handles the transfer loop and per-run cloning.
//
// MCP: set MCPServerProfiles (via [WithMCPServerProfiles]) so each [RunLoop]
// bootstraps tools from tools/list and merges them after ToolInfos; sessions are
// closed when the run ends. Alternatively use [AttachMCP] + [WithToolInfos] for
// explicit control.
type Agent struct {
	Name               string
	Description        string // human-readable summary; surfaced in transfer tool descriptions
	ModelName          string
	SystemInstructions string
	ChatModel          model.ToolCallingChatModel
	ToolInfos          []*model.ToolInfo
	Executor           tool.ToolExecutor

	// MCPServerProfiles lists MCP servers to connect on each run. When non-empty,
	// RunLoop calls [loopforge/pkg/mcp.BootstrapToolInfos] with the request context
	// and appends discovered tools (prefixed names, MCP Handles) after ToolInfos.
	MCPServerProfiles []cfg.MCPServerProfile `json:"-"`

	mcpStop      func()
	mcpToolInfos []*model.ToolInfo `json:"-"`

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

	// Variable enables var_set and [Variables] prompt injection for this agent.
	Variable bool

	// SystemPromptBuilder optionally composes the system prompt each step.
	// If nil, RunLoop uses the default composition strategy.
	SystemPromptBuilder SystemPromptBuilder

	// SkillNames lists logical skill names to inject (order is preserved). Requires SkillRegistry.
	SkillNames []string
	// SkillRegistry resolves SkillNames to loaded SKILL.md specs. Read-only after LoadFromPaths.
	SkillRegistry *skill.SkillRegistry `json:"-"`
	// SkillShellTool registers execute_shell_script when true and at least one skill is resolved.
	SkillShellTool bool
	// SkillShellTimeout caps execute_shell_script runs. Zero uses the skill package default.
	SkillShellTimeout time.Duration
	// LoadSkillTool registers load_skill when true (requires SkillRegistry). Default false.
	LoadSkillTool bool

	// SpawnEnabled, when true and [ChildAgentBuilder] is set, may register `spawn_subagent` for this run.
	SpawnEnabled bool
	// ChildAgentBuilder materializes a child [Agent] from a [SpawnSpec] (e.g. Clone and tune).
	ChildAgentBuilder func(*exchange.SpawnSpec) *Agent
	// Spawner runs child loops; if nil, [RunLoop] uses a [defaultSpawner] built from this agent.
	Spawner Spawner

	handoffs []*Agent
}

// AddHandoff registers one or more agents as allowed transfer (handoff) targets.
// The Runner reads these at runtime to build transfer_to_{name} tools.
func (a *Agent) AddHandoff(targets ...*Agent) {
	a.handoffs = append(a.handoffs, targets...)
}

// Handoffs returns the registered transfer targets.
func (a *Agent) Handoffs() []*Agent {
	return a.handoffs
}

// Clone creates a shallow copy of the Agent with independent slice headers.
// The elements themselves (e.g. *ToolInfo, *Agent in handoffs) are shared —
// they are treated as immutable templates.
func (a *Agent) Clone() *Agent {
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
		c.handoffs = make([]*Agent, len(a.handoffs))
		copy(c.handoffs, a.handoffs)
	}
	if a.MCPServerProfiles != nil {
		c.MCPServerProfiles = make([]cfg.MCPServerProfile, len(a.MCPServerProfiles))
		copy(c.MCPServerProfiles, a.MCPServerProfiles)
	}
	if a.SkillNames != nil {
		c.SkillNames = make([]string, len(a.SkillNames))
		copy(c.SkillNames, a.SkillNames)
	}
	c.SkillRegistry = a.SkillRegistry
	c.SkillShellTool = a.SkillShellTool
	c.SkillShellTimeout = a.SkillShellTimeout
	c.LoadSkillTool = a.LoadSkillTool
	c.mcpStop = nil
	c.mcpToolInfos = nil
	c.Variable = a.Variable
	c.SystemPromptBuilder = a.SystemPromptBuilder
	c.SpawnEnabled = a.SpawnEnabled
	c.ChildAgentBuilder = a.ChildAgentBuilder
	c.Spawner = a.Spawner
	return &c
}

var _ Runnable = (*Agent)(nil)

// Run starts the agent loop in a goroutine and returns a channel that streams
// RuntimeEvents. The channel is closed when the run finishes.
func (a *Agent) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
	ch := make(chan *event.RuntimeEvent, 128)

	go func() {
		defer close(ch)
		a.RunLoop(ctx, req, ch, nil, nil)
	}()

	return ch
}

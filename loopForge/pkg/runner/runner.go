package runner

import (
	"context"
	"fmt"
	"sync"

	"loopforge/internal/defaults"
	"loopforge/internal/engine"
	"loopforge/pkg/agent"
	"loopforge/pkg/log"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/budget"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/exchange"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/skill"
	"loopforge/pkg/variable"
)

const defaultMaxTransfers = 10

// Runner is the top-level execution entry point for an agent graph. It manages
// the runtime lifecycle including transfer orchestration, metrics tracking, and
// concurrency isolation (per-run Clone of agent templates).
//
// For single-agent use without transfers, Runner works identically to calling
// RunnerAgent.Run directly.
type Runner struct {
	entryAgent   *agent.Agent
	maxTransfers int
	logger       log.Logger
	varStore     *variable.VarStore

	skillRegistry *skill.SkillRegistry
	skillPaths    []string
	pathRegMu     sync.Mutex
	pathLoadedReg *skill.SkillRegistry
	pathLoadErr   error

	// spawnConfig overrides the default RunState and LoopPolicy for spawn tests.
	spawnMaxDepth       *int    // nil = use req.Options.SpawnMaxDepth
	maxConcurrentSpawns int     // 0 = default
	budgetTokens        int64   // 0 = default (unlimited)
	budgetUSD           float64 // 0 = default (unlimited)
}

var _ agent.Runnable = (*Runner)(nil)

// RunOption configures a Runner when passed to NewRunner.
type RunOption func(*Runner)

// NewRunner creates a Runner that starts execution from entryAgent.
// If entryAgent.ChatModel is nil but LARK_API_KEY (or DOUBAO_API_KEY / ARK_API_KEY)
// is set in the environment, Run wires a default LarkChatModel before each hop.
func NewRunner(entryAgent *agent.Agent, opts ...RunOption) *Runner {
	r := &Runner{
		entryAgent:   entryAgent,
		maxTransfers: defaultMaxTransfers,
		logger:       log.Default(),
	}
	for _, o := range opts {
		if o != nil {
			o(r)
		}
	}
	return r
}

// WithLogger sets a structured logger for the Runner. By default the Runner
// uses log.Default() (slog-backed). Pass log.Nop() to silence, or
// log.FromSugared(zapLogger.Sugar()) for zap.
func WithLogger(l log.Logger) RunOption {
	return func(r *Runner) {
		if l != nil {
			r.logger = l
		}
	}
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

// WithVarStore sets the shared variable store for the run (transfer-safe).
// If nil is passed, the option is ignored and a new store is created per run.
func WithVarStore(s *variable.VarStore) RunOption {
	return func(r *Runner) {
		if s != nil {
			r.varStore = s
		}
	}
}

// WithSpawnConfig sets spawn limits for this run.
// maxDepth overrides req.Options.SpawnMaxDepth; zero means use request default.
// maxConcurrentSpawns limits how many child runs can be active at once; zero means unlimited.
func WithSpawnConfig(maxDepth, maxConcurrentSpawns int) RunOption {
	return func(r *Runner) {
		if maxDepth > 0 {
			r.spawnMaxDepth = &maxDepth
		}
		if maxConcurrentSpawns > 0 {
			r.maxConcurrentSpawns = maxConcurrentSpawns
		}
	}
}

// WithBudgetLimits sets tree-wide token/cost budget for the entire run tree.
// Zero means unlimited for that dimension.
func WithBudgetLimits(maxTokens int64, maxUSD float64) RunOption {
	return func(r *Runner) {
		if maxTokens > 0 {
			r.budgetTokens = maxTokens
		}
		if maxUSD > 0 {
			r.budgetUSD = maxUSD
		}
	}
}

// Run starts the agent graph in a goroutine and returns a channel that streams
// RuntimeEvents. If the entry agent has handoff targets, the runner
// automatically manages the transfer loop; otherwise it runs the single agent.
func (r *Runner) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
	// Buffered enough that slow SSE clients do not deadlock the agent on emit
	// (select only drains on ctx.Done otherwise).
	ch := make(chan *event.RuntimeEvent, 128)

	go func() {
		defer close(ch)
		runID := runIDFrom(req)

		// Resolve spawn depth from runner config (if set) or request default
		if r.spawnMaxDepth != nil {
			if req.Options.SpawnMaxDepth == nil || *req.Options.SpawnMaxDepth <= 0 {
				req.Options.SpawnMaxDepth = r.spawnMaxDepth
			}
		}

		// Resolve budget limits from runner config or defaults
		tokensMax := r.budgetTokens
		if tokensMax <= 0 {
			tokensMax = defaults.SpawnBudgetTokenTreeDefault
		}
		usdMax := r.budgetUSD
		if usdMax <= 0 {
			usdMax = defaults.SpawnBudgetUSDTreeDefault
		}

		// Create tree-wide RunState with spawn tracking and budget
		maxConcurrent := r.maxConcurrentSpawns
		if maxConcurrent <= 0 {
			maxConcurrent = defaults.MaxConcurrentSpawnsDefault
		}
		bc := budget.NewBudgetCounter(tokensMax, usdMax)
		policy := engine.NewCustomLoopPolicy(
			defaults.MaxStepsDefault,
			defaults.SpawnMaxDepthDefault,
			maxConcurrent,
			bc,
		)
		runState := &engine.RunState{
			RunID:         runID,
			Depth:         0,
			AllowSpawn:    true,
			Children:      engine.NewChildRegistry(),
			BudgetCounter: policy.BudgetCounter(),
		}

		if len(r.entryAgent.Handoffs()) > 0 {
			r.logger.Debug("run started in transfer mode",
				"entry_agent", r.entryAgent.Name,
				"handoffs", len(r.entryAgent.Handoffs()),
			)
			r.runTransferLoop(ctx, req, ch, runState, policy)
		} else {
			r.logger.Debug("run started in single-agent mode",
				"agent", r.entryAgent.Name,
			)
			store := r.varStore
			if store == nil {
				store = variable.New()
			}
			st := &agent.LoopState{
				VarStore:      store,
				CurrentRunRef: &exchange.RunRef{RunID: runID, Depth: 0},
			}
			exec := r.entryAgent.Clone()
			applyDefaultLarkIfNeeded(exec)
			if err := r.prepareAgent(ctx, exec); err != nil {
				emitRunSetupError(ctx, ch, runID, err.Error())
				return
			}

			// Inject engine-level spawner for ChildRegistry + budget tracking
			maxD := agent.ResolveSpawnMaxDepth(req)
			inner := agent.NewDefaultSpawner(exec, maxD)
			exec.Spawner = engine.NewEngineSpawner(inner, runState)

			exec.RunLoop(ctx, req, ch, nil, st)

			// Cascade cleanup: cancel any lingering children after parent completes
			runState.Terminate(outcome.TerminationCompleted)
		}
	}()

	return ch
}

func (r *Runner) runTransferLoop(ctx context.Context, req *request.RuntimeRequest, ch chan<- *event.RuntimeEvent, runState *engine.RunState, _ engine.LoopPolicy) {
	runID := runIDFrom(req)

	emit := func(step int, payload event.EventPayload) {
		select {
		case ch <- event.Emit(runID, step, payload):
		case <-ctx.Done():
		}
	}

	var runStore *variable.VarStore

	emitError := func(code, msg string) {
		emit(0, &event.ErrorPayload{Code: code, Message: msg})
		oc := &outcome.RuntimeOutcome{
			RunID:       runID,
			Termination: outcome.TerminationError,
		}
		if runStore != nil {
			oc.VarStore = runStore
		}
		emit(0, &event.QueryEndPayload{Outcome: oc})
	}

	if r.entryAgent == nil {
		r.logger.Error("entry agent is nil")
		emitError("invalid_config", "Runner.entryAgent is nil")
		return
	}

	r.logger.Info("transfer loop started",
		"run_id", runID,
		"entry_agent", r.entryAgent.Name,
		"max_transfers", r.maxTransfers,
	)

	runStore = r.varStore
	if runStore == nil {
		runStore = variable.New()
	}

	current := r.entryAgent
	var inheritedMsgs []*model.Message
	accumulated := outcome.RunMetrics{}
	var transferChain []string
	transferCount := 0
	firstAgent := true

	for {
		a := current.Clone()
		applyDefaultLarkIfNeeded(a)
		if err := r.prepareAgent(ctx, a); err != nil {
			emitError("invalid_config", err.Error())
			return
		}

		a.ExtraTools = agent.BuildTransferTools(current)
		a.ToolInterceptor = agent.IsTransferTool
		if p := agent.BuildTransferPrompt(current); p != "" {
			a.SystemInstructions += p
		}

		st := &agent.LoopState{
			AccumulatedMetrics: accumulated,
			TransferChain:      transferChain,
			SuppressBookends:   !firstAgent,
			VarStore:           runStore,
			CurrentRunRef:      &exchange.RunRef{RunID: runID, Depth: 0},
		}

		r.logger.Debug("agent executing",
			"agent", current.Name,
			"transfer_count", transferCount,
		)

		// Inject engine-level spawner for this agent hop
		maxD := agent.ResolveSpawnMaxDepth(req)
		inner := agent.NewDefaultSpawner(a, maxD)
		a.Spawner = engine.NewEngineSpawner(inner, runState)

		result := a.RunLoop(ctx, req, ch, inheritedMsgs, st)

		// Cascade cleanup after each agent hop
		runState.Terminate(outcome.TerminationCompleted)

		if result == nil {
			r.logger.Info("transfer loop completed",
				"run_id", runID,
				"last_agent", current.Name,
				"total_transfers", transferCount,
			)
			return
		}

		transferCount++
		if transferCount > r.maxTransfers {
			r.logger.Warn("max transfers exceeded",
				"run_id", runID,
				"limit", r.maxTransfers,
			)
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

		target := agent.TargetAgent(result.ToolCall.Name)
		reason := agent.ExtractReason(result.ToolCall.Arguments)

		next := findHandoff(current, target)
		if next == nil {
			r.logger.Error("invalid transfer target",
				"run_id", runID,
				"from", current.Name,
				"target", target,
			)
			emitError("invalid_transfer", "target agent not found in handoffs: "+target)
			return
		}

		r.logger.Info("agent transfer",
			"run_id", runID,
			"from", current.Name,
			"to", target,
			"reason", reason,
		)

		transferChain = append(transferChain, current.Name)

		emit(0, &event.AgentTransferPayload{
			Phase:     event.TransferStart,
			FromAgent: current.Name,
			ToAgent:   target,
			Reason:    reason,
		})

		inheritedMsgs = appendSyntheticToolResponsesAfterTransfer(result.Msgs, &result.ToolCall, target)
		current = next
		firstAgent = false
	}
}

func findHandoff(a *agent.Agent, name string) *agent.Agent {
	for _, h := range a.Handoffs() {
		if h.Name == name {
			return h
		}
	}
	return nil
}

// appendSyntheticToolResponsesAfterTransfer appends one role=tool message per
// entry in the last assistant message's ToolCalls. Downstream chat APIs
// (OpenAI-compatible, including DeepSeek) reject histories where an assistant
// message lists tool_calls but the following tool messages do not cover every
// tool_call_id. Transfer intercepts before normal tool execution, so we must
// still emit a synthetic result for each parallel tool call in that turn.
func appendSyntheticToolResponsesAfterTransfer(
	msgs []*model.Message,
	intercepted *model.ToolCallPart,
	targetAgent string,
) []*model.Message {
	if intercepted == nil {
		return msgs
	}
	handoff := fmt.Sprintf("Transfer accepted. The conversation is now handled by %s.", targetAgent)
	placeholder := `{"cancelled":true,"reason":"superseded by agent transfer"}`

	lastIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] != nil && msgs[i].Role == model.RoleAssistant && len(msgs[i].ToolCalls) > 0 {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		return append(msgs, &model.Message{
			Role:       model.RoleTool,
			ToolCallID: intercepted.ID,
			Name:       intercepted.Name,
			Content:    handoff,
		})
	}

	out := msgs
	for _, tc := range msgs[lastIdx].ToolCalls {
		content := placeholder
		if toolCallPartsMatch(&tc, intercepted) {
			content = handoff
		}
		out = append(out, &model.Message{
			Role:       model.RoleTool,
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Content:    content,
		})
	}
	return out
}

func toolCallPartsMatch(a, b *model.ToolCallPart) bool {
	if a == nil || b == nil {
		return false
	}
	if a.ID != "" && b.ID != "" {
		return a.ID == b.ID
	}
	return a.Name == b.Name && a.Arguments == b.Arguments
}

func runIDFrom(req *request.RuntimeRequest) string {
	if req.SessionID != "" {
		return req.SessionID
	}
	return "run"
}

func emitRunSetupError(ctx context.Context, ch chan<- *event.RuntimeEvent, runID, msg string) {
	select {
	case ch <- event.Emit(runID, 0, &event.ErrorPayload{Code: "invalid_config", Message: msg}):
	case <-ctx.Done():
		return
	}
	select {
	case ch <- event.Emit(runID, 0, &event.QueryEndPayload{
		Outcome: &outcome.RuntimeOutcome{
			RunID:       runID,
			Termination: outcome.TerminationError,
		},
	}):
	case <-ctx.Done():
	}
}

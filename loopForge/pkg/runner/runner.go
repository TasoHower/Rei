package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TasoHower/rei/loopForge/internal/defaults"
	"github.com/TasoHower/rei/loopForge/internal/engine"
	"github.com/TasoHower/rei/loopForge/pkg/agent"
	"github.com/TasoHower/rei/loopForge/pkg/log"
	"github.com/TasoHower/rei/loopForge/pkg/model"
	"github.com/TasoHower/rei/loopForge/pkg/plan"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/budget"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/exchange"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/outcome"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/request"
	"github.com/TasoHower/rei/loopForge/pkg/skill"
	"github.com/TasoHower/rei/loopForge/pkg/variable"
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

	// planEnabled, when true, overrides req.RunMode to RunModePlan.
	planEnabled bool
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

// WithPlanEnabled enables plan mode. When set, the entry agent evaluates the
// query's complexity and may call plan_generate to create a structured plan.
// Plan steps are tracked via plan_update and injected into the system prompt.
// This overrides req.RunMode to RunModePlan.
func WithPlanEnabled() RunOption {
	return func(r *Runner) {
		r.planEnabled = true
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

		if req.RunMode == request.RunModePlan || r.planEnabled {
			r.logger.Debug("run started in plan mode",
				"entry_agent", r.entryAgent.Name,
			)
			r.runPlanLoop(ctx, req, ch, runState, policy)
		} else if len(r.entryAgent.Handoffs()) > 0 {
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
			applyDefaultOpenAIIfNeeded(exec)
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
		applyDefaultOpenAIIfNeeded(a)
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

// ─── Plan Mode ────────────────────────────────────────────────────────────

// runPlanLoop executes a 3-phase plan-driven execution:
//
//	Phase 1: Entry agent analyzes the query. If complex, calls plan_generate.
//	Phase 2: Plan steps are injected into SystemInstructions. Agent executes
//	         steps, using plan_update to track progress. Transfer is supported.
//	Phase 3: Remaining steps are auto-completed; final events emitted.
func (r *Runner) runPlanLoop(
	ctx context.Context,
	req *request.RuntimeRequest,
	ch chan<- *event.RuntimeEvent,
	runState *engine.RunState,
	policy engine.LoopPolicy,
) {
	runID := runIDFrom(req)
	planState := plan.NewPlanState()

	// Shared helpers
	emit := func(step int, payload event.EventPayload) {
		select {
		case ch <- event.Emit(runID, step, payload):
		case <-ctx.Done():
		}
	}
	emitError := func(step int, code, msg string) {
		emit(step, &event.ErrorPayload{Code: code, Message: msg})
	}

	baseStore := r.varStore
	if baseStore == nil {
		baseStore = variable.New()
	}

	// ── Phase 1: Plan Generation ──────────────────────────────────────────
	r.logger.Debug("plan mode: phase 1 — planning")
	entry := r.entryAgent.Clone()
	applyDefaultLarkIfNeeded(entry)
	applyDefaultOpenAIIfNeeded(entry)
	if err := r.prepareAgent(ctx, entry); err != nil {
		emitError(0, "invalid_config", err.Error())
		return
	}

	// Inject engine-level spawner
	maxD := agent.ResolveSpawnMaxDepth(req)
	inner := agent.NewDefaultSpawner(entry, maxD)
	entry.Spawner = engine.NewEngineSpawner(inner, runState)

	// Inject plan decision rules into SystemInstructions (ensures agent knows
	// when to call plan_generate, even if the caller didn't set it).
	if rules := planDecisionRulesPrompt(); rules != "" {
		entry.SystemInstructions += rules
	}

	// Inject plan_generate tool via ExtraTools
	entry.ExtraTools = append(entry.ExtraTools, planGenerateToolInfo())

	// Track whether plan_generate was intercepted
	var planGenerated bool

	entry.ToolInterceptor = func(tc model.ToolCallPart) bool {
		if tc.Name == "plan_generate" {
			var params struct {
				Title       string          `json:"title"`
				Description string          `json:"description,omitempty"`
				Steps       []plan.PlanStep `json:"steps"`
			}
			if err := json.Unmarshal([]byte(tc.Arguments), &params); err != nil {
				return false // let it fail through normal tool execution
			}
			if len(params.Steps) == 0 {
				return false
			}

			// Normalize step fields
			for i := range params.Steps {
				if params.Steps[i].ID == "" {
					params.Steps[i].Status = plan.PlanStepPending
					continue
				}
				if params.Steps[i].AssignedTo == "" {
					params.Steps[i].AssignedTo = "self"
				}
				params.Steps[i].Status = plan.PlanStepPending
			}

			p := &plan.Plan{
				ID:          fmt.Sprintf("plan-%d", time.Now().UnixNano()),
				Title:       params.Title,
				Description: params.Description,
				Steps:       params.Steps,
				CreatedAt:   time.Now(),
			}

			_ = planState.SetPlan(p)
			planGenerated = true
			return true // intercept
		}
		return false
	}

	st := &agent.LoopState{
		VarStore:      baseStore,
		CurrentRunRef: &exchange.RunRef{RunID: runID, Depth: 0},
	}

	_ = entry.RunLoop(ctx, req, ch, nil, st)

	if planGenerated {
		emit(0, &event.PlanGeneratedPayload{Plan: planState.Plan()})
	} else {
		// No plan generated — simple query, finish normally
		runState.Terminate(outcome.TerminationCompleted)
		return
	}

	// ── Phase 2: Plan Execution ──────────────────────────────────────────
	r.logger.Debug("plan mode: phase 2 — execution",
		"plan", planState.Plan().Title,
		"steps", len(planState.Plan().Steps),
	)

	current := r.entryAgent.Clone()
	applyDefaultLarkIfNeeded(current)
	applyDefaultOpenAIIfNeeded(current)
	if err := r.prepareAgent(ctx, current); err != nil {
		emitError(0, "invalid_config", err.Error())
		return
	}

	// Inject engine-level spawner
	inner2 := agent.NewDefaultSpawner(current, maxD)
	current.Spawner = engine.NewEngineSpawner(inner2, runState)

	// Inject plan_update tool
	planUpdateHandler := makePlanUpdateHandler(planState, ch, runID)
	current.ExtraTools = append(current.ExtraTools, planUpdateToolInfo(planUpdateHandler))
	// No tool interceptor in Phase 2 — plan_update runs as a normal tool

	// Inject Plan into LoopState
	st2 := &agent.LoopState{
		VarStore:      baseStore,
		CurrentRunRef: &exchange.RunRef{RunID: runID, Depth: 0},
		Plan:          planState.Plan(),
	}

	// Append [Plan] block to SystemInstructions
	if frag := planPromptFragment(planState); frag != "" {
		current.SystemInstructions += frag
	}

	// Set a low MaxSteps to prevent infinite loops if the agent forgets to
	// call plan_update. Each step in the plan gets at most 3 LLM rounds.
	stepsLeft := len(planState.Plan().Steps) * 3
	if current.MaxSteps <= 0 || current.MaxSteps > stepsLeft {
		current.MaxSteps = stepsLeft
	}

	// Execute agent loop (through the spawner-injected agent).
	// Transfer events work naturally via the existing transfer interceptor.
	current.RunLoop(ctx, req, ch, nil, st2)

	// Auto-complete any remaining pending steps in the plan.
	// The agent finished its run; mark incomplete steps as skipped.
	// This prevents stale "pending" steps from confusing observers.
	if currentPlan := planState.Plan(); currentPlan != nil {
		for _, s := range currentPlan.Steps {
			if s.Status == plan.PlanStepPending || s.Status == plan.PlanStepInProgress {
				_ = planState.UpdateStepStatus(s.ID, plan.PlanStepSkipped,
					"step not completed before agent finished")
			}
		}
	}

	// ── Phase 3: Finalize ────────────────────────────────────────────────
	r.logger.Debug("plan mode: phase 3 — finalize",
		"plan", planState.Plan().Title,
	)

	runState.Terminate(outcome.TerminationCompleted)
}

// ─── Plan Tool Helpers ──────────────────────────────────────────────────

// planGenerateToolInfo returns the ToolInfo for the plan_generate tool.
func planGenerateToolInfo() *model.ToolInfo {
	return &model.ToolInfo{
		Name:        "plan_generate",
		Description: `Generate a structured execution plan for a complex task. Call this ONLY when you determine the user's query genuinely needs multiple coordinated steps or multi-agent coordination. For simple queries (single step), do NOT call this tool—just answer directly.`,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Short plan title summarizing the overall task",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional: overall plan description or context",
				},
				"steps": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id": map[string]interface{}{
								"type":        "string",
								"description": "Unique step identifier (e.g. step-1)",
							},
							"description": map[string]interface{}{
								"type":        "string",
								"description": "What this step should accomplish",
							},
							"assigned_to": map[string]interface{}{
								"type":        "string",
								"description": `"self" = current agent, "spawn" = spawn sub-agent, "transfer:AgentName" = transfer to named agent, "team:TeamName" = reserved for future agent teams`,
							},
							"depends_on": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "string",
								},
								"description": "Step IDs this step depends on (optional, for ordering)",
							},
						},
						"required": []string{"id", "description"},
					},
				},
			},
			"required": []string{"title", "steps"},
		},
	}
}

// planUpdateToolInfo returns the ToolInfo for the plan_update tool.
func planUpdateToolInfo(handler model.ToolCallHandler) *model.ToolInfo {
	return &model.ToolInfo{
		Name:        "plan_update",
		Description: `Update the status and/or result of a plan step. Call this when you start working on a step (mark in_progress) or finish it (mark completed/failed).`,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "Step ID to update (e.g. step-1)",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"in_progress", "completed", "failed", "skipped"},
					"description": "New step status",
				},
				"result": map[string]interface{}{
					"type":        "string",
					"description": "Optional: description of what was accomplished or error details",
				},
			},
			"required": []string{"step_id", "status"},
		},
		Handle: handler,
	}
}

// makePlanUpdateHandler creates the Handle closure for plan_update.
// It updates PlanState and emits plan_step_start / plan_step_end events.
func makePlanUpdateHandler(
	ps *plan.PlanState,
	ch chan<- *event.RuntimeEvent,
	runID string,
) model.ToolCallHandler {
	return func(ctx context.Context, argsJSON string) (string, error) {
		var params struct {
			StepID string              `json:"step_id"`
			Status plan.PlanStepStatus `json:"status"`
			Result string              `json:"result,omitempty"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return "", fmt.Errorf("plan_update: invalid arguments: %w", err)
		}

		// Validate status
		switch params.Status {
		case plan.PlanStepInProgress, plan.PlanStepCompleted,
			plan.PlanStepFailed, plan.PlanStepSkipped:
		default:
			return "", fmt.Errorf("plan_update: invalid status %q", params.Status)
		}

		// Get previous state for event emission
		before := ps.Snapshot()

		if err := ps.UpdateStepStatus(params.StepID, params.Status, params.Result); err != nil {
			return "", err
		}

		// Emit events based on transition
		after := ps.Plan()
		if before != nil && after != nil {
			for _, bs := range before.Steps {
				if bs.ID != params.StepID {
					continue
				}
				for _, as := range after.Steps {
					if as.ID != params.StepID {
						continue
					}
					if as.Status != bs.Status {
						if as.Status == plan.PlanStepInProgress {
							select {
							case ch <- event.Emit(runID, 0, &event.PlanStepStartPayload{
								StepID:      as.ID,
								Description: as.Description,
								AssignedTo:  as.AssignedTo,
							}):
							case <-ctx.Done():
							}
						}
						if as.Status == plan.PlanStepCompleted ||
							as.Status == plan.PlanStepFailed ||
							as.Status == plan.PlanStepSkipped {
							select {
							case ch <- event.Emit(runID, 0, &event.PlanStepEndPayload{
								StepID: as.ID,
								Status: as.Status,
								Result: as.Result,
							}):
							case <-ctx.Done():
							}
						}
					}
					break
				}
				break
			}
		}

		return fmt.Sprintf("Step %q updated to %s.", params.StepID, params.Status), nil
	}
}

// planDecisionRulesPrompt returns the prompt fragment that tells the agent
// when to (and when not to) call plan_generate.
func planDecisionRulesPrompt() string {
	return `
## Plan Decision Rules

### Call plan_generate when ANY of these is true:
- Task needs 3+ sequential steps where later steps depend on earlier results
- Task needs multi-agent coordination (transfer to different agents)
- Task has independent subtasks that can run in parallel (spawn)
- Steps require different tools or knowledge domains
- Intermediate validation is needed before proceeding
- Number of steps is uncertain — needs iterative execution

### Do NOT call plan_generate when ANY of these is true:
- Single tool call will suffice
- Direct answer without external tools or data
- Conversational follow-up or clarification
- Simple instruction within your own capabilities
- User is correcting or refining within an existing plan

When in doubt, prefer NOT to plan — try direct execution first.
If execution shows the task is more complex, you can plan in the next round.

## Reasoning Discipline (Strict)
- Your reasoning is internal and must NEVER be shown to the user.
- Your entire response must be exactly one tool call (function) OR one final message. Never mix text narration with a tool call.
- If you catch yourself about to repeat a thought or an already executed action, stop immediately and take a different action (move to the next step, or output the final answer).
- Be decisive: calling a tool is ALWAYS better than continuing to deliberate. Once you know what to do, do it silently.

## Execution Flow (No Narration)
When executing a plan, follow this rigid sequence without any surrounding text:

1. Call plan_update(step_id, "in_progress") — this is your entire message.
2. Immediately call the required tool (e.g., divide, multiply). Again, this single call is your whole response.
3. After the tool returns, call plan_update(step_id, "completed", result) to mark it done.
4. Move straight to the next step. Do NOT re-analyze completed steps. Never output a sentence like "Now step-X: …"; simply invoke the function.

## Anti-Loop Clause
- If you find yourself on the same step for more than two turns without progress, mark the step as completed with the best available result or report an issue.
- Never repeat the same plan_update or tool call for the same step. If a call fails, you may retry once with a different parameter, but not the exact same request.
- After all steps are done, output only the final answer. No summary narration like "All steps completed" — just the result.`
}

// planPromptFragment renders the [Plan] block injected into SystemInstructions.
func planPromptFragment(ps *plan.PlanState) string {
	p := ps.Plan()
	if p == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n[Plan]\n")
	b.WriteString(fmt.Sprintf("Title: %s\n", p.Title))
	if p.Description != "" {
		b.WriteString(fmt.Sprintf("Description: %s\n", p.Description))
	}
	b.WriteString("\n| Step | Description | Assigned To | Status |\n")
	b.WriteString("|------|-------------|-------------|--------|\n")
	for _, s := range p.Steps {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", s.ID, s.Description, s.AssignedTo, s.Status))
	}
	b.WriteString("\n\n## Reasoning Discipline\n")
	b.WriteString("- Limit reasoning to 2-4 sentences per step. Be concise.\n")
	b.WriteString("- After reasoning, IMMEDIATELY call a tool — do not keep thinking.\n")
	b.WriteString("- If you catch yourself rephrasing the same idea, STOP and call a tool.\n")
	b.WriteString("- Calling a tool is ALWAYS better than continuing to reason.\n")
	b.WriteString("\n## Execution Rules\n")
	b.WriteString("- Each step: call plan_update(step_id, \"in_progress\") → brief reasoning → call tools → plan_update(step_id, \"completed\", result).\n")
	b.WriteString("- Move to the next step immediately after marking one complete. Do not re-analyze finished steps.\n")
	b.WriteString("- Be fast. Efficiency matters.")
	return b.String()
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

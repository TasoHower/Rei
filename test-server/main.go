package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/sse"

	"log/slog"

	"github.com/cloudwego/hertz/pkg/common/hlog"

	"github.com/TasoHower/rei/loopForge/pkg/agent"
	"github.com/TasoHower/rei/loopForge/pkg/log"
	"github.com/TasoHower/rei/loopForge/pkg/mcp/cfg"
	"github.com/TasoHower/rei/loopForge/pkg/model"
	deepseekadapter "github.com/TasoHower/rei/loopForge/pkg/model/adapters/deepseek"
	"github.com/TasoHower/rei/loopForge/pkg/runner"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/exchange"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/request"
	"github.com/TasoHower/rei/loopForge/pkg/skill"
	"github.com/TasoHower/rei/loopForge/pkg/variable"
)

const (
	defaultModel        = "deepseek-chat"
	defaultSystemPrompt = `You are the loopForge test assistant (single-agent mode).

Shared variables appear in a [Variables] block at the end of the system prompt. Keys starting with const_ are read-only; never call var_set on them.
Use var_set to write user-requested values for writable keys. When MCP arithmetic tools are available (names may be prefixed), call them for every numeric step; do not compute mentally.

After finishing, reply briefly and confirm what you set or computed.`
	defaultSkillDebugSystemPrompt = `You are the loopForge Skill Runtime debugger (single-agent). Loaded skills appear as markdown sections ## Skill: <name> in the system prompt.
Follow skill text when it applies. Use built-in add/subtract/multiply/divide tools for arithmetic when MCP tools are not available.
Use var_set only when testing the variable store. If execute_shell_script or load_skill are listed, use them only as allowed by the skill and tool descriptions.`
	defaultSpawnSystemPrompt = `You are the loopForge test assistant in spawn demo mode.

**spawn_subagent** launches a sub-agent asynchronously. When you call it:
  1. It returns IMMEDIATELY with {"status":"completed","child_ref":"..."} — the child runs in the background.
  2. Reply to the user briefly after calling spawn_subagent (e.g. "The subtask is running...").
  3. After you reply, the parent loop waits for the child to finish.
  4. When the child completes, the parent receives its result and makes a final LLM call.
  5. Summarize the child's result naturally for the user.

Example:
  User: "calculate 123 * 456 + 789"
  You (call spawn_subagent with task "compute 123*456+789"):
    → returns immediately
  You (reply to user): "Done. The subtask has been launched."
  [parent waits for child]
  You (after receiving child result): "The computation result is: ..."

Call with: {"task": "<concrete subtask>"}

Optional parameters:
  - "system_addendum": extra system instructions for the child
  - "allow_child_spawn": if true, the child may also use spawn_subagent (default false)
  - "max_steps": override max ReAct steps for the child (default 8)

The child cannot see parent variables. Prefer spawn_subagent for self-contained arithmetic.`
)

// ChatRequest is the JSON body for POST /api/chat.
type ChatRequest struct {
	Message      string `json:"message"`
	SessionID    string `json:"session_id,omitempty"`
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Mode         string `json:"mode,omitempty"` // "single" (default), "transfer", or "spawn"

	// MCPMode: "env" (default) uses server env defaults; "off" disables MCP; "custom" uses MCP.
	MCPMode string     `json:"mcp_mode,omitempty"`
	MCP     *MCPFields `json:"mcp,omitempty"`

	// SkillRuntime: when SkillDebug is true, default system prompt targets skill inspection; SkillNames selects registry entries.
	SkillDebug bool     `json:"skill_debug,omitempty"`
	SkillNames []string `json:"skill_names,omitempty"`
	SkillShell bool     `json:"skill_shell,omitempty"`
	SkillLoad  bool     `json:"skill_load,omitempty"`
	ShowSystem bool     `json:"show_system_prompt,omitempty"`

	// SpawnConfig fields for spawn mode testing
	SpawnMaxDepth   *int    `json:"spawn_max_depth,omitempty"`  // max spawn depth (default 2)
	SpawnConcurrent int     `json:"spawn_concurrent,omitempty"` // max concurrent child runs (0 = default)
	BudgetTokens    int64   `json:"budget_tokens,omitempty"`    // tree-wide token budget (0 = unlimited)
	BudgetUSD       float64 `json:"budget_usd,omitempty"`       // tree-wide cost budget USD (0 = unlimited)
}

// SSEEvent is the JSON payload written as SSE data for each RuntimeEvent.
type SSEEvent struct {
	Type string `json:"type"`
	Step int    `json:"step,omitempty"`
	Data any    `json:"data,omitempty"`
}

func (r *ChatRequest) resolveDefaults() {
	if r.APIKey == "" {
		r.APIKey = firstNonEmpty(os.Getenv("DEEPSEEK_API_KEY"))
	}
	if r.BaseURL == "" {
		r.BaseURL = firstNonEmpty(os.Getenv("DEEPSEEK_BASE_URL"), deepseekadapter.DefaultBaseURL)
	}
	r.BaseURL = strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if r.Model == "" {
		r.Model = firstNonEmpty(os.Getenv("DEEPSEEK_MODEL"), defaultModel)
	}
	if r.SystemPrompt == "" {
		if r.SkillDebug {
			r.SystemPrompt = defaultSkillDebugSystemPrompt
		} else {
			if r.Mode == "spawn" {
				r.SystemPrompt = defaultSpawnSystemPrompt
			} else {
				r.SystemPrompt = defaultSystemPrompt
			}
		}
	}
	if r.Mode == "" {
		r.Mode = "single"
	}
}

func trimStringSlice(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (r *ChatRequest) effectiveSkillNames(reg *skill.SkillRegistry) []string {
	names := trimStringSlice(r.SkillNames)
	if len(names) > 0 {
		return names
	}
	if r.SkillDebug && reg != nil {
		if _, err := reg.Get("demo"); err == nil {
			return []string{"demo"}
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func derefInt(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

// --- session-scoped variables (in-memory; demonstrates Materialize + Snapshot) ---

var (
	varSnapMu sync.Mutex
	varSnaps  = make(map[string][]byte) // session_id -> JSON StoreSnapshot
)

func testServerVariableManifest() *variable.Manifest {
	return &variable.Manifest{Specs: []variable.Spec{
		{Key: "const_session_id", Description: "当前会话 ID（只读）"},
		{Key: "const_chat_mode", Description: "聊天模式 single / transfer（只读）"},
		{Key: "session_note", Description: "会话备注（var_set 可写，多轮保留）"},
		{Key: "user_goal", Description: "用户本回合目标（简短概括）"},
	}}
}

func materializeVarStore(sessionID, chatMode string) *variable.VarStore {
	var persisted *variable.StoreSnapshot
	varSnapMu.Lock()
	raw := varSnaps[sessionID]
	varSnapMu.Unlock()
	if len(raw) > 0 {
		var snap variable.StoreSnapshot
		if json.Unmarshal(raw, &snap) == nil {
			persisted = &snap
		}
	}
	return variable.Materialize(
		testServerVariableManifest(),
		persisted,
		map[string]any{
			"const_session_id": sessionID,
			"const_chat_mode":  chatMode,
		},
	)
}

func persistVarSnapshot(sessionID string, store *variable.VarStore) {
	if store == nil {
		return
	}
	raw, err := json.Marshal(store.Snapshot())
	if err != nil {
		return
	}
	varSnapMu.Lock()
	varSnaps[sessionID] = raw
	varSnapMu.Unlock()
	slog.Default().Debug("variable snapshot persisted", "session_id", sessionID, "bytes", len(raw))
}

func parseBinaryArgs(raw string) (a, b float64, err error) {
	var args struct {
		A float64 `json:"a"`
		B float64 `json:"b"`
	}
	if err = json.Unmarshal([]byte(raw), &args); err != nil {
		return 0, 0, err
	}
	return args.A, args.B, nil
}

func mathToolInfos() []*model.ToolInfo {
	binarySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "number"},
			"b": map[string]interface{}{"type": "number"},
		},
		"required": []string{"a", "b"},
	}

	makeHandler := func(name string, op func(a, b float64) (float64, error)) model.ToolCallHandler {
		return func(ctx context.Context, argsJSON string) (string, error) {
			a, b, err := parseBinaryArgs(argsJSON)
			if err != nil {
				return "", fmt.Errorf("%s: %w", name, err)
			}
			result, err := op(a, b)
			if err != nil {
				return "", fmt.Errorf("%s: %w", name, err)
			}
			return fmt.Sprintf("%.6g", result), nil
		}
	}

	return []*model.ToolInfo{
		{
			Name:        "add",
			Description: "Return a + b.",
			Parameters:  binarySchema,
			Handle:      makeHandler("add", func(a, b float64) (float64, error) { return a + b, nil }),
		},
		{
			Name:        "subtract",
			Description: "Return a - b.",
			Parameters:  binarySchema,
			Handle:      makeHandler("subtract", func(a, b float64) (float64, error) { return a - b, nil }),
		},
		{
			Name:        "multiply",
			Description: "Return a * b.",
			Parameters:  binarySchema,
			Handle:      makeHandler("multiply", func(a, b float64) (float64, error) { return a * b, nil }),
		},
		{
			Name:        "divide",
			Description: "Return a / b. Returns error when b is zero.",
			Parameters:  binarySchema,
			Handle: makeHandler("divide", func(a, b float64) (float64, error) {
				if b == 0 {
					return 0, fmt.Errorf("division by zero")
				}
				return math.Round(a/b*1e6) / 1e6, nil
			}),
		},
	}
}

// --- agent builders ---

func buildSingleAgent(req *ChatRequest, mcpProf []cfg.MCPServerProfile, mcpSuffix string, reg *skill.SkillRegistry) *agent.Agent {
	sys := req.SystemPrompt
	if mcpSuffix != "" {
		sys += mcpSuffix
	}
	opts := []agent.Option{
		agent.WithName("test-server-agent"),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(12),
		agent.WithSystemInstructions(sys),
		agent.WithCallOptions(model.WithTemperature(0.1)),
		agent.WithVariable(),
		agent.WithToolInfos(mathToolInfos()),
	}
	if len(mcpProf) > 0 {
		opts = append(opts, agent.WithMCPServerProfiles(mcpProf...))
	}
	names := req.effectiveSkillNames(reg)
	if reg != nil && len(names) > 0 {
		opts = append(opts, agent.WithSkills(reg, names...))
	}
	if req.SkillShell {
		opts = append(opts, agent.WithSkillShellTool(true))
	}
	if req.SkillLoad {
		opts = append(opts, agent.WithLoadSkillTool(true))
	}
	a := agent.New(nil, opts...)
	runner.ApplyDeepSeekFromConfig(a, req.APIKey, req.BaseURL, req.Model)
	return a
}

// buildSpawnAgent is the spawn demo entry: no MCP (built-in add/subtract/multiply/divide only);
// parent may call spawn_subagent; child runs in a separate RunLoop.
// Children default to AllowChildSpawn=true for nested spawn testing.
func buildSpawnAgent(req *ChatRequest, reg *skill.SkillRegistry) *agent.Agent {
	subSys := `You are a temporary sub-agent for a single subtask. Use only the built-in tools add, subtract, multiply, and divide for all numeric work. Keep the final reply to one or two short sentences.`
	childOpts := []agent.Option{
		agent.WithName("spawn-child"),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(8),
		agent.WithSystemInstructions(subSys),
		agent.WithCallOptions(model.WithTemperature(0.1)),
		agent.WithVariable(),
		agent.WithToolInfos(mathToolInfos()),
	}
	childOpts = appendSkillRuntimeOpts(req, reg, childOpts)
	childTmpl := agent.New(nil, childOpts...)
	runner.ApplyDeepSeekFromConfig(childTmpl, req.APIKey, req.BaseURL, req.Model)

	pSys := req.SystemPrompt

	var spawnFromSpec func(*exchange.SpawnSpec) *agent.Agent
	spawnFromSpec = func(spec *exchange.SpawnSpec) *agent.Agent {
		c := childTmpl.Clone()
		// Enable nested spawn by default; the policy layer enforces max depth
		c.SpawnEnabled = true
		c.ChildAgentBuilder = spawnFromSpec
		return c
	}

	pOpts := []agent.Option{
		agent.WithName("spawn-parent"),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(12),
		agent.WithSystemInstructions(pSys),
		agent.WithCallOptions(model.WithTemperature(0.1)),
		agent.WithVariable(),
		agent.WithToolInfos(mathToolInfos()),
		agent.WithSpawn(spawnFromSpec),
	}
	pOpts = appendSkillRuntimeOpts(req, reg, pOpts)
	par := agent.New(nil, pOpts...)
	runner.ApplyDeepSeekFromConfig(par, req.APIKey, req.BaseURL, req.Model)
	return par
}

func appendSkillRuntimeOpts(req *ChatRequest, reg *skill.SkillRegistry, opts []agent.Option) []agent.Option {
	names := req.effectiveSkillNames(reg)
	if reg != nil && len(names) > 0 {
		opts = append(opts, agent.WithSkills(reg, names...))
	}
	if req.SkillShell {
		opts = append(opts, agent.WithSkillShellTool(true))
	}
	if req.SkillLoad {
		opts = append(opts, agent.WithLoadSkillTool(true))
	}
	return opts
}

// buildTransferEntry returns the entry agent (triage); use runner.NewRunner(entry, ...) with WithVarStore.
func buildTransferEntry(req *ChatRequest, mcpProf []cfg.MCPServerProfile, mcpSuffix string, reg *skill.SkillRegistry) *agent.Agent {
	triageOpts := []agent.Option{
		agent.WithName("triage"),
		agent.WithDescription("Analyzes the user's request and routes to the appropriate specialist."),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(20),
		agent.WithSystemInstructions(`You are a triage agent. Route to one specialist (do not answer the user yourself):
- Arithmetic, math, or step-by-step calculation → transfer to "math_expert"
- Writing or creative content → transfer to "writer"
- Session variables, var_set, or [Variables] updates → transfer to "writer"
Always call transfer to a specialist.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
		agent.WithVariable(),
	}
	triageOpts = appendSkillRuntimeOpts(req, reg, triageOpts)
	triage := agent.New(nil, triageOpts...)
	runner.ApplyDeepSeekFromConfig(triage, req.APIKey, req.BaseURL, req.Model)

	mathSys := `You are the arithmetic specialist (math_expert). Call tools for every numeric step; never compute mentally.
When MCP arithmetic tools are listed (names may be prefixed), use only those for calculations. If no MCP server is attached, use the built-in tools add, subtract, multiply, divide.
Chain steps: feed each tool output as the next input when needed.
Optional: var_set session_note only if the user explicitly asks to store a preference.`
	if mcpSuffix != "" {
		mathSys += mcpSuffix
	}

	mathOpts := []agent.Option{
		agent.WithName("math_expert"),
		agent.WithDescription("Performs arithmetic via MCP tools (when configured) or built-in add/subtract/multiply/divide."),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(20),
		agent.WithSystemInstructions(mathSys),
		agent.WithCallOptions(model.WithTemperature(0.1)),
		agent.WithVariable(),
		agent.WithToolInfos(mathToolInfos()),
	}
	if len(mcpProf) > 0 {
		mathOpts = append(mathOpts, agent.WithMCPServerProfiles(mcpProf...))
	}
	mathOpts = appendSkillRuntimeOpts(req, reg, mathOpts)
	mathExpert := agent.New(nil, mathOpts...)
	mathExpert.ChatModel = triage.ChatModel

	writerSys := `You are a creative writer. When the user asks for 参数赋值 / var_set / [Variables], use var_set to fill session_note and user_goal as requested, then reply briefly. For normal creative requests, write as usual; you may still use var_set if the user wants session fields updated.`
	if mcpSuffix != "" {
		writerSys += mcpSuffix
	}
	writerOpts := []agent.Option{
		agent.WithName("writer"),
		agent.WithDescription("Creates creative text, stories, poems, and other written content."),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(20),
		agent.WithSystemInstructions(writerSys),
		agent.WithCallOptions(model.WithTemperature(0.7)),
		agent.WithVariable(),
	}
	if len(mcpProf) > 0 {
		writerOpts = append(writerOpts, agent.WithMCPServerProfiles(mcpProf...))
	}
	writerOpts = appendSkillRuntimeOpts(req, reg, writerOpts)
	writer := agent.New(nil, writerOpts...)
	writer.ChatModel = triage.ChatModel

	triage.AddHandoff(mathExpert, writer)
	mathExpert.AddHandoff(triage)

	return triage
}

// --- handler ---

func handleChat(logger log.Logger) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var chatReq ChatRequest
		if err := c.BindJSON(&chatReq); err != nil {
			c.JSON(400, map[string]string{"error": "invalid request body: " + err.Error()})
			return
		}
		if strings.TrimSpace(chatReq.Message) == "" {
			c.JSON(400, map[string]string{"error": "message is required"})
			return
		}

		chatReq.resolveDefaults()

		mcpProf, mcpSuffix, err := chatReq.resolveMCP()
		if err != nil {
			c.JSON(400, map[string]string{"error": err.Error()})
			return
		}

		if chatReq.APIKey == "" {
			c.JSON(400, map[string]string{"error": "api_key is required (pass in request or set LARK_API_KEY / DOUBAO_API_KEY / ARK_API_KEY env)"})
			return
		}

		logger.Info("chat request received",
			"mode", chatReq.Mode,
			"mcp_mode", chatReq.MCPMode,
			"model", chatReq.Model,
			"session_id", chatReq.SessionID,
			"mcp_servers", len(mcpProf),
		)

		sessionID := chatReq.SessionID
		if sessionID == "" {
			sessionID = "sse-session"
		}

		vstore := materializeVarStore(sessionID, chatReq.Mode)

		reg := getSkillRegistry()
		var ag agent.Runnable
		switch chatReq.Mode {
		case "transfer":
			ag = runner.NewRunner(buildTransferEntry(&chatReq, mcpProf, mcpSuffix, reg),
				runner.WithMaxTransfers(10),
				runner.WithLogger(log.Default()),
				runner.WithVarStore(vstore),
				runner.WithSkillRegistry(reg),
			)
		case "spawn":
			ropts := []runner.RunOption{
				runner.WithLogger(log.Default()),
				runner.WithVarStore(vstore),
				runner.WithSpawnConfig(derefInt(chatReq.SpawnMaxDepth, 2), chatReq.SpawnConcurrent),
				runner.WithBudgetLimits(chatReq.BudgetTokens, chatReq.BudgetUSD),
			}
			if reg != nil {
				ropts = append(ropts, runner.WithSkillRegistry(reg))
			}
			if chatReq.ShowSystem {
				sysPrompt := chatReq.SystemPrompt
				logger.Info("spawn mode system prompt", "system_prompt", sysPrompt)
			}
			ag = runner.NewRunner(
				buildSpawnAgent(&chatReq, reg),
				ropts...,
			)
		default:
			entry := buildSingleAgent(&chatReq, mcpProf, mcpSuffix, reg)
			ropts := []runner.RunOption{
				runner.WithLogger(log.Default()),
				runner.WithVarStore(vstore),
			}
			if reg != nil {
				ropts = append(ropts, runner.WithSkillRegistry(reg))
			}
			ag = runner.NewRunner(entry, ropts...)
		}

		req := &request.RuntimeRequest{
			SessionID:   sessionID,
			UserMessage: chatReq.Message,
			RunMode:     request.RunModeSingleAgent,
		}

		w := sse.NewWriter(c)
		ch := ag.Run(ctx, req)

		eventID := 0
		var lastVarStore *variable.VarStore
		for ev := range ch {
			if ev.Type == event.EventCallLLMStart {
				if p := ev.CallLLMStart(); p != nil {
					toolsJSON, _ := json.Marshal(p.Tools)
					logger.Info("call_llm_start",
						"step", ev.Step,
						"agent", p.AgentName,
						"model", p.Model,
						"mcp_server_ids", p.MCPServerIDs,
						"mcp_tool_names", p.MCPToolNames,
						"system_prompt", p.SystemPrompt,
						"tools_json", string(toolsJSON),
					)
				}
			}
			if qe := ev.QueryEnd(); qe != nil && qe.Outcome != nil {
				lastVarStore = qe.Outcome.VarStore
			}
			eventID++
			sseEvt := runtimeEventToSSE(ev)
			data, _ := json.Marshal(sseEvt)
			w.WriteEvent(
				fmt.Sprintf("%d", eventID),
				string(ev.Type),
				data,
			)
		}
		w.Close()

		persistVarSnapshot(sessionID, lastVarStore)

		logger.Info("chat request completed",
			"session_id", sessionID,
			"events", eventID,
		)
	}
}

func runtimeEventToSSE(ev *event.RuntimeEvent) SSEEvent {
	out := SSEEvent{
		Type: string(ev.Type),
		Step: ev.Step,
	}
	switch ev.Type {
	case event.EventStart:
		out.Data = ev.Start()
	case event.EventQuestion:
		if p := ev.Question(); p != nil {
			out.Data = p
		}
	case event.EventAnswer:
		if p := ev.Answer(); p != nil {
			out.Data = p
		}
	case event.EventCallLLMStart:
		if p := ev.CallLLMStart(); p != nil {
			out.Data = p
		}
	case event.EventCallLLMEnd:
		if p := ev.CallLLMEnd(); p != nil {
			out.Data = p
		}
	case event.EventToolCallStart:
		if p := ev.ToolCallStart(); p != nil {
			out.Data = p
		}
	case event.EventToolCallEnd:
		if p := ev.ToolCallEnd(); p != nil {
			out.Data = p
		}
	case event.EventAgentTransfer:
		if p := ev.AgentTransfer(); p != nil {
			out.Data = p
		}
	case event.EventSpawnStart:
		if p := ev.SpawnStart(); p != nil {
			out.Data = p
		}
	case event.EventSpawnEnd:
		if p := ev.SpawnEnd(); p != nil {
			out.Data = p
		}
	case event.EventError:
		if p := ev.Error(); p != nil {
			out.Data = p
		}
	case event.EventQueryEnd:
		if p := ev.QueryEnd(); p != nil {
			if p.Outcome != nil && p.Outcome.VarStore != nil {
				out.Data = map[string]any{
					"outcome":   p.Outcome,
					"variables": p.Outcome.VarStore.Snapshot(),
				}
			} else {
				out.Data = p
			}
		}
	}
	return out
}

func handleSkillList(_ context.Context, c *app.RequestContext) {
	reg := getSkillRegistry()
	if reg == nil {
		c.JSON(200, map[string]any{"loaded": false, "generation": uint64(0), "skills": []skill.SkillMeta{}})
		return
	}
	c.JSON(200, map[string]any{
		"loaded":     true,
		"generation": reg.Generation(),
		"skills":     reg.List(),
	})
}

func handleRuntimeConfig(_ context.Context, c *app.RequestContext) {
	out := map[string]any{"skill_paths": skillPathDirs()}
	reg := getSkillRegistry()
	if reg == nil {
		out["skill_loaded"] = false
		c.JSON(200, out)
		return
	}
	out["skill_loaded"] = true
	out["skill_generation"] = reg.Generation()
	out["skills"] = reg.List()
	c.JSON(200, out)
}

func handleSkillReload(ctx context.Context, c *app.RequestContext) {
	if err := reloadSkillRegistry(ctx); err != nil {
		c.JSON(500, map[string]string{"error": err.Error()})
		return
	}
	handleSkillList(ctx, c)
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8191"
	}

	logger := slog.Default()
	initSkillRegistry(context.Background())

	hlog.SetSilentMode(true)
	h := server.New(server.WithHostPorts(addr))

	h.StaticFile("/", "./static/index.html")
	h.Static("/static", "./static")

	h.GET("/api/skills", handleSkillList)
	h.GET("/api/runtime-config", handleRuntimeConfig)
	h.POST("/api/skills/reload", handleSkillReload)
	h.POST("/api/chat", handleChat(logger))

	logger.Info("loopForge test-server starting", "addr", addr)
	logMCPStartup(logger)
	h.Spin()
}

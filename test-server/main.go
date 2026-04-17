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

	"loopforge/pkg/agent"
	"loopforge/pkg/log"
	"loopforge/pkg/mcp/cfg"
	"loopforge/pkg/model"
	larkadapter "loopforge/pkg/model/adapters/lark"
	"loopforge/pkg/runner"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/variable"
)

const (
	defaultModel = "deepseek-v3-2-251201"
	defaultSystemPrompt = `You are the loopForge test assistant (single-agent mode).

Shared variables appear in a [Variables] block at the end of the system prompt. Keys starting with const_ are read-only; never call var_set on them.
Use var_set to write user-requested values for writable keys. When MCP arithmetic tools are available (names may be prefixed), call them for every numeric step; do not compute mentally.

After finishing, reply briefly and confirm what you set or computed.`
)

// ChatRequest is the JSON body for POST /api/chat.
type ChatRequest struct {
	Message      string `json:"message"`
	SessionID    string `json:"session_id,omitempty"`
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url,omitempty"`
	Model        string `json:"model,omitempty"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Mode         string `json:"mode,omitempty"` // "single" (default) or "transfer"

	// MCPMode: "env" (default) uses server env defaults; "off" disables MCP; "custom" uses MCP.
	MCPMode string     `json:"mcp_mode,omitempty"`
	MCP     *MCPFields `json:"mcp,omitempty"`
}

// SSEEvent is the JSON payload written as SSE data for each RuntimeEvent.
type SSEEvent struct {
	Type string `json:"type"`
	Step int    `json:"step,omitempty"`
	Data any    `json:"data,omitempty"`
}

func (r *ChatRequest) resolveDefaults() {
	if r.APIKey == "" {
		r.APIKey = firstNonEmpty(os.Getenv("LARK_API_KEY"), os.Getenv("DOUBAO_API_KEY"), os.Getenv("ARK_API_KEY"))
	}
	if r.BaseURL == "" {
		r.BaseURL = firstNonEmpty(os.Getenv("LARK_BASE_URL"), os.Getenv("DOUBAO_BASE_URL"), larkadapter.DefaultBaseURL)
	}
	r.BaseURL = strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if r.Model == "" {
		r.Model = firstNonEmpty(os.Getenv("LARK_MODEL"), os.Getenv("ARK_MODEL"), os.Getenv("DOUBAO_MODEL"), defaultModel)
	}
	if r.SystemPrompt == "" {
		r.SystemPrompt = defaultSystemPrompt
	}
	if r.Mode == "" {
		r.Mode = "single"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
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

func buildSingleAgent(req *ChatRequest, mcpProf []cfg.MCPServerProfile, mcpSuffix string) *agent.Agent {
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
	}
	if len(mcpProf) > 0 {
		opts = append(opts, agent.WithMCPServerProfiles(mcpProf...))
	}
	a := agent.New(nil, opts...)
	runner.ApplyLarkFromConfig(a, req.APIKey, req.BaseURL, req.Model)
	return a
}

// buildTransferEntry returns the entry agent (triage); use runner.NewRunner(entry, ...) with WithVarStore.
func buildTransferEntry(req *ChatRequest, mcpProf []cfg.MCPServerProfile, mcpSuffix string) *agent.Agent {
	triage := agent.New(nil,
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
	)
	runner.ApplyLarkFromConfig(triage, req.APIKey, req.BaseURL, req.Model)

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
	}
	if len(mcpProf) > 0 {
		mathOpts = append(mathOpts, agent.WithMCPServerProfiles(mcpProf...))
	} else {
		mathOpts = append(mathOpts, agent.WithToolInfos(mathToolInfos()))
	}
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

		var ag agent.Runnable
		switch chatReq.Mode {
		case "transfer":
			ag = runner.NewRunner(buildTransferEntry(&chatReq, mcpProf, mcpSuffix),
				runner.WithMaxTransfers(10),
				runner.WithLogger(log.Default()),
				runner.WithVarStore(vstore),
			)
		default:
			ag = runner.NewRunner(buildSingleAgent(&chatReq, mcpProf, mcpSuffix),
				runner.WithLogger(log.Default()),
				runner.WithVarStore(vstore),
			)
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

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8191"
	}

	logger := slog.Default()

	hlog.SetSilentMode(true)
	h := server.New(server.WithHostPorts(addr))

	h.StaticFile("/", "./static/index.html")
	h.Static("/static", "./static")

	h.POST("/api/chat", handleChat(logger))

	logger.Info("loopForge test-server starting", "addr", addr)
	logMCPStartup(logger)
	h.Spin()
}

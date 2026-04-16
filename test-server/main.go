package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/sse"

	"loopforge/pkg/agent"
	"loopforge/pkg/model"
	arkdoubao "loopforge/pkg/model/adapters/doubao"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/transfer"
)

const (
	defaultBaseURL      = "https://ark.cn-beijing.volces.com/api/v3"
	defaultModel        = "deepseek-v3-2-251201"
	defaultSystemPrompt = `You are a calculator assistant. You have four arithmetic tools:
  add(a, b)       → a + b
  subtract(a, b)  → a - b
  multiply(a, b)  → a * b
  divide(a, b)    → a / b
You MUST call the tools for every calculation step. Never compute in your head.
When multiple steps depend on previous results, call them one step at a time and use the returned value for the next call.`
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
}

// SSEEvent is the JSON payload written as SSE data for each RuntimeEvent.
type SSEEvent struct {
	Type string `json:"type"`
	Step int    `json:"step,omitempty"`
	Data any    `json:"data,omitempty"`
}

func (r *ChatRequest) resolveDefaults() {
	if r.APIKey == "" {
		r.APIKey = firstNonEmpty(os.Getenv("DOUBAO_API_KEY"), os.Getenv("ARK_API_KEY"))
	}
	if r.BaseURL == "" {
		r.BaseURL = firstNonEmpty(os.Getenv("DOUBAO_BASE_URL"), defaultBaseURL)
	}
	r.BaseURL = strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	if r.Model == "" {
		r.Model = firstNonEmpty(os.Getenv("ARK_MODEL"), os.Getenv("DOUBAO_MODEL"), defaultModel)
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

func buildSingleAgent(req *ChatRequest) agent.Agent {
	chat := arkdoubao.NewArkChatModel(req.APIKey, req.BaseURL, req.Model)
	return agent.NewRunnerAgent(chat,
		agent.WithName("test-server-agent"),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(12),
		agent.WithToolInfos(mathToolInfos()),
		agent.WithSystemInstructions(req.SystemPrompt),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)
}

func buildTransferAgent(req *ChatRequest) agent.Agent {
	chat := arkdoubao.NewArkChatModel(req.APIKey, req.BaseURL, req.Model)

	triage := agent.NewRunnerAgent(chat,
		agent.WithName("triage"),
		agent.WithDescription("Analyzes the user's request and routes to the appropriate specialist."),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(4),
		agent.WithSystemInstructions(`You are a triage agent. Analyze the user's request and transfer to the right specialist:
- For math/calculation tasks → transfer to "math_expert"
- For writing/creative/other tasks → transfer to "writer"
Do NOT attempt to answer yourself. Always transfer to a specialist.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)

	mathExpert := agent.NewRunnerAgent(chat,
		agent.WithName("math_expert"),
		agent.WithDescription("Solves math problems step by step using arithmetic tools (add, subtract, multiply, divide)."),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(12),
		agent.WithToolInfos(mathToolInfos()),
		agent.WithSystemInstructions(`You are a math expert. You have four arithmetic tools: add, subtract, multiply, divide.
You MUST call tools for every calculation step. Never compute in your head.
When multiple steps depend on previous results, call them one step at a time.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)

	writer := agent.NewRunnerAgent(chat,
		agent.WithName("writer"),
		agent.WithDescription("Creates creative text, stories, poems, and other written content."),
		agent.WithModelName(req.Model),
		agent.WithMaxSteps(6),
		agent.WithSystemInstructions(`You are a creative writer. Produce engaging, well-structured text based on the user's request.
Be creative and thoughtful in your writing.`),
		agent.WithCallOptions(model.WithTemperature(0.7)),
	)

	triage.AddHandoff(mathExpert, writer)
	mathExpert.AddHandoff(triage)

	return transfer.NewOrchestrator(triage, transfer.WithMaxTransfers(5))
}

// --- handler ---

func handleChat() app.HandlerFunc {
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

		if chatReq.APIKey == "" {
			c.JSON(400, map[string]string{"error": "api_key is required (pass in request or set DOUBAO_API_KEY / ARK_API_KEY env)"})
			return
		}

		var ag agent.Agent
		switch chatReq.Mode {
		case "transfer":
			ag = buildTransferAgent(&chatReq)
		default:
			ag = buildSingleAgent(&chatReq)
		}

		sessionID := chatReq.SessionID
		if sessionID == "" {
			sessionID = "sse-session"
		}

		req := &request.RuntimeRequest{
			SessionID:   sessionID,
			UserMessage: chatReq.Message,
			RunMode:     request.RunModeSingleAgent,
		}

		w := sse.NewWriter(c)
		ch := ag.Run(ctx, req)

		eventID := 0
		for ev := range ch {
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
			out.Data = p
		}
	}
	return out
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	h := server.Default(server.WithHostPorts(addr))

	h.StaticFile("/", "./static/index.html")
	h.Static("/static", "./static")

	h.POST("/api/chat", handleChat())

	fmt.Printf("loopForge test-server starting on %s\n", addr)
	fmt.Printf("  open http://localhost%s in browser\n", addr)
	h.Spin()
}

// Command agentdemo demonstrates loopForge capabilities.
//
// Modes:
//   - single (default): single-agent multi-tool chaining with four arithmetic tools.
//   - transfer:         multi-agent handoff — triage → math_expert / writer.
//
// Env:
//   - ARK_API_KEY or DOUBAO_API_KEY (required)
//   - ARK_MODEL or DOUBAO_MODEL (optional, default deepseek-v3-2-251201)
//   - DOUBAO_BASE_URL (optional)
//   - DOUBAO_USER_MESSAGE (optional)
//   - DEMO_MODE (optional: "single" | "transfer", default "single")
//
// Offline tool test (no API key): go test ./pkg/agent/... -run TestRunnerAgent_toolLoop
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"loopforge/pkg/agent"
	"loopforge/pkg/model"
	arkdoubao "loopforge/pkg/model/adapters/doubao"
	"loopforge/pkg/runner"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/request"
)

const (
	defaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	defaultModel   = "deepseek-v3-2-251201"

	defaultUserText = `请按以下步骤计算，每步必须调用对应的工具，不要心算：
1. 用 add 计算 17 + 28
2. 用 multiply 将第1步的结果乘以 3
3. 用 subtract 将第2步的结果减去 10
最后告诉我每一步的结果和最终答案。`

	transferUserText = `帮我算一下 (15 + 27) * 3 - 10 的结果，每步都要用工具计算。`
)

type demoConfig struct {
	APIKey      string
	BaseURL     string
	Model       string
	UserMessage string
	Mode        string // "single" or "transfer"
}

func loadConfig() (demoConfig, error) {
	key := strings.TrimSpace(os.Getenv("DOUBAO_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ARK_API_KEY"))
	}
	if key == "" {
		return demoConfig{}, fmt.Errorf("set ARK_API_KEY or DOUBAO_API_KEY")
	}
	base := strings.TrimSpace(os.Getenv("DOUBAO_BASE_URL"))
	if base == "" {
		base = defaultBaseURL
	}
	m := strings.TrimSpace(os.Getenv("ARK_MODEL"))
	if m == "" {
		m = strings.TrimSpace(os.Getenv("DOUBAO_MODEL"))
	}
	if m == "" {
		m = defaultModel
	}

	mode := strings.TrimSpace(os.Getenv("DEMO_MODE"))
	if mode == "" {
		mode = "single"
	}

	msg := strings.TrimSpace(os.Getenv("DOUBAO_USER_MESSAGE"))
	if msg == "" {
		if mode == "transfer" {
			msg = transferUserText
		} else {
			msg = defaultUserText
		}
	}

	return demoConfig{
		APIKey:      key,
		BaseURL:     strings.TrimRight(base, "/"),
		Model:       m,
		UserMessage: msg,
		Mode:        mode,
	}, nil
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

// --- single-agent mode ---

func runSingleAgent(ctx context.Context, cfg demoConfig) {
	chat := arkdoubao.NewArkChatModel(cfg.APIKey, cfg.BaseURL, cfg.Model)
	runner := agent.New(chat,
		agent.WithName("agentdemo"),
		agent.WithModelName(cfg.Model),
		agent.WithMaxSteps(12),
		agent.WithToolInfos(mathToolInfos()),
		agent.WithSystemInstructions(`You are a calculator assistant. You have four arithmetic tools:
  add(a, b)       → a + b
  subtract(a, b)  → a - b
  multiply(a, b)  → a * b
  divide(a, b)    → a / b
You MUST call the tools for every calculation step. Never compute in your head.
When multiple steps depend on previous results, call them one step at a time and use the returned value for the next call.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)

	streamAndPrint(ctx, runner, cfg)
}

// --- multi-agent transfer mode ---

func runTransferDemo(ctx context.Context, cfg demoConfig) {
	chat := arkdoubao.NewArkChatModel(cfg.APIKey, cfg.BaseURL, cfg.Model)

	triage := agent.New(chat,
		agent.WithName("triage"),
		agent.WithDescription("Routes user requests to the appropriate specialist agent."),
		agent.WithModelName(cfg.Model),
		agent.WithMaxSteps(4),
		agent.WithSystemInstructions(`You are a triage agent. Analyze the user's request and transfer to the right specialist:
- For math/calculation tasks → transfer to "math_expert"
- For writing/creative tasks → transfer to "writer"
Do NOT attempt to answer yourself. Always transfer to a specialist.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)

	mathExpert := agent.New(chat,
		agent.WithName("math_expert"),
		agent.WithDescription("Solves math problems step by step using arithmetic tools."),
		agent.WithModelName(cfg.Model),
		agent.WithMaxSteps(12),
		agent.WithToolInfos(mathToolInfos()),
		agent.WithSystemInstructions(`You are a math expert. You have four arithmetic tools: add, subtract, multiply, divide.
You MUST call tools for every calculation step. Never compute in your head.
When multiple steps depend on previous results, call them one step at a time.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)

	writer := agent.New(chat,
		agent.WithName("writer"),
		agent.WithDescription("Creates creative text, stories, poems, and other written content."),
		agent.WithModelName(cfg.Model),
		agent.WithMaxSteps(4),
		agent.WithSystemInstructions(`You are a creative writer. Produce engaging, well-structured text based on the user's request.
Be creative and thoughtful in your writing.`),
		agent.WithCallOptions(model.WithTemperature(0.7)),
	)

	triage.AddHandoff(mathExpert, writer)
	mathExpert.AddHandoff(triage)

	r := runner.NewRunner(triage, runner.WithMaxTransfers(5))
	streamAndPrint(ctx, r, cfg)
}

// --- shared event printer ---

func streamAndPrint(ctx context.Context, ag agent.Runnable, cfg demoConfig) {
	req := &request.RuntimeRequest{
		SessionID:   "agentdemo-session",
		UserMessage: cfg.UserMessage,
		RunMode:     request.RunModeSingleAgent,
		Options:     request.RuntimeOptions{},
	}

	fmt.Printf("user: %s\n\n", cfg.UserMessage)

	ch := ag.Run(ctx, req)
	for ev := range ch {
		switch ev.Type {
		case event.EventStart:
			fmt.Println("[start]")

		case event.EventQuestion:
			if q := ev.Question(); q != nil {
				fmt.Printf("[question] %s\n", q.UserMessage)
			}

		case event.EventCallLLMStart:
			fmt.Printf("\n── call_llm step %d ──\n", ev.Step)

		case event.EventCallLLMEnd:
			fmt.Printf("── call_llm_end step %d ──\n", ev.Step)

		case event.EventAnswer:
			if a := ev.Answer(); a != nil {
				fmt.Print(a.Delta)
			}

		case event.EventToolCallStart:
			if ts := ev.ToolCallStart(); ts != nil {
				fmt.Printf("  → [tool_call_start] %s(%s)\n", ts.Name, ts.Arguments)
			}

		case event.EventToolCallEnd:
			if te := ev.ToolCallEnd(); te != nil {
				status := "✓"
				if te.IsError {
					status = "✗"
				}
				fmt.Printf("  ← [tool_call_end]   %s result=%s\n", status, te.Output)
			}

		case event.EventAgentTransfer:
			if at := ev.AgentTransfer(); at != nil {
				fmt.Printf("\n⇄ [agent_transfer] %s → %s (reason: %s)\n\n",
					at.FromAgent, at.ToAgent, at.Reason)
			}

		case event.EventError:
			if ep := ev.Error(); ep != nil {
				fmt.Fprintf(os.Stderr, "  !! [error] %s: %s\n", ep.Code, ep.Message)
			}

		case event.EventQueryEnd:
			if qe := ev.QueryEnd(); qe != nil && qe.Outcome != nil {
				oc := qe.Outcome
				fmt.Printf("\n══════════════════════════════\n")
				fmt.Printf("run_id=%s  termination=%s\n", oc.RunID, oc.Termination)
				fmt.Printf("model=%s  steps=%d\n", oc.Metrics.Model, oc.Metrics.Steps)
				if len(oc.TransferChain) > 0 {
					fmt.Printf("transfer_chain=%s\n", strings.Join(oc.TransferChain, " → "))
				}
				if oc.FinalText != "" {
					fmt.Printf("\nassistant:\n%s\n", oc.FinalText)
				}
			}
		}
	}
}

func main() {
	ctx := context.Background()
	cfg, err := loadConfig()
	if err != nil {
		printUsage(err)
		os.Exit(1)
	}

	switch cfg.Mode {
	case "transfer":
		fmt.Println("=== Multi-Agent Transfer Demo ===")
		fmt.Println()
		runTransferDemo(ctx, cfg)
	default:
		fmt.Println("=== Single-Agent Tool Chain Demo ===")
		fmt.Println()
		runSingleAgent(ctx, cfg)
	}
}

func printUsage(err error) {
	fmt.Fprintf(os.Stderr, "agentdemo: %v\n\n", err)
	fmt.Fprintf(os.Stderr, "Env:\n")
	fmt.Fprintf(os.Stderr, "  ARK_API_KEY or DOUBAO_API_KEY   required\n")
	fmt.Fprintf(os.Stderr, "  ARK_MODEL or DOUBAO_MODEL       default %s\n", defaultModel)
	fmt.Fprintf(os.Stderr, "  DOUBAO_BASE_URL                 default %s\n", defaultBaseURL)
	fmt.Fprintf(os.Stderr, "  DOUBAO_USER_MESSAGE             optional\n")
	fmt.Fprintf(os.Stderr, "  DEMO_MODE                       single | transfer (default single)\n")
	fmt.Fprintf(os.Stderr, "Offline tool test: go test ./pkg/agent/... -run TestRunnerAgent_toolLoop\n")
}

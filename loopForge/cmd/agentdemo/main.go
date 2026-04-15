// Command agentdemo demonstrates multi-tool chaining with loopForge RunnerAgent.
//
// The agent has four arithmetic tools: add, multiply, subtract, divide.
// The default prompt asks a question that requires chaining three of them across
// multiple loop steps, exercising the full tool-call streaming pipeline.
//
// Env:
//   - ARK_API_KEY or DOUBAO_API_KEY (required)
//   - ARK_MODEL or DOUBAO_MODEL (optional, default deepseek-v3-2-251201)
//   - DOUBAO_BASE_URL (optional)
//   - DOUBAO_USER_MESSAGE (optional)
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
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/request"
)

const (
	defaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	defaultModel   = "deepseek-v3-2-251201"

	// Requires: add(17,28)=45 → multiply(45,3)=135 → subtract(135,10)=125
	defaultUserText = `请按以下步骤计算，每步必须调用对应的工具，不要心算：
1. 用 add 计算 17 + 28
2. 用 multiply 将第1步的结果乘以 3
3. 用 subtract 将第2步的结果减去 10
最后告诉我每一步的结果和最终答案。`
)

type demoConfig struct {
	APIKey      string
	BaseURL     string
	Model       string
	UserMessage string
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
	msg := strings.TrimSpace(os.Getenv("DOUBAO_USER_MESSAGE"))
	if msg == "" {
		msg = defaultUserText
	}
	return demoConfig{
		APIKey:      key,
		BaseURL:     strings.TrimRight(base, "/"),
		Model:       m,
		UserMessage: msg,
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

func demoToolInfos() []*model.ToolInfo {
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

func main() {
	ctx := context.Background()
	cfg, err := loadConfig()
	if err != nil {
		printUsage(err)
		os.Exit(1)
	}

	chat := arkdoubao.NewArkChatModel(cfg.APIKey, cfg.BaseURL, cfg.Model)
	runner := agent.NewRunnerAgent(chat,
		agent.WithName("agentdemo"),
		agent.WithModelName(cfg.Model),
		agent.WithMaxSteps(12),
		agent.WithToolInfos(demoToolInfos()),
		agent.WithSystemInstructions(`You are a calculator assistant. You have four arithmetic tools:
  add(a, b)       → a + b
  subtract(a, b)  → a - b
  multiply(a, b)  → a * b
  divide(a, b)    → a / b
You MUST call the tools for every calculation step. Never compute in your head.
When multiple steps depend on previous results, call them one step at a time and use the returned value for the next call.`),
		agent.WithCallOptions(model.WithTemperature(0.1)),
	)

	var ag agent.Agent = runner

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
				if oc.FinalText != "" {
					fmt.Printf("\nassistant:\n%s\n", oc.FinalText)
				}
			}
		}
	}
}

func printUsage(err error) {
	fmt.Fprintf(os.Stderr, "agentdemo: %v\n\n", err)
	fmt.Fprintf(os.Stderr, "Env:\n")
	fmt.Fprintf(os.Stderr, "  ARK_API_KEY or DOUBAO_API_KEY   required\n")
	fmt.Fprintf(os.Stderr, "  ARK_MODEL or DOUBAO_MODEL       default %s\n", defaultModel)
	fmt.Fprintf(os.Stderr, "  DOUBAO_BASE_URL                 default %s\n", defaultBaseURL)
	fmt.Fprintf(os.Stderr, "  DOUBAO_USER_MESSAGE             optional\n")
	fmt.Fprintf(os.Stderr, "Offline tool test: go test ./pkg/agent/... -run TestRunnerAgent_toolLoop\n")
}

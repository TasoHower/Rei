package main

import (
	"context"
	"fmt"
	"io"

	"github.com/agentizen/agent-sdk-go/pkg/agent"
	"github.com/agentizen/agent-sdk-go/pkg/runner"
	"github.com/agentizen/agent-sdk-go/pkg/tool"
)

// runMock executes the offline scripted demo (no network).
func runMock(ctx context.Context, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	provider := MockProvider{}
	rn := runner.NewRunner().WithDefaultProvider(provider)

	echoTool := tool.NewFunctionTool(
		"demo_echo",
		"Echo input text back to the model.",
		func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
			_ = ctx
			text, _ := args["text"].(string)
			return map[string]interface{}{"echo": text}, nil
		},
	)

	ag := agent.NewAgent("loopforge-demo").
		SetSystemInstructions("You are a deterministic demo agent. Follow tool policy when tools are available.").
		SetModelProvider(provider).
		WithModel("mock").
		WithTools(echoTool)

	fmt.Fprintln(w, "--- Mock demo (no API key) ---")
	fmt.Fprintln(w, "--- Case 1: text-only mock (no tool calls) ---")
	if err := runMockCase(ctx, w, rn, ag, "Hello from loopForge", 3); err != nil {
		return err
	}

	fmt.Fprintln(w, "--- Case 2: WITH_TOOL keyword triggers demo_echo then finishes ---")
	if err := runMockCase(ctx, w, rn, ag, "Please run WITH_TOOL for the demo.", 5); err != nil {
		return err
	}
	return nil
}

func runMockCase(ctx context.Context, w io.Writer, rn *runner.Runner, ag *agent.Agent, userText string, maxTurns int) error {
	res, err := rn.Run(ctx, ag, &runner.RunOptions{
		Input:    userText,
		MaxTurns: maxTurns,
		RunConfig: &runner.RunConfig{
			ModelProvider:   MockProvider{},
			TracingDisabled: true,
		},
	})
	if err != nil {
		return fmt.Errorf("runner.Run: %w", err)
	}
	out := res.FinalOutput
	switch v := out.(type) {
	case string:
		fmt.Fprintf(w, "final_output: %s\n", v)
	default:
		fmt.Fprintf(w, "final_output: %v\n", v)
	}
	fmt.Fprintf(w, "turns_used (raw responses): %d new_items: %d\n\n", len(res.RawResponses), len(res.NewItems))
	return nil
}

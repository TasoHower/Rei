package main

import (
	"context"
	"fmt"
	"io"

	"github.com/agentizen/agent-sdk-go/pkg/agent"
	"github.com/agentizen/agent-sdk-go/pkg/model/providers/openai"
	"github.com/agentizen/agent-sdk-go/pkg/runner"
)

// runDoubaoRunner performs the first chat completion against Ark using the OpenAI-compatible surface.
func runDoubaoRunner(ctx context.Context, w io.Writer, cfg DoubaoConfig) error {
	provider := openai.NewOpenAIProvider(cfg.APIKey).
		SetBaseURL(cfg.BaseURL).
		WithDefaultModel(cfg.Model)

	rn := runner.NewRunner().WithDefaultProvider(provider).WithDefaultMaxTurns(8)

	ag := agent.NewAgent("loopforge-doubao").
		SetSystemInstructions("You are a helpful assistant. Be concise unless the user asks for detail.").
		SetModelProvider(provider).
		WithModel(cfg.Model)

	const userPrompt = "Hello. Please reply with a short greeting and one sentence describing what you can help with."

	fmt.Fprintln(w, "--- Doubao (Volcengine Ark) first interaction ---")
	fmt.Fprintf(w, "endpoint (model id): %s\n", cfg.Model)

	res, err := rn.Run(ctx, ag, &runner.RunOptions{
		Input:    userPrompt,
		MaxTurns: 4,
		RunConfig: &runner.RunConfig{
			ModelProvider:   provider,
			TracingDisabled: true,
		},
	})
	if err != nil {
		return fmt.Errorf("runner.Run: %w", err)
	}

	out := res.FinalOutput
	switch v := out.(type) {
	case string:
		fmt.Fprintf(w, "\nassistant:\n%s\n", v)
	default:
		fmt.Fprintf(w, "\nassistant (non-string):\n%v\n", v)
	}
	fmt.Fprintf(w, "\nturns_used: %d new_items: %d\n", len(res.RawResponses), len(res.NewItems))
	return nil
}

package main

import (
	"context"
	"fmt"
	"io"

	"loopforge/pkg/model"
	larkadapter "loopforge/pkg/model/adapters/lark"
)

// runLarkRunner performs one chat completion against Ark using loopForge's Lark adapter
// (github.com/volcengine/volcengine-go-sdk / arkruntime).
func runLarkRunner(ctx context.Context, w io.Writer, cfg LarkConfig) error {
	cm := larkadapter.NewLarkChatModel(cfg.APIKey, cfg.BaseURL, cfg.Model)

	const userPrompt = "Hello. Please reply with a short greeting and one sentence describing what you can help with."

	fmt.Fprintln(w, "--- Lark (Volcengine Ark) first interaction ---")
	fmt.Fprintf(w, "endpoint (model id): %s\n", cfg.Model)

	out, err := cm.Generate(ctx, []*model.Message{
		{Role: model.RoleSystem, Content: "You are a helpful assistant. Be concise unless the user asks for detail."},
		{Role: model.RoleUser, Content: userPrompt},
	})
	if err != nil {
		return fmt.Errorf("Generate: %w", err)
	}
	if out == nil {
		return fmt.Errorf("empty model response")
	}

	fmt.Fprintf(w, "\nassistant:\n%s\n", out.Content)
	if out.InputTokens > 0 || out.OutputTokens > 0 {
		fmt.Fprintf(w, "\ntokens: prompt=%d completion=%d\n", out.InputTokens, out.OutputTokens)
	}
	return nil
}

package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	"seraglf/internal/config"
)

type Provider struct {
	Grader    model.BaseChatModel
	Generator model.BaseChatModel
}

func NewProvider(ctx context.Context, cfg config.LLMConfig) (*Provider, error) {
	grader, err := newChatModel(ctx, cfg, cfg.GraderModel)
	if err != nil {
		return nil, fmt.Errorf("create grader model: %w", err)
	}
	generator, err := newChatModel(ctx, cfg, cfg.GeneratorModel)
	if err != nil {
		return nil, fmt.Errorf("create generator model: %w", err)
	}
	return &Provider{Grader: grader, Generator: generator}, nil
}

func newChatModel(ctx context.Context, cfg config.LLMConfig, modelName string) (model.BaseChatModel, error) {
	opts := &openai.ChatModelConfig{
		Model:  modelName,
		APIKey: cfg.APIKey,
	}
	if cfg.BaseURL != "" {
		opts.BaseURL = cfg.BaseURL
	}
	return openai.NewChatModel(ctx, opts)
}

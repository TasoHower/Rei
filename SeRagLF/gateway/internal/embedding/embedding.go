package embedding

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	einoEmbed "github.com/cloudwego/eino/components/embedding"

	"seraglf/internal/config"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	Dimension() int
}

type Service struct {
	embedder einoEmbed.Embedder
	dim      int
}

func NewService(ctx context.Context, cfg config.EmbeddingConfig) (*Service, error) {
	embedder, err := openai.NewEmbedder(ctx, &openai.EmbeddingConfig{
		Model:  cfg.Model,
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create embedder: %w", err)
	}
	return &Service{embedder: embedder, dim: 1536}, nil
}

func (s *Service) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	vecs, err := s.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	return vecs, nil
}

func (s *Service) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	vecs, err := s.Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("empty embedding result")
	}
	return vecs[0], nil
}

func (s *Service) Dimension() int {
	return s.dim
}

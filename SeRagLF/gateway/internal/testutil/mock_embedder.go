package testutil

import "context"

type MockEmbedder struct {
	EmbedFn      func(ctx context.Context, texts []string) ([][]float64, error)
	EmbedQueryFn func(ctx context.Context, text string) ([]float64, error)
	DimensionFn  func() int
}

func (m *MockEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if m.EmbedFn != nil {
		return m.EmbedFn(ctx, texts)
	}
	vecs := make([][]float64, len(texts))
	for i := range texts {
		vecs[i] = make([]float64, 4)
	}
	return vecs, nil
}

func (m *MockEmbedder) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	if m.EmbedQueryFn != nil {
		return m.EmbedQueryFn(ctx, text)
	}
	return make([]float64, 4), nil
}

func (m *MockEmbedder) Dimension() int {
	if m.DimensionFn != nil {
		return m.DimensionFn()
	}
	return 4
}

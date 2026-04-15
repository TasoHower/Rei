package testutil

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type MockChatModel struct {
	GenerateFn func(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
}

func (m *MockChatModel) Generate(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return m.GenerateFn(ctx, messages)
}

func (m *MockChatModel) Stream(ctx context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *MockChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

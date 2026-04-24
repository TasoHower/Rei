package model

import (
	"context"

	"loopforge/pkg/log"
	"loopforge/pkg/model/types"
)

// nonStreamModel wraps a ToolCallingChatModel and forces non-streaming behavior.
// Stream calls are downgraded to Generate, with the single response wrapped into
// a completed stream reader. This implements spawn-runtime-rules.md §6.
type nonStreamModel struct {
	inner ToolCallingChatModel
}

// WrapNonStream returns a ToolCallingChatModel wrapper that disables streaming.
// The SpawnSpec.AllowStream field is preserved but ignored by the engine.
func WrapNonStream(chat ToolCallingChatModel) ToolCallingChatModel {
	return &nonStreamModel{inner: chat}
}

func (m *nonStreamModel) Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error) {
	return m.inner.Generate(ctx, input, opts...)
}

func (m *nonStreamModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	log.Default().Debug("nonStreamModel: downgrading Stream to Generate",
		"impl", "nonStreamModel",
	)
	msg, err := m.inner.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}

func (m *nonStreamModel) WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error) {
	inner, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &nonStreamModel{inner: inner}, nil
}

// compile-time check
var _ ToolCallingChatModel = (*nonStreamModel)(nil)

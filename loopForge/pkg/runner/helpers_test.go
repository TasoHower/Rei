package runner

import (
	"context"
	"sync"

	"loopforge/pkg/agent"
	modeliface "loopforge/pkg/model/interface"
	"loopforge/pkg/model/types"
	"loopforge/pkg/runtime/event"
)

// mockTransferChatModel simulates an agent that calls transfer_to_{target} on the first round.
type mockTransferChatModel struct {
	target string
}

func (m mockTransferChatModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}

func (m mockTransferChatModel) Generate(_ context.Context, _ []*types.Message, _ ...types.CallOption) (*types.Message, error) {
	return &types.Message{
		Role:    types.RoleAssistant,
		Content: "",
		ToolCalls: []types.ToolCallPart{{
			ID:        "call_transfer_1",
			Name:      agent.TransferToolPrefix + m.target,
			Arguments: `{"reason":"user needs math help"}`,
		}},
	}, nil
}

func (m mockTransferChatModel) WithTools([]*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return m, nil
}

var _ modeliface.ToolCallingChatModel = mockTransferChatModel{}

// mockFinalChatModel always returns a plain text response (no tool calls).
type mockFinalChatModel struct {
	text string
}

func (m mockFinalChatModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}

func (m mockFinalChatModel) Generate(_ context.Context, _ []*types.Message, _ ...types.CallOption) (*types.Message, error) {
	return &types.Message{
		Role:    types.RoleAssistant,
		Content: m.text,
	}, nil
}

func (m mockFinalChatModel) WithTools([]*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return m, nil
}

var _ modeliface.ToolCallingChatModel = mockFinalChatModel{}

// captureSystemChatModel records the last system message content from Generate input.
type captureSystemChatModel struct {
	mu         sync.Mutex
	lastSystem string
}

func (m *captureSystemChatModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}

func (m *captureSystemChatModel) Generate(_ context.Context, input []*types.Message, _ ...types.CallOption) (*types.Message, error) {
	for _, msg := range input {
		if msg != nil && msg.Role == types.RoleSystem {
			m.mu.Lock()
			m.lastSystem = msg.Content
			m.mu.Unlock()
			break
		}
	}
	return &types.Message{
		Role:    types.RoleAssistant,
		Content: "ok",
	}, nil
}

func (m *captureSystemChatModel) WithTools([]*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return m, nil
}

func (m *captureSystemChatModel) LastSystem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSystem
}

var _ modeliface.ToolCallingChatModel = (*captureSystemChatModel)(nil)

func collectEvents(ch <-chan *event.RuntimeEvent) []*event.RuntimeEvent {
	var events []*event.RuntimeEvent
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func findQueryEnd(events []*event.RuntimeEvent) *event.QueryEndPayload {
	for i := len(events) - 1; i >= 0; i-- {
		if qe := events[i].QueryEnd(); qe != nil {
			return qe
		}
	}
	return nil
}

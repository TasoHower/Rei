package agent

import (
	"context"

	modeliface "github.com/TasoHower/rei/loopForge/pkg/model/interface"
	"github.com/TasoHower/rei/loopForge/pkg/model/types"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/event"
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
			Name:      TransferToolPrefix + m.target,
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

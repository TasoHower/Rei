package agent

import (
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"testing"

	"loopforge/pkg/model"
	modeliface "loopforge/pkg/model/interface"
	"loopforge/pkg/model/types"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/outcome"
	"loopforge/pkg/runtime/request"
)

// mockToolLoopChatModel simulates one tool round then a final assistant text (no network).
type mockToolLoopChatModel struct{}

func (mockToolLoopChatModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
	m, err := mockToolLoopChatModel{}.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return types.NewSliceStreamReader([]*types.Message{m}), nil
}

func (mockToolLoopChatModel) Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error) {
	_ = ctx
	_ = opts
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role == types.RoleTool {
			return &types.Message{
				Role:    types.RoleAssistant,
				Content: "Tool returned; the sum is confirmed.",
			}, nil
		}
	}
	return &types.Message{
		Role:    types.RoleAssistant,
		Content: "",
		ToolCalls: []types.ToolCallPart{{
			ID:        "call_mock_1",
			Name:      "demo_add",
			Arguments: `{"a":3,"b":4}`,
		}},
	}, nil
}

func (mockToolLoopChatModel) WithTools([]*types.ToolInfo) (modeliface.ToolCallingChatModel, error) {
	return mockToolLoopChatModel{}, nil
}

var _ modeliface.ToolCallingChatModel = mockToolLoopChatModel{}

func TestAgent_toolLoop_invokesHandle(t *testing.T) {
	ctx := context.Background()
	var invoked bool
	infos := []*model.ToolInfo{
		{
			Name:        "demo_add",
			Description: "Add a and b.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{"type": "number"},
					"b": map[string]interface{}{"type": "number"},
				},
				"required": []string{"a", "b"},
			},
			Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
				_ = ctx
				invoked = true
				var args struct {
					A float64 `json:"a"`
					B float64 `json:"b"`
				}
				if err := sonic.UnmarshalString(argumentsJSON, &args); err != nil {
					return "", err
				}
				return fmt.Sprintf("%.0f", args.A+args.B), nil
			},
		},
	}

	a := New(mockToolLoopChatModel{},
		WithToolInfos(infos),
		WithMaxSteps(6),
	)
	ch := a.Run(ctx, &request.RuntimeRequest{
		SessionID:   "test-session",
		UserMessage: "Use demo_add.",
		Options:     request.RuntimeOptions{},
	})
	events := collectEvents(ch)

	if !invoked {
		t.Fatal("expected ToolInfo.Handle to run")
	}

	qe := findQueryEnd(events)
	if qe == nil || qe.Outcome == nil {
		t.Fatal("missing QueryEndPayload in event stream")
	}
	if qe.Outcome.Termination != outcome.TerminationCompleted {
		t.Fatalf("termination: got %q", qe.Outcome.Termination)
	}
	if qe.Outcome.FinalText == "" {
		t.Fatal("empty final text")
	}

	var sawToolStart, sawToolEnd bool
	for _, ev := range events {
		if ev.Type == event.EventToolCallStart {
			sawToolStart = true
		}
		if ev.Type == event.EventToolCallEnd {
			sawToolEnd = true
		}
	}
	if !sawToolStart {
		t.Fatal("expected EventToolCallStart in stream")
	}
	if !sawToolEnd {
		t.Fatal("expected EventToolCallEnd in stream")
	}
}

func TestAgent_toolLoop_noTransferRegression(t *testing.T) {
	ctx := context.Background()
	a := New(mockFinalChatModel{text: "standalone response"},
		WithMaxSteps(4),
	)
	ch := a.Run(ctx, &request.RuntimeRequest{
		SessionID:   "test-standalone",
		UserMessage: "hello",
	})
	events := collectEvents(ch)

	qe := findQueryEnd(events)
	if qe == nil || qe.Outcome == nil {
		t.Fatal("missing QueryEndPayload")
	}
	if qe.Outcome.Termination != outcome.TerminationCompleted {
		t.Fatalf("termination=%q", qe.Outcome.Termination)
	}
	if qe.Outcome.FinalText != "standalone response" {
		t.Fatalf("FinalText=%q", qe.Outcome.FinalText)
	}
	if len(qe.Outcome.TransferChain) != 0 {
		t.Fatalf("TransferChain should be empty, got %v", qe.Outcome.TransferChain)
	}
}

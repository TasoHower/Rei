package main

import (
	"context"
	"strings"

	"github.com/agentizen/agent-sdk-go/pkg/model"
)

// MockProvider resolves a single mock model for offline demos (no API keys).
type MockProvider struct{}

// GetModel implements model.Provider.
func (MockProvider) GetModel(name string) (model.Model, error) {
	_ = name
	return &MockModel{}, nil
}

// MockModel drives a tiny scripted conversation to exercise runner.Run without a real LLM.
type MockModel struct{}

const toolLoopKeyword = "WITH_TOOL"

// GetResponse implements model.Model.
func (m *MockModel) GetResponse(ctx context.Context, request *model.Request) (*model.Response, error) {
	_ = ctx
	if request == nil {
		return &model.Response{Content: "empty request"}, nil
	}
	if inputHasToolResult(request.Input) {
		return &model.Response{
			Content: "Final: tool results are in context; the demo_echo tool was used as intended.",
		}, nil
	}
	text := flattenUserText(request.Input)
	if strings.Contains(strings.ToUpper(text), toolLoopKeyword) && len(request.Tools) > 0 {
		return &model.Response{
			Content: "Calling demo_echo once.",
			ToolCalls: []model.ToolCall{
				{
					ID:   "demo_call_1",
					Name: "demo_echo",
					Parameters: map[string]interface{}{
						"text": "loopforge-demo",
					},
				},
			},
		}, nil
	}
	return &model.Response{
		Content: "Mock reply: " + text,
	}, nil
}

// StreamResponse implements model.Model.
func (m *MockModel) StreamResponse(ctx context.Context, request *model.Request) (<-chan model.StreamEvent, error) {
	ch := make(chan model.StreamEvent, 8)
	go func() {
		defer close(ch)
		resp, err := m.GetResponse(ctx, request)
		if err != nil {
			ch <- model.StreamEvent{Type: model.StreamEventTypeError, Error: err}
			return
		}
		if resp.Content != "" {
			ch <- model.StreamEvent{Type: model.StreamEventTypeContent, Content: resp.Content}
		}
		for i := range resp.ToolCalls {
			tc := resp.ToolCalls[i]
			ch <- model.StreamEvent{Type: model.StreamEventTypeToolCall, ToolCall: &tc}
		}
		ch <- model.StreamEvent{Type: model.StreamEventTypeDone, Response: resp, Done: true}
	}()
	return ch, nil
}

func inputHasToolResult(input interface{}) bool {
	list, ok := input.([]interface{})
	if !ok {
		return false
	}
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); t == "tool_result" {
			return true
		}
	}
	return false
}

func flattenUserText(input interface{}) string {
	switch v := input.(type) {
	case string:
		return v
	case []interface{}:
		var b strings.Builder
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); t != "message" {
				continue
			}
			if role, _ := m["role"].(string); role != "user" {
				continue
			}
			if c, ok := m["content"].(string); ok {
				b.WriteString(c)
			}
		}
		return b.String()
	default:
		return ""
	}
}

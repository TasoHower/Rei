package lark

import (
	"fmt"
	"strings"

	lferrors "loopforge/pkg/errors"
	lpmodel "loopforge/pkg/model"

	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

func stringPtr(s string) *string { return &s }

func toArkMessages(msgs []*lpmodel.Message) ([]*arkmodel.ChatCompletionMessage, error) {
	var sysParts []string
	var rest []*lpmodel.Message
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == lpmodel.RoleSystem {
			sysParts = append(sysParts, m.Content)
			continue
		}
		rest = append(rest, m)
	}
	out := make([]*arkmodel.ChatCompletionMessage, 0, len(rest)+1)
	if sys := strings.TrimSpace(strings.Join(sysParts, "\n\n")); sys != "" {
		out = append(out, &arkmodel.ChatCompletionMessage{
			Role:    arkmodel.ChatMessageRoleSystem,
			Content: &arkmodel.ChatCompletionMessageContent{StringValue: stringPtr(sys)},
		})
	}
	for _, m := range rest {
		am, err := oneArkMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, am)
	}
	return out, nil
}

func oneArkMessage(m *lpmodel.Message) (*arkmodel.ChatCompletionMessage, error) {
	switch m.Role {
	case lpmodel.RoleUser:
		return &arkmodel.ChatCompletionMessage{
			Role:    arkmodel.ChatMessageRoleUser,
			Content: &arkmodel.ChatCompletionMessageContent{StringValue: stringPtr(m.Content)},
		}, nil
	case lpmodel.RoleAssistant:
		cm := &arkmodel.ChatCompletionMessage{
			Role:    arkmodel.ChatMessageRoleAssistant,
			Content: &arkmodel.ChatCompletionMessageContent{StringValue: stringPtr(m.Content)},
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, &arkmodel.ToolCall{
					ID:   tc.ID,
					Type: arkmodel.ToolTypeFunction,
					Function: arkmodel.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		return cm, nil
	case lpmodel.RoleTool:
		return &arkmodel.ChatCompletionMessage{
			Role:       arkmodel.ChatMessageRoleTool,
			ToolCallID: m.ToolCallID,
			Content:    &arkmodel.ChatCompletionMessageContent{StringValue: stringPtr(m.Content)},
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", lferrors.ErrUnsupportedRole, m.Role)
	}
}

func toArkTools(tools []*lpmodel.ToolInfo) []*arkmodel.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]*arkmodel.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		params := t.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object"}
		}
		out = append(out, &arkmodel.Tool{
			Type: arkmodel.ToolTypeFunction,
			Function: &arkmodel.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func fromArkResponse(r *arkmodel.ChatCompletionResponse) *lpmodel.Message {
	if r == nil || len(r.Choices) == 0 {
		return nil
	}
	ch := r.Choices[0]
	msg := ch.Message
	out := &lpmodel.Message{
		Role:    lpmodel.RoleAssistant,
		Content: chatMessageStringContent(msg.Content),
	}
	for _, tc := range msg.ToolCalls {
		if tc == nil {
			continue
		}
		args := ""
		if tc.Function.Arguments != "" {
			args = tc.Function.Arguments
		}
		out.ToolCalls = append(out.ToolCalls, lpmodel.ToolCallPart{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	out.InputTokens = int64(r.Usage.PromptTokens)
	out.OutputTokens = int64(r.Usage.CompletionTokens)
	return out
}

func chatMessageStringContent(c *arkmodel.ChatCompletionMessageContent) string {
	if c == nil {
		return ""
	}
	if c.StringValue != nil {
		return *c.StringValue
	}
	return ""
}

package deepseek

import (
	"fmt"

	lferrors "github.com/TasoHower/rei/loopForge/pkg/errors"
	lpmodel "github.com/TasoHower/rei/loopForge/pkg/model"

	dspk "github.com/cohesion-org/deepseek-go"
)

func toDeepSeekMessages(msgs []*lpmodel.Message) ([]dspk.ChatCompletionMessage, error) {
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
	out := make([]dspk.ChatCompletionMessage, 0, len(rest)+1)
	if len(sysParts) > 0 {
		sys := sysParts[0]
		for i := 1; i < len(sysParts); i++ {
			sys += "\n\n" + sysParts[i]
		}
		if sys != "" {
			out = append(out, dspk.ChatCompletionMessage{
				Role:    dspk.ChatMessageRoleSystem,
				Content: sys,
			})
		}
	}
	for _, m := range rest {
		dm, err := oneDeepSeekMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, *dm)
	}
	return out, nil
}

func oneDeepSeekMessage(m *lpmodel.Message) (*dspk.ChatCompletionMessage, error) {
	switch m.Role {
	case lpmodel.RoleUser:
		return &dspk.ChatCompletionMessage{
			Role:    dspk.ChatMessageRoleUser,
			Content: m.Content,
		}, nil
	case lpmodel.RoleAssistant:
		cm := &dspk.ChatCompletionMessage{
			Role:             dspk.ChatMessageRoleAssistant,
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
		}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, dspk.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: dspk.ToolCallFunction{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}
		return cm, nil
	case lpmodel.RoleTool:
		return &dspk.ChatCompletionMessage{
			Role:       dspk.ChatMessageRoleTool,
			ToolCallID: m.ToolCallID,
			Content:    m.Content,
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", lferrors.ErrUnsupportedRole, m.Role)
	}
}

func toDeepSeekTools(tools []*lpmodel.ToolInfo) []dspk.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]dspk.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		params := deepSeekToolParameters(t.Parameters)
		out = append(out, dspk.Tool{
			Type: "function",
			Function: dspk.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func deepSeekToolParameters(params map[string]interface{}) *dspk.FunctionParameters {
	if params == nil {
		return &dspk.FunctionParameters{Type: "object"}
	}
	t, ok := params["type"].(string)
	if !ok {
		t = "object"
	}
	props, _ := params["properties"].(map[string]interface{})
	reqRaw, _ := params["required"].([]interface{})
	required := make([]string, 0, len(reqRaw))
	for _, r := range reqRaw {
		if s, ok := r.(string); ok {
			required = append(required, s)
		}
	}
	return &dspk.FunctionParameters{
		Type:       t,
		Properties: props,
		Required:   required,
	}
}

func fromDeepSeekResponse(r *dspk.ChatCompletionResponse) *lpmodel.Message {
	if r == nil || len(r.Choices) == 0 {
		return nil
	}
	ch := r.Choices[0]
	out := &lpmodel.Message{
		Role:             lpmodel.RoleAssistant,
		Content:          ch.Message.Content,
		ReasoningContent: ch.Message.ReasoningContent,
	}
	for _, tc := range ch.Message.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, lpmodel.ToolCallPart{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	out.InputTokens = int64(r.Usage.PromptTokens)
	out.OutputTokens = int64(r.Usage.CompletionTokens)
	return out
}

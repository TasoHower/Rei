package openai

import (
	"fmt"
	"strings"

	lferrors "loopforge/pkg/errors"
	lpmodel "loopforge/pkg/model"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
)

func toOpenAIMessages(msgs []*lpmodel.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
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
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(rest)+1)
	if sys := strings.TrimSpace(strings.Join(sysParts, "\n\n")); sys != "" {
		out = append(out, openai.SystemMessage(sys))
	}
	for _, m := range rest {
		u, err := oneOpenAIMessage(m)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func oneOpenAIMessage(m *lpmodel.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch m.Role {
	case lpmodel.RoleUser:
		return openai.UserMessage(m.Content), nil

	case lpmodel.RoleAssistant:
		content := m.Content
		if m.ReasoningContent != "" && content == "" {
			content = m.ReasoningContent
		}
		assistant := openai.ChatCompletionAssistantMessageParam{
			Content: openai.ChatCompletionAssistantMessageParamContentUnion{OfString: param.NewOpt(content)},
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]openai.ChatCompletionMessageToolCallParam, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, openai.ChatCompletionMessageToolCallParam{
					ID:   tc.ID,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
			assistant.ToolCalls = tcs
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}, nil

	case lpmodel.RoleTool:
		return openai.ToolMessage(m.Content, m.ToolCallID), nil

	default:
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("%w: %q", lferrors.ErrUnsupportedRole, m.Role)
	}
}

func toOpenAITools(tools []*lpmodel.ToolInfo) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		params := shared.FunctionParameters(t.Parameters)
		if params == nil {
			params = shared.FunctionParameters{"type": "object"}
		}
		out = append(out, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				Parameters:  params,
			},
		})
	}
	return out
}

func fromOpenAIResponse(choice *openai.ChatCompletionChoice) *lpmodel.Message {
	if choice == nil {
		return nil
	}
	msg := choice.Message
	out := &lpmodel.Message{
		Role:    lpmodel.RoleAssistant,
		Content: msg.Content,
	}
	for _, tc := range msg.ToolCalls {
		if tc.ID == "" && tc.Function.Name == "" {
			continue
		}
		out.ToolCalls = append(out.ToolCalls, lpmodel.ToolCallPart{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out
}

func mergeToolLists(bound, perCall []*lpmodel.ToolInfo) []*lpmodel.ToolInfo {
	if len(perCall) > 0 {
		return perCall
	}
	return bound
}

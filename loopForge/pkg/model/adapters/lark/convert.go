package lark

import (
	"encoding/json"
	"fmt"
	"strings"

	lferrors "github.com/TasoHower/rei/loopForge/pkg/errors"
	lpmodel "github.com/TasoHower/rei/loopForge/pkg/model"

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
		var rc *string
		if m.ReasoningContent != "" {
			rc = stringPtr(m.ReasoningContent)
		}
		cm := &arkmodel.ChatCompletionMessage{
			Role:             arkmodel.ChatMessageRoleAssistant,
			Content:          &arkmodel.ChatCompletionMessageContent{StringValue: stringPtr(m.Content)},
			ReasoningContent: rc,
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

// arkKeywordsRejectedByDoubao strips JSON Schema keys that Ark / Doubao chat
// completions reject with 400 (e.g. InvalidParameter around function format).
func arkStripUnsupportedSchemaKeywords(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			switch k {
			case "additionalProperties", "patternProperties", "unevaluatedProperties":
				continue
			default:
				out[k] = arkStripUnsupportedSchemaKeywords(val)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, e := range x {
			out[i] = arkStripUnsupportedSchemaKeywords(e)
		}
		return out
	default:
		return v
	}
}

// arkEnsurePropertyTypes adds a minimal "type" on property sub-schemas when
// missing; Ark validators reject some tool definitions otherwise.
func arkEnsurePropertyTypes(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	if props, ok := m["properties"].(map[string]interface{}); ok {
		for _, pv := range props {
			pm, ok := pv.(map[string]interface{})
			if !ok {
				continue
			}
			if _, has := pm["type"]; !has {
				switch {
				case pm["properties"] != nil:
					pm["type"] = "object"
				case pm["items"] != nil:
					pm["type"] = "array"
				default:
					pm["type"] = "string"
				}
			}
			arkEnsurePropertyTypes(pm)
		}
	}
	if items, ok := m["items"].(map[string]interface{}); ok {
		arkEnsurePropertyTypes(items)
	}
	if items, ok := m["items"].([]interface{}); ok {
		for _, e := range items {
			arkEnsurePropertyTypes(e)
		}
	}
}

func arkToolParametersForAPI(params map[string]interface{}) map[string]interface{} {
	if params == nil {
		return map[string]interface{}{"type": "object"}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return params
	}
	var root interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return params
	}
	root = arkStripUnsupportedSchemaKeywords(root)
	arkEnsurePropertyTypes(root)
	out, ok := root.(map[string]interface{})
	if !ok {
		return params
	}
	return out
}

// SanitizeToolParametersForAPI applies the same JSON Schema normalization the Lark adapter uses
// before sending tool definitions to Ark. MCP-derived ToolInfo.Parameters must use this path.
func SanitizeToolParametersForAPI(params map[string]interface{}) map[string]interface{} {
	return arkToolParametersForAPI(params)
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
		params := arkToolParametersForAPI(t.Parameters)
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
	reasoningContent := ""
	if msg.ReasoningContent != nil {
		reasoningContent = *msg.ReasoningContent
	}
	out := &lpmodel.Message{
		Role:             lpmodel.RoleAssistant,
		Content:          chatMessageStringContent(msg.Content),
		ReasoningContent: reasoningContent,
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

package agentsdk

import (
	"encoding/json"
	"fmt"
	"strings"

	lpmodel "loopforge/pkg/model"

	sdkmodel "github.com/agentizen/agent-sdk-go/pkg/model"
)

// MessagesToSDKRequest maps loopForge messages to an agent-sdk-go model.Request.
func MessagesToSDKRequest(
	msgs []*lpmodel.Message,
	tools []interface{},
	settings *sdkmodel.Settings,
) (*sdkmodel.Request, error) {
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
	sysJoined := strings.Join(sysParts, "\n\n")

	if len(rest) == 0 {
		return &sdkmodel.Request{
			SystemInstructions: sysJoined,
			Input:              "",
			Tools:              tools,
			Settings:           settings,
		}, nil
	}

	items, err := messagesToInputList(rest)
	if err != nil {
		return nil, err
	}

	return &sdkmodel.Request{
		SystemInstructions: sysJoined,
		Input:              items,
		Tools:              tools,
		Settings:           settings,
	}, nil
}

func messagesToInputList(msgs []*lpmodel.Message) ([]interface{}, error) {
	out := make([]interface{}, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case lpmodel.RoleUser:
			out = append(out, map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": m.Content,
			})
		case lpmodel.RoleAssistant:
			mm := map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": m.Content,
			}
			if len(m.ToolCalls) > 0 {
				tcs := make([]map[string]interface{}, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					tcs = append(tcs, map[string]interface{}{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      tc.Name,
							"arguments": tc.Arguments,
						},
					})
				}
				mm["tool_calls"] = tcs
			}
			out = append(out, mm)
		case lpmodel.RoleTool:
			out = append(out, toolResultMap(m))
		default:
			return nil, fmt.Errorf("unsupported message role %q", m.Role)
		}
	}
	return out, nil
}

func toolResultMap(m *lpmodel.Message) map[string]interface{} {
	return map[string]interface{}{
		"type": "tool_result",
		"tool_call": map[string]interface{}{
			"id":         m.ToolCallID,
			"name":       m.Name,
			"parameters": map[string]interface{}{},
		},
		"tool_result": map[string]interface{}{
			"content": m.Content,
		},
	}
}

// FromSDKResponse maps an agent-sdk-go model.Response to a single assistant Message.
func FromSDKResponse(r *sdkmodel.Response) *lpmodel.Message {
	if r == nil {
		return nil
	}
	out := &lpmodel.Message{
		Role:    lpmodel.RoleAssistant,
		Content: r.Content,
	}
	for _, tc := range r.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, lpmodel.ToolCallPart{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: sdkToolCallArgumentsJSON(&tc),
		})
	}
	if r.Usage != nil {
		out.InputTokens = int64(r.Usage.PromptTokens)
		out.OutputTokens = int64(r.Usage.CompletionTokens)
	}
	return out
}

// sdkToolCallArgumentsJSON prefers the raw JSON string from the provider; otherwise marshals Parameters.
// This keeps downstream tool execution (e.g. loopforge/pkg/tool) aligned with the model JSON.
func sdkToolCallArgumentsJSON(tc *sdkmodel.ToolCall) string {
	if tc == nil {
		return "{}"
	}
	if tc.RawParameter.Len() > 0 {
		return tc.RawParameter.String()
	}
	if len(tc.Parameters) == 0 {
		return "{}"
	}
	if raw, ok := tc.Parameters["raw_arguments"]; ok {
		if s, ok2 := raw.(string); ok2 {
			return s
		}
		b, _ := json.Marshal(raw)
		return string(b)
	}
	b, err := json.Marshal(tc.Parameters)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func mergeToolLists(bound, perCall []*lpmodel.ToolInfo) []*lpmodel.ToolInfo {
	if len(perCall) > 0 {
		return perCall
	}
	return bound
}

func toolsToIface(tools []*lpmodel.ToolInfo) []interface{} {
	if len(tools) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		out = append(out, t.ToOpenAITool())
	}
	return out
}

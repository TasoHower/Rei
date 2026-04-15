package types

// ToolInfo describes a callable function the model may invoke (OpenAI function-calling shape).
// Handle binds the Go function that runs after the model emits a tool call for this name (not sent in ToOpenAITool).
type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]interface{} // JSON Schema object
	Handle      ToolCallHandler          `json:"-"`
}

// ToOpenAITool returns a value suitable for agent-sdk-go Request.Tools ([]interface{} entries).
func (t *ToolInfo) ToOpenAITool() map[string]interface{} {
	if t == nil {
		return nil
	}
	params := t.Parameters
	if params == nil {
		params = map[string]interface{}{"type": "object"}
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  params,
		},
	}
}

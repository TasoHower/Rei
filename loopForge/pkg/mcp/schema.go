package mcp

import (
	"encoding/json"
	"fmt"

	larkadapter "loopforge/pkg/model/adapters/lark"
)

// ParseInputSchema converts an MCP tools/list inputSchema payload into a JSON Schema object map.
// Accepts nil, JSON-encoded string, []byte, json.RawMessage, or map[string]interface{}.
func ParseInputSchema(v any) (map[string]interface{}, error) {
	if v == nil {
		return map[string]interface{}{"type": "object"}, nil
	}

	switch x := v.(type) {
	case map[string]interface{}:
		return cloneStringMap(x), nil
	case json.RawMessage:
		return parseJSONToObject(x)
	case []byte:
		return parseJSONToObject(x)
	case string:
		if x == "" {
			return map[string]interface{}{"type": "object"}, nil
		}
		return parseJSONToObject([]byte(x))
	default:
		raw, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("mcp inputSchema: marshal: %w", err)
		}
		return parseJSONToObject(raw)
	}
}

func parseJSONToObject(raw []byte) (map[string]interface{}, error) {
	var root interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("mcp inputSchema: %w", err)
	}
	m, ok := root.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("mcp inputSchema: want JSON object, got %T", root)
	}
	return m, nil
}

func cloneStringMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// NormalizeInputSchema parses MCP inputSchema and applies Ark-compatible sanitization via the
// Lark adapter single exit (strip unsupported keywords, ensure property types).
func NormalizeInputSchema(v any) (map[string]interface{}, error) {
	parsed, err := ParseInputSchema(v)
	if err != nil {
		return nil, err
	}
	return larkadapter.SanitizeToolParametersForAPI(parsed), nil
}

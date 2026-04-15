package tool

// ToolOrigin classifies where a tool implementation comes from.
type ToolOrigin string

const (
	ToolOriginLocal   ToolOrigin = "local"
	ToolOriginMCP     ToolOrigin = "mcp"
	ToolOriginBuiltin ToolOrigin = "builtin"
)

// ToolDescriptor is what the LLM sees in tools/list equivalent.
type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema string
	Origin      ToolOrigin
	SourceRef   string
}

// ToolCall is emitted by the model (one round may have many).
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ToolResult is fed back into message history for the next loop round.
type ToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// ToolRegistryKey is an internal stable id; exposed Name may differ (MCP prefix).
type ToolRegistryKey struct {
	Origin ToolOrigin
	Name   string
}

package types

// Role identifies who produced a message in a chat transcript.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// ToolCallPart is an assistant-side tool invocation (OpenAI-style arguments JSON string).
// Handle binds the local implementation when you define tools yourself; the model only fills ID/Name/Arguments.
// Resolution order at runtime (see loopforge/pkg/tool.Invoke): Handle on this part, then ToolInfo.Handle by name, then ToolExecutor.
type ToolCallPart struct {
	ID        string
	Name      string
	Arguments string
	// Handle is optional; json tags omitted so this field is never sent on the wire.
	Handle ToolCallHandler `json:"-"`
}

// Message is one turn in a multi-turn chat. Keep fields only for roles you use.
type Message struct {
	Role    Role
	Content string

	// Assistant: optional parallel tool calls.
	ToolCalls []ToolCallPart

	// Tool: result for a prior tool invocation.
	ToolCallID string
	Name       string // tool name (optional, for tracing)

	// ReasoningContent holds the chain-of-thought / reasoning text returned by
	// thinking-capable models (e.g. DeepSeek-R1).  Populated by model adapters;
	// round-tripped via the ReasoningContent field itself, or via Content markers
	// when the upstream model adapter does not know the field.
	ReasoningContent string `json:"-"`

	// Usage: populated by model adapters when available (assistant messages only).
	InputTokens  int64 `json:"-"`
	OutputTokens int64 `json:"-"`
}

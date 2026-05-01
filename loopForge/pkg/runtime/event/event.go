package event

import "github.com/TasoHower/rei/loopForge/pkg/runtime/outcome"

// EventMessageType identifies outbound streaming chunks to the client.
// Values are aligned with multi-agent-server EventMessageType (see doc/design/data-fusion.md §3).
type EventMessageType string

const (
	EventStart         EventMessageType = "start"
	EventQuestion      EventMessageType = "question"
	EventAnswer        EventMessageType = "answer"
	EventToolCallStart EventMessageType = "tool_call_start"
	EventToolCallEnd   EventMessageType = "tool_call_end"
	EventCallLLMStart  EventMessageType = "call_llm_start"
	EventCallLLMEnd    EventMessageType = "call_llm_end"
	EventAgentTransfer EventMessageType = "agent_transfer"
	EventSpawnStart    EventMessageType = "spawn_start"
	EventSpawnEnd      EventMessageType = "spawn_end"
	EventVarChange     EventMessageType = "var_change"
	EventQueryEnd      EventMessageType = "query_end"
	EventError         EventMessageType = "error"
)

// EventPayload is the sealed marker interface for all RuntimeEvent payloads.
// Only types in this package should implement it.
type EventPayload interface {
	eventPayload() EventMessageType
}

// RuntimeEvent is one outbound chunk to the user client.
type RuntimeEvent struct {
	Type    EventMessageType
	RunID   string
	Step    int
	Payload EventPayload
}

// --- type-safe accessors ---

func (e *RuntimeEvent) Start() *StartPayload       { p, _ := e.Payload.(*StartPayload); return p }
func (e *RuntimeEvent) Question() *QuestionPayload { p, _ := e.Payload.(*QuestionPayload); return p }
func (e *RuntimeEvent) Answer() *AnswerPayload     { p, _ := e.Payload.(*AnswerPayload); return p }
func (e *RuntimeEvent) ToolCallStart() *ToolCallStartPayload {
	p, _ := e.Payload.(*ToolCallStartPayload)
	return p
}
func (e *RuntimeEvent) ToolCallEnd() *ToolCallEndPayload {
	p, _ := e.Payload.(*ToolCallEndPayload)
	return p
}
func (e *RuntimeEvent) CallLLMStart() *CallLLMStartPayload {
	p, _ := e.Payload.(*CallLLMStartPayload)
	return p
}
func (e *RuntimeEvent) CallLLMEnd() *CallLLMEndPayload {
	p, _ := e.Payload.(*CallLLMEndPayload)
	return p
}
func (e *RuntimeEvent) AgentTransfer() *AgentTransferPayload {
	p, _ := e.Payload.(*AgentTransferPayload)
	return p
}
func (e *RuntimeEvent) SpawnStart() *SpawnStartPayload {
	p, _ := e.Payload.(*SpawnStartPayload)
	return p
}
func (e *RuntimeEvent) SpawnEnd() *SpawnEndPayload {
	p, _ := e.Payload.(*SpawnEndPayload)
	return p
}
func (e *RuntimeEvent) VarChange() *VarChangePayload { p, _ := e.Payload.(*VarChangePayload); return p }
func (e *RuntimeEvent) QueryEnd() *QueryEndPayload   { p, _ := e.Payload.(*QueryEndPayload); return p }
func (e *RuntimeEvent) Error() *ErrorPayload         { p, _ := e.Payload.(*ErrorPayload); return p }

// --- Payload types ---

type StartPayload struct{}

func (*StartPayload) eventPayload() EventMessageType { return EventStart }

type QuestionPayload struct {
	UserMessage string `json:"user_message"`
}

func (*QuestionPayload) eventPayload() EventMessageType { return EventQuestion }

type AnswerPayload struct {
	Delta       string `json:"delta"`
	IsReasoning bool   `json:"is_reasoning,omitempty"`
	IsFinal     bool   `json:"is_final,omitempty"`
}

func (*AnswerPayload) eventPayload() EventMessageType { return EventAnswer }

// LLMToolSummary is one tool/function definition passed to the chat model (name, description, JSON Schema parameters).
type LLMToolSummary struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type CallLLMStartPayload struct {
	Model       string   `json:"model"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	// AgentName is the running agent template name (e.g. triage, writer).
	AgentName string `json:"agent,omitempty"`
	// MCPServerIDs lists cfg profile IDs when this agent has WithMCPServerProfiles (empty if none).
	MCPServerIDs []string `json:"mcp_server_ids,omitempty"`
	// MCPToolNames lists tool names discovered from MCP tools/list for this run (empty if no MCP or no tools).
	MCPToolNames []string `json:"mcp_tool_names,omitempty"`
	// SystemPrompt is the final system instructions sent on this LLM call
	// (after builders, variable block merge, and VarStore {{}} replacement).
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Tools lists function definitions bound for this LLM call (same order as merged ToolInfos + MCP + extras).
	Tools []LLMToolSummary `json:"tools,omitempty"`
}

func (*CallLLMStartPayload) eventPayload() EventMessageType { return EventCallLLMStart }

// FinishReason indicates why a single LLM call stopped generating.
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishToolCalls FinishReason = "tool_calls"
	FinishLength    FinishReason = "length"
	FinishError     FinishReason = "error"
)

type CallLLMEndPayload struct {
	FinishReason FinishReason `json:"finish_reason"`
	FullText     string       `json:"full_text,omitempty"`
	InputTokens  int64        `json:"input_tokens"`
	OutputTokens int64        `json:"output_tokens"`
}

func (*CallLLMEndPayload) eventPayload() EventMessageType { return EventCallLLMEnd }

type ToolCallStartPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Arguments  string `json:"arguments"`
}

func (*ToolCallStartPayload) eventPayload() EventMessageType { return EventToolCallStart }

type ToolCallEndPayload struct {
	ToolCallID string `json:"tool_call_id"`
	OK         bool   `json:"ok"`
	Output     string `json:"output,omitempty"`
	IsError    bool   `json:"is_error"`
}

func (*ToolCallEndPayload) eventPayload() EventMessageType { return EventToolCallEnd }

// TransferPhase distinguishes agent_transfer start vs end.
type TransferPhase string

const (
	TransferStart TransferPhase = "start"
	TransferEnd   TransferPhase = "end"
)

type AgentTransferPayload struct {
	Phase     TransferPhase `json:"phase"`
	FromAgent string        `json:"from_agent"`
	ToAgent   string        `json:"to_agent"`
	Reason    string        `json:"reason,omitempty"`
}

func (*AgentTransferPayload) eventPayload() EventMessageType { return EventAgentTransfer }

// SpawnStartPayload 描述子 Agent 启动信息。
type SpawnStartPayload struct {
	ChildRunID  string `json:"child_run_id"`
	ParentRunID string `json:"parent_run_id,omitempty"`
	AgentRole   string `json:"agent_role"`
	Depth       int    `json:"depth"`
	TaskSummary string `json:"task_summary,omitempty"`
}

func (*SpawnStartPayload) eventPayload() EventMessageType { return EventSpawnStart }

// SpawnEndPayload 描述子 Agent 结束信息。
type SpawnEndPayload struct {
	ChildRunID   string              `json:"child_run_id"`
	ParentRunID  string              `json:"parent_run_id,omitempty"`
	AgentRole    string              `json:"agent_role"`
	Depth        int                 `json:"depth"`
	OK           bool                `json:"ok"`
	Status       string              `json:"status"`
	ErrorCode    string              `json:"error_code,omitempty"`
	FinalTextLen int                 `json:"final_text_len,omitempty"`
	Metrics      *outcome.RunMetrics `json:"metrics,omitempty"`
}

func (*SpawnEndPayload) eventPayload() EventMessageType { return EventSpawnEnd }

// VarChangePayload is emitted whenever a shared variable is written or deleted.
type VarChangePayload struct {
	// Operation is "set" when a value is assigned, "delete" when the key is removed.
	Operation string `json:"operation"`
	Key       string `json:"key"`
	Value     any    `json:"value,omitempty"`
	// Agent is the name of the agent that triggered the change (empty when system-initiated).
	Agent string `json:"agent,omitempty"`
}

func (*VarChangePayload) eventPayload() EventMessageType { return EventVarChange }

type QueryEndPayload struct {
	Outcome *outcome.RuntimeOutcome `json:"outcome"`
}

func (*QueryEndPayload) eventPayload() EventMessageType { return EventQueryEnd }

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (*ErrorPayload) eventPayload() EventMessageType { return EventError }

// Emit is a convenience constructor: it infers Type from the payload's marker method.
func Emit(runID string, step int, payload EventPayload) *RuntimeEvent {
	return &RuntimeEvent{
		Type:    payload.eventPayload(),
		RunID:   runID,
		Step:    step,
		Payload: payload,
	}
}

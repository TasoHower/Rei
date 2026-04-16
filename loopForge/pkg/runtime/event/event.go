package event

import "loopforge/pkg/runtime/outcome"

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
	EventCallRAGStart  EventMessageType = "call_rag_start"
	EventCallRAGEnd    EventMessageType = "call_rag_end"
	EventAgentTransfer EventMessageType = "agent_transfer"
	EventQueryEnd      EventMessageType = "query_end"
	EventWelcome       EventMessageType = "welcome"
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

func (e *RuntimeEvent) Start() *StartPayload             { p, _ := e.Payload.(*StartPayload); return p }
func (e *RuntimeEvent) Question() *QuestionPayload        { p, _ := e.Payload.(*QuestionPayload); return p }
func (e *RuntimeEvent) Answer() *AnswerPayload            { p, _ := e.Payload.(*AnswerPayload); return p }
func (e *RuntimeEvent) ToolCallStart() *ToolCallStartPayload { p, _ := e.Payload.(*ToolCallStartPayload); return p }
func (e *RuntimeEvent) ToolCallEnd() *ToolCallEndPayload  { p, _ := e.Payload.(*ToolCallEndPayload); return p }
func (e *RuntimeEvent) CallLLMStart() *CallLLMStartPayload { p, _ := e.Payload.(*CallLLMStartPayload); return p }
func (e *RuntimeEvent) CallLLMEnd() *CallLLMEndPayload    { p, _ := e.Payload.(*CallLLMEndPayload); return p }
func (e *RuntimeEvent) CallRAGStart() *CallRAGStartPayload { p, _ := e.Payload.(*CallRAGStartPayload); return p }
func (e *RuntimeEvent) CallRAGEnd() *CallRAGEndPayload    { p, _ := e.Payload.(*CallRAGEndPayload); return p }
func (e *RuntimeEvent) AgentTransfer() *AgentTransferPayload { p, _ := e.Payload.(*AgentTransferPayload); return p }
func (e *RuntimeEvent) QueryEnd() *QueryEndPayload        { p, _ := e.Payload.(*QueryEndPayload); return p }
func (e *RuntimeEvent) Welcome() *WelcomePayload          { p, _ := e.Payload.(*WelcomePayload); return p }
func (e *RuntimeEvent) Error() *ErrorPayload              { p, _ := e.Payload.(*ErrorPayload); return p }

// --- Payload types ---

type StartPayload struct{}

func (*StartPayload) eventPayload() EventMessageType { return EventStart }

type QuestionPayload struct {
	UserMessage string `json:"user_message"`
}

func (*QuestionPayload) eventPayload() EventMessageType { return EventQuestion }

type AnswerPayload struct {
	Delta string `json:"delta"`
}

func (*AnswerPayload) eventPayload() EventMessageType { return EventAnswer }

type CallLLMStartPayload struct {
	Model       string   `json:"model"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
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

type CallRAGStartPayload struct{}

func (*CallRAGStartPayload) eventPayload() EventMessageType { return EventCallRAGStart }

type CallRAGEndPayload struct{}

func (*CallRAGEndPayload) eventPayload() EventMessageType { return EventCallRAGEnd }

// TransferPhase distinguishes agent_transfer start vs end.
type TransferPhase string

const (
	TransferStart TransferPhase = "start"
	TransferEnd   TransferPhase = "end"
)

type AgentTransferPayload struct {
	Phase      TransferPhase `json:"phase"`
	FromAgent  string        `json:"from_agent"`
	ToAgent    string        `json:"to_agent"`
	Reason     string        `json:"reason,omitempty"`
	ChildRunID string        `json:"child_run_id,omitempty"`
	Depth      int           `json:"depth,omitempty"`
	OK         bool          `json:"ok,omitempty"`
}

func (*AgentTransferPayload) eventPayload() EventMessageType { return EventAgentTransfer }

type QueryEndPayload struct {
	Outcome *outcome.RuntimeOutcome `json:"outcome"`
}

func (*QueryEndPayload) eventPayload() EventMessageType { return EventQueryEnd }

type WelcomePayload struct{}

func (*WelcomePayload) eventPayload() EventMessageType { return EventWelcome }

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

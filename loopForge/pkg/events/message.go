package events

// EventMessage is the outbound envelope aligned with multi-agent-server runner event shape.
// Event type constants are aligned with loopForge runtime event (pkg/runtime/event) as the
// authoritative source. See doc/design/data-fusion.md for the alignment map.
type EventMessage struct {
	Code     string                `json:"code"`
	Data     *EventMessageData     `json:"data,omitempty"`
	Type     string                `json:"type"`
	Msg      string                `json:"msg"`
	MetaData *EventMessageMetaData `json:"meta_data,omitempty"`
}

// EventMessageData binds session coordinates and payload for one event.
type EventMessageData struct {
	SegmentCode    string         `json:"segment_code,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	MessageID      string         `json:"message_id,omitempty"`
	EventType      string         `json:"event_type,omitempty"`
	Role           string         `json:"role,omitempty"`
	Answer         string         `json:"answer,omitempty"`
	ContentType    string         `json:"content_type,omitempty"`
	MetaData       map[string]any `json:"meta_data,omitempty"`
}

// EventMessageMetaData is optional outer metadata for adapters.
type EventMessageMetaData struct {
	Extra map[string]any `json:"extra,omitempty"`
}

// EventMessageType enumerates semantic event kinds for UI and observability.
// Values are aligned 1:1 with pkg/runtime/event EventMessageType.
type EventMessageType string

const (
	EventMessageTypeStart         EventMessageType = "start"
	EventMessageTypeQuestion      EventMessageType = "question"
	EventMessageTypeAnswer        EventMessageType = "answer"
	EventMessageTypeToolCallStart EventMessageType = "tool_call_start"
	EventMessageTypeToolCallEnd   EventMessageType = "tool_call_end"
	EventMessageTypeCallLLMStart  EventMessageType = "call_llm_start"
	EventMessageTypeCallLLMEnd    EventMessageType = "call_llm_end"
	EventMessageTypeAgentTransfer EventMessageType = "agent_transfer"
	EventMessageTypeSpawnStart    EventMessageType = "spawn_start"
	EventMessageTypeSpawnEnd      EventMessageType = "spawn_end"
	EventMessageTypeVarChange     EventMessageType = "var_change"
	EventMessageTypeQueryEnd      EventMessageType = "query_end"
	EventMessageTypeError         EventMessageType = "error"
)

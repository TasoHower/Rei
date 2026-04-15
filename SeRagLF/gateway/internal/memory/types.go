package memory

import "time"

type Message struct {
	Role      string `json:"role" jsonschema:"Message author role, typically user, assistant, or system"`
	Content   string `json:"content" jsonschema:"Full message text to fold into distilled context"`
	Timestamp int64  `json:"timestamp" jsonschema:"Unix milliseconds; use 0 if unknown"`
}

type Fact struct {
	ID                     int64     `json:"id,omitempty"`
	UserID                 string    `json:"user_id"`
	Category               string    `json:"category"`
	FactKey                string    `json:"fact_key"`
	FactValue              string    `json:"fact_value"`
	Confidence             float64   `json:"confidence,omitempty"`
	Score                  float64   `json:"score,omitempty"`
	SourceConversationID   string    `json:"source_conversation_id,omitempty"`
	CreatedAt              time.Time `json:"created_at,omitempty"`
	UpdatedAt              time.Time `json:"updated_at,omitempty"`
}

type DistilledContext struct {
	ConversationID string   `json:"conversation_id"`
	Topics         []string `json:"topics"`
	Decisions      []string `json:"decisions"`
	Entities       []string `json:"entities"`
	ActionItems    []string `json:"action_items"`
	CurrentTask    string   `json:"current_task"`
	Narrative      string   `json:"narrative"`
	UpdatedAt      int64    `json:"updated_at"`
	Version        int      `json:"version"`
}

type ConversationSummary struct {
	UserID         string  `json:"user_id"`
	ConversationID string  `json:"conversation_id"`
	Summary        string  `json:"summary"`
	Score          float64 `json:"score,omitempty"`
	CreatedAt      int64   `json:"created_at"`
}

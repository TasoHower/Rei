package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type Extractor struct {
	llm model.BaseChatModel
	log *slog.Logger
}

func NewExtractor(llm model.BaseChatModel, log *slog.Logger) *Extractor {
	return &Extractor{llm: llm, log: log}
}

const extractFactsPrompt = `Analyze the following conversation and extract structured facts about the user.
Output a JSON array where each element has the following fields:
- "category": one of "preference", "background", "expertise", "other"
- "fact_key": a short identifier for the fact
- "fact_value": a description of the fact
- "confidence": a float between 0.0 and 1.0

Only extract facts that are explicitly stated or strongly implied. Do not speculate.
If no facts can be extracted, output an empty array [].
Output ONLY the JSON array, no other text.

Conversation:
%s`

const summarizePrompt = `Compress the following conversation into a concise summary (100-200 words) that includes:
1. Main topics discussed
2. Key decisions or conclusions
3. Action items (if any)

Do not include greetings or other meaningless content.
Output ONLY the summary text.

Conversation:
%s`

func (e *Extractor) ExtractFacts(ctx context.Context, messages []Message) ([]Fact, error) {
	start := time.Now()
	conv := FormatConversation(messages)
	prompt := fmt.Sprintf(extractFactsPrompt, conv)

	resp, err := e.llm.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return nil, fmt.Errorf("llm extract facts: %w", err)
	}

	text := strings.TrimSpace(resp.Content)
	text = ExtractJSONArray(text)

	var raw []struct {
		Category   string  `json:"category"`
		FactKey    string  `json:"fact_key"`
		FactValue  string  `json:"fact_value"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse facts JSON: %w (raw: %s)", err, Truncate(text, 200))
	}

	facts := make([]Fact, 0, len(raw))
	for _, r := range raw {
		if r.FactKey == "" || r.FactValue == "" {
			continue
		}
		if r.Confidence <= 0 {
			r.Confidence = 0.8
		}
		facts = append(facts, Fact{
			Category:   r.Category,
			FactKey:    r.FactKey,
			FactValue:  r.FactValue,
			Confidence: r.Confidence,
		})
	}
	e.log.Info("facts extracted", "count", len(facts), "messages", len(messages), "duration_ms", time.Since(start).Milliseconds())
	return facts, nil
}

const distillPrompt = `You are a context distillation engine. Given the EXISTING context summary and NEW messages from a conversation, produce an UPDATED context summary.

Rules:
1. Preserve all important information from the existing context
2. Incorporate key information from the new messages
3. Remove outdated information that has been superseded
4. Keep the output concise but complete

Output ONLY a JSON object with this exact schema:
{
  "topics": ["topic1", "topic2"],
  "decisions": ["decision made about X"],
  "entities": ["entity1", "entity2"],
  "action_items": ["todo1"],
  "current_task": "what is being worked on right now",
  "narrative": "A 2-3 sentence natural language summary of the conversation state"
}

EXISTING CONTEXT:
%s

NEW MESSAGES:
%s`

func (e *Extractor) Distill(ctx context.Context, existing *DistilledContext, newMessages []Message) (*DistilledContext, error) {
	start := time.Now()
	existingStr := "None (this is the start of the conversation)"
	if existing != nil {
		data, err := json.Marshal(existing)
		if err != nil {
			return nil, fmt.Errorf("marshal existing context: %w", err)
		}
		existingStr = string(data)
	}

	conv := FormatConversation(newMessages)
	prompt := fmt.Sprintf(distillPrompt, existingStr, conv)

	resp, err := e.llm.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return nil, fmt.Errorf("llm distill: %w", err)
	}

	text := strings.TrimSpace(resp.Content)
	text = ExtractJSONObject(text)

	var dc DistilledContext
	if err := json.Unmarshal([]byte(text), &dc); err != nil {
		return nil, fmt.Errorf("parse distilled context: %w (raw: %s)", err, Truncate(text, 300))
	}

	version := 1
	if existing != nil {
		version = existing.Version + 1
	}
	dc.Version = version

	e.log.Info("context distilled", "version", dc.Version, "new_messages", len(newMessages), "topics", len(dc.Topics), "duration_ms", time.Since(start).Milliseconds())
	return &dc, nil
}

func (e *Extractor) Summarize(ctx context.Context, messages []Message) (string, error) {
	start := time.Now()
	conv := FormatConversation(messages)
	prompt := fmt.Sprintf(summarizePrompt, conv)

	resp, err := e.llm.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return "", fmt.Errorf("llm summarize: %w", err)
	}
	summary := strings.TrimSpace(resp.Content)
	e.log.Info("conversation summarized", "messages", len(messages), "summary_length", len(summary), "duration_ms", time.Since(start).Milliseconds())
	return summary, nil
}

func FormatConversation(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s]: %s\n", m.Role, m.Content)
	}
	return b.String()
}

func ExtractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func ExtractJSONArray(s string) string {
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

package memory_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/memory"
	"seraglf/internal/testutil"
)

func newTestLogger() *slog.Logger {
	return slog.Default()
}

func TestExtractFacts_Success(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: `[
				{"category": "preference", "fact_key": "language", "fact_value": "Go", "confidence": 0.9},
				{"category": "background", "fact_key": "role", "fact_value": "engineer", "confidence": 0.85}
			]`}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	facts, err := ext.ExtractFacts(context.Background(), []memory.Message{
		{Role: "user", Content: "I love Go programming"},
	})
	require.NoError(t, err)
	require.Len(t, facts, 2)
	assert.Equal(t, "preference", facts[0].Category)
	assert.Equal(t, "language", facts[0].FactKey)
	assert.Equal(t, "Go", facts[0].FactValue)
	assert.Equal(t, 0.9, facts[0].Confidence)
}

func TestExtractFacts_EmptyArray(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: `[]`}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	facts, err := ext.ExtractFacts(context.Background(), []memory.Message{
		{Role: "user", Content: "hello"},
	})
	require.NoError(t, err)
	assert.Empty(t, facts)
}

func TestExtractFacts_LLMError(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("llm unavailable")
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	_, err := ext.ExtractFacts(context.Background(), []memory.Message{
		{Role: "user", Content: "test"},
	})
	assert.Error(t, err)
}

func TestExtractFacts_InvalidJSON(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: `not valid json`}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	_, err := ext.ExtractFacts(context.Background(), []memory.Message{
		{Role: "user", Content: "test"},
	})
	assert.Error(t, err)
}

func TestExtractFacts_SkipEmptyKeys(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: `[
				{"category": "preference", "fact_key": "", "fact_value": "Go", "confidence": 0.9},
				{"category": "preference", "fact_key": "lang", "fact_value": "", "confidence": 0.9},
				{"category": "preference", "fact_key": "editor", "fact_value": "vim", "confidence": 0.8}
			]`}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	facts, err := ext.ExtractFacts(context.Background(), []memory.Message{
		{Role: "user", Content: "test"},
	})
	require.NoError(t, err)
	assert.Len(t, facts, 1, "should skip entries with empty fact_key or fact_value")
	assert.Equal(t, "editor", facts[0].FactKey)
}

func TestDistill_FirstMessage(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: `{
				"topics": ["greeting"],
				"decisions": [],
				"entities": [],
				"action_items": [],
				"current_task": "greeting",
				"narrative": "User said hello"
			}`}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	dc, err := ext.Distill(context.Background(), nil, []memory.Message{
		{Role: "user", Content: "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, dc.Version)
	assert.Contains(t, dc.Topics, "greeting")
}

func TestDistill_Incremental(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: `{
				"topics": ["greeting", "coding"],
				"decisions": ["use Go"],
				"entities": ["Go"],
				"action_items": [],
				"current_task": "coding",
				"narrative": "Discussing Go programming"
			}`}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	existing := &memory.DistilledContext{Version: 2}
	dc, err := ext.Distill(context.Background(), existing, []memory.Message{
		{Role: "user", Content: "let's code in Go"},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, dc.Version)
}

func TestDistill_LLMError(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("llm error")
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	_, err := ext.Distill(context.Background(), nil, []memory.Message{
		{Role: "user", Content: "hello"},
	})
	assert.Error(t, err)
}

func TestSummarize_Success(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: "The user discussed Go programming and testing strategies."}, nil
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	summary, err := ext.Summarize(context.Background(), []memory.Message{
		{Role: "user", Content: "let's talk about Go testing"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, summary)
	assert.Contains(t, summary, "Go")
}

func TestSummarize_LLMError(t *testing.T) {
	llm := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("llm error")
		},
	}
	ext := memory.NewExtractor(llm, newTestLogger())

	_, err := ext.Summarize(context.Background(), []memory.Message{
		{Role: "user", Content: "hello"},
	})
	assert.Error(t, err)
}

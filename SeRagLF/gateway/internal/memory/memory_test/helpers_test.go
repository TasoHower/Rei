package memory_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seraglf/internal/memory"
)

func TestFormatConversation(t *testing.T) {
	t.Run("multiple messages", func(t *testing.T) {
		msgs := []memory.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "how are you"},
		}
		result := memory.FormatConversation(msgs)
		assert.Contains(t, result, "[user]: hello")
		assert.Contains(t, result, "[assistant]: hi there")
		assert.Contains(t, result, "[user]: how are you")
	})

	t.Run("empty slice", func(t *testing.T) {
		result := memory.FormatConversation(nil)
		assert.Equal(t, "", result)
	})
}

func TestExtractJSONObject(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "with surrounding text",
			in:   `Here is the result: {"key": "value"} done.`,
			want: `{"key": "value"}`,
		},
		{
			name: "no JSON",
			in:   "plain text without json",
			want: "plain text without json",
		},
		{
			name: "nested braces",
			in:   `prefix {"outer": {"inner": 1}} suffix`,
			want: `{"outer": {"inner": 1}}`,
		},
		{
			name: "just JSON",
			in:   `{"a": 1}`,
			want: `{"a": 1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := memory.ExtractJSONObject(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractJSONArray(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "with surrounding text",
			in:   `Result: [{"a":1},{"b":2}] end`,
			want: `[{"a":1},{"b":2}]`,
		},
		{
			name: "no JSON array",
			in:   "no array here",
			want: "no array here",
		},
		{
			name: "nested brackets",
			in:   `start [[1,2],[3,4]] end`,
			want: `[[1,2],[3,4]]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := memory.ExtractJSONArray(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Run("shorter than maxLen", func(t *testing.T) {
		assert.Equal(t, "hello", memory.Truncate("hello", 10))
	})

	t.Run("equal to maxLen", func(t *testing.T) {
		assert.Equal(t, "hello", memory.Truncate("hello", 5))
	})

	t.Run("exceeds maxLen", func(t *testing.T) {
		assert.Equal(t, "hel...", memory.Truncate("hello world", 3))
	})
}

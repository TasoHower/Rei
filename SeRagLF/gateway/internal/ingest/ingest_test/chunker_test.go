package ingest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/ingest"
)

func TestChunkText_Basic(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		chunkSize int
		overlap   int
		wantCount int
	}{
		{
			name:      "short text fits in one chunk",
			text:      "hello world",
			chunkSize: 100,
			overlap:   10,
			wantCount: 1,
		},
		{
			name:      "long text splits into multiple chunks",
			text:      strings.Repeat("a", 1000),
			chunkSize: 300,
			overlap:   50,
			wantCount: 4,
		},
		{
			name:      "exact divisible length",
			text:      strings.Repeat("x", 200),
			chunkSize: 100,
			overlap:   0,
			wantCount: 2,
		},
		{
			name:      "unicode CJK characters",
			text:      strings.Repeat("\u4f60\u597d\u4e16\u754c", 50),
			chunkSize: 20,
			overlap:   5,
			wantCount: 13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ingest.ChunkText(tt.text, tt.chunkSize, tt.overlap, nil)
			assert.Len(t, chunks, tt.wantCount)

			for i, c := range chunks {
				assert.Equal(t, i, c.Index, "chunk index mismatch")
				assert.NotEmpty(t, c.Content, "chunk content should not be empty")
			}
		})
	}
}

func TestChunkText_EdgeCases(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		chunks := ingest.ChunkText("", 100, 10, nil)
		assert.Nil(t, chunks)
	})

	t.Run("chunkSize zero returns nil", func(t *testing.T) {
		chunks := ingest.ChunkText("hello", 0, 10, nil)
		assert.Nil(t, chunks)
	})

	t.Run("negative chunkSize returns nil", func(t *testing.T) {
		chunks := ingest.ChunkText("hello", -1, 10, nil)
		assert.Nil(t, chunks)
	})

	t.Run("overlap >= chunkSize gets corrected", func(t *testing.T) {
		chunks := ingest.ChunkText(strings.Repeat("a", 100), 10, 10, nil)
		require.NotNil(t, chunks)
		assert.True(t, len(chunks) > 1, "should produce multiple chunks with corrected overlap")
	})
}

func TestChunkText_Overlap(t *testing.T) {
	text := "abcdefghijklmnopqrstuvwxyz"
	chunks := ingest.ChunkText(text, 10, 3, nil)
	require.True(t, len(chunks) >= 2, "need at least 2 chunks to verify overlap")

	for i := 0; i < len(chunks)-1; i++ {
		curr := chunks[i].Content
		next := chunks[i+1].Content
		overlapSuffix := curr[len(curr)-3:]
		overlapPrefix := next[:3]
		assert.Equal(t, overlapSuffix, overlapPrefix,
			"adjacent chunks should have overlapping content at boundary")
	}
}

func TestChunkText_Metadata(t *testing.T) {
	meta := map[string]string{"source": "test.txt", "format": ".txt"}
	chunks := ingest.ChunkText("hello world, this is a test", 10, 2, meta)
	require.NotEmpty(t, chunks)

	for _, c := range chunks {
		assert.Equal(t, meta, c.Metadata, "metadata should be passed through to each chunk")
	}
}

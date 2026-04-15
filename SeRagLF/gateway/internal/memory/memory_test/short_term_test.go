package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/config"
	"seraglf/internal/memory"
)

func newTestShortTerm(t *testing.T) (*memory.ShortTermStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	cfg := config.MemoryConfig{
		ShortTermTTLHours:      24,
		MaxConversationMessages: 100,
	}
	store := memory.NewShortTermStore(rdb, cfg, newTestLogger())
	return store, mr
}

func TestShortTerm_UpdateAndGet(t *testing.T) {
	store, _ := newTestShortTerm(t)
	ctx := context.Background()
	convID := "conv-1"

	dc := &memory.DistilledContext{
		ConversationID: convID,
		Topics:         []string{"golang", "testing"},
		Narrative:      "discussing go testing",
		Version:        1,
	}

	require.NoError(t, store.Update(ctx, convID, dc))

	got, err := store.Get(ctx, convID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, convID, got.ConversationID)
	assert.Equal(t, []string{"golang", "testing"}, got.Topics)
	assert.Equal(t, "discussing go testing", got.Narrative)
	assert.Equal(t, 1, got.Version)
}

func TestShortTerm_GetNonExistent(t *testing.T) {
	store, _ := newTestShortTerm(t)
	ctx := context.Background()

	got, err := store.Get(ctx, "nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

func TestShortTerm_BufferMessages(t *testing.T) {
	store, _ := newTestShortTerm(t)
	ctx := context.Background()
	convID := "conv-buf"

	msgs := []memory.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "how are you"},
	}
	require.NoError(t, store.BufferMessages(ctx, convID, msgs))

	listed, err := store.ListMessages(ctx, convID)
	require.NoError(t, err)
	require.Len(t, listed, 3)
	assert.Equal(t, "user", listed[0].Role)
	assert.Equal(t, "hello", listed[0].Content)
	assert.Equal(t, "assistant", listed[1].Role)
	assert.Equal(t, "user", listed[2].Role)
}

func TestShortTerm_BufferMessages_MaxLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	cfg := config.MemoryConfig{
		ShortTermTTLHours:      24,
		MaxConversationMessages: 3,
	}
	store := memory.NewShortTermStore(rdb, cfg, newTestLogger())
	ctx := context.Background()
	convID := "conv-limit"

	msgs1 := []memory.Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
	}
	require.NoError(t, store.BufferMessages(ctx, convID, msgs1))

	msgs2 := []memory.Message{
		{Role: "assistant", Content: "msg4"},
		{Role: "user", Content: "msg5"},
	}
	require.NoError(t, store.BufferMessages(ctx, convID, msgs2))

	listed, err := store.ListMessages(ctx, convID)
	require.NoError(t, err)
	assert.Len(t, listed, 3, "should be trimmed to max 3 messages")
	assert.Equal(t, "msg3", listed[0].Content)
	assert.Equal(t, "msg4", listed[1].Content)
	assert.Equal(t, "msg5", listed[2].Content)
}

func TestShortTerm_Clear(t *testing.T) {
	store, _ := newTestShortTerm(t)
	ctx := context.Background()
	convID := "conv-clear"

	dc := &memory.DistilledContext{ConversationID: convID, Version: 1}
	require.NoError(t, store.Update(ctx, convID, dc))
	msgs := []memory.Message{{Role: "user", Content: "hello"}}
	require.NoError(t, store.BufferMessages(ctx, convID, msgs))

	require.NoError(t, store.Clear(ctx, convID))

	got, err := store.Get(ctx, convID)
	assert.NoError(t, err)
	assert.Nil(t, got)

	listed, err := store.ListMessages(ctx, convID)
	assert.NoError(t, err)
	assert.Empty(t, listed)
}

func TestShortTerm_TTL(t *testing.T) {
	store, mr := newTestShortTerm(t)
	ctx := context.Background()
	convID := "conv-ttl"

	dc := &memory.DistilledContext{ConversationID: convID, Version: 1}
	require.NoError(t, store.Update(ctx, convID, dc))

	ttl := mr.TTL("conv:" + convID + ":context")
	assert.True(t, ttl > 0, "key should have a positive TTL set")
	assert.True(t, ttl <= 24*time.Hour, "TTL should be at most 24 hours")
}

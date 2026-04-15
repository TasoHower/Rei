package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"seraglf/internal/config"
)

type ShortTermStore struct {
	rdb *redis.Client
	cfg config.MemoryConfig
	log *slog.Logger
}

func NewShortTermStore(rdb *redis.Client, cfg config.MemoryConfig, log *slog.Logger) *ShortTermStore {
	return &ShortTermStore{rdb: rdb, cfg: cfg, log: log}
}

func contextKey(conversationID string) string {
	return fmt.Sprintf("conv:%s:context", conversationID)
}

func messagesKey(conversationID string) string {
	return fmt.Sprintf("conv:%s:messages", conversationID)
}

// Update stores the distilled context snapshot for a conversation.
func (s *ShortTermStore) Update(ctx context.Context, conversationID string, dc *DistilledContext) error {
	data, err := json.Marshal(dc)
	if err != nil {
		return fmt.Errorf("marshal context: %w", err)
	}
	ttl := time.Duration(s.cfg.ShortTermTTLHours) * time.Hour
	if err := s.rdb.Set(ctx, contextKey(conversationID), data, ttl).Err(); err != nil {
		return fmt.Errorf("redis set context: %w", err)
	}
	s.log.Debug("context updated", "conversation_id", conversationID, "version", dc.Version, "ttl_hours", s.cfg.ShortTermTTLHours)
	return nil
}

// Get retrieves the current distilled context for a conversation.
// Returns nil (no error) if the conversation has no context yet.
func (s *ShortTermStore) Get(ctx context.Context, conversationID string) (*DistilledContext, error) {
	val, err := s.rdb.Get(ctx, contextKey(conversationID)).Result()
	if err == redis.Nil {
		s.log.Debug("no existing context", "conversation_id", conversationID)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get context: %w", err)
	}
	var dc DistilledContext
	if err := json.Unmarshal([]byte(val), &dc); err != nil {
		return nil, fmt.Errorf("unmarshal context: %w", err)
	}
	s.log.Debug("context loaded", "conversation_id", conversationID, "version", dc.Version)
	return &dc, nil
}

// BufferMessages appends raw messages to the internal message buffer.
// This buffer is used by memory_save to extract facts and generate summaries.
func (s *ShortTermStore) BufferMessages(ctx context.Context, conversationID string, msgs []Message) error {
	pipe := s.rdb.Pipeline()
	key := messagesKey(conversationID)

	for i := range msgs {
		if msgs[i].Timestamp == 0 {
			msgs[i].Timestamp = time.Now().Unix()
		}
		data, err := json.Marshal(msgs[i])
		if err != nil {
			return fmt.Errorf("marshal message: %w", err)
		}
		pipe.RPush(ctx, key, data)
	}

	maxMsgs := int64(s.cfg.MaxConversationMessages)
	if maxMsgs > 0 {
		pipe.LTrim(ctx, key, -maxMsgs, -1)
	}

	ttl := time.Duration(s.cfg.ShortTermTTLHours) * time.Hour
	pipe.Expire(ctx, key, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline buffer: %w", err)
	}
	s.log.Debug("messages buffered", "conversation_id", conversationID, "count", len(msgs))
	return nil
}

// ListMessages reads the internal message buffer (used by memory_save).
func (s *ShortTermStore) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	vals, err := s.rdb.LRange(ctx, messagesKey(conversationID), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("redis lrange messages: %w", err)
	}
	msgs := make([]Message, 0, len(vals))
	for _, v := range vals {
		var m Message
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	s.log.Debug("messages listed", "conversation_id", conversationID, "count", len(msgs))
	return msgs, nil
}

// Clear removes both the distilled context and the message buffer.
func (s *ShortTermStore) Clear(ctx context.Context, conversationID string) error {
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, contextKey(conversationID))
	pipe.Del(ctx, messagesKey(conversationID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	s.log.Info("conversation cleared", "conversation_id", conversationID)
	return nil
}

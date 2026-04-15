package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	pb "github.com/qdrant/go-client/qdrant"

	"seraglf/internal/config"
	"seraglf/internal/embedding"
	"seraglf/internal/qdrant"
)

type LongTermStore struct {
	db       *sql.DB
	vectorDB qdrant.VectorStore
	embedder embedding.Embedder
	cfg      config.MemoryConfig
	log      *slog.Logger
}

func NewLongTermStore(db *sql.DB, vectorDB qdrant.VectorStore, embedder embedding.Embedder, cfg config.MemoryConfig, log *slog.Logger) *LongTermStore {
	return &LongTermStore{db: db, vectorDB: vectorDB, embedder: embedder, cfg: cfg, log: log}
}

func (l *LongTermStore) EnsureCollections(ctx context.Context, dim uint64) error {
	names, err := l.vectorDB.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("list collections: %w", err)
	}
	existing := make(map[string]bool, len(names))
	for _, n := range names {
		existing[n] = true
	}

	for _, col := range []string{l.cfg.SummaryCollection, l.cfg.FactsCollection} {
		if !existing[col] {
			if err := l.vectorDB.CreateCollection(ctx, col, dim, "cosine"); err != nil {
				return fmt.Errorf("create collection %s: %w", col, err)
			}
			l.log.Info("collection created", "name", col, "dim", dim)
		}
	}
	return nil
}

// factPointID produces a deterministic UUID for a (user, category, key) triple
// so Qdrant upsert naturally overwrites stale vectors.
func FactPointID(userID, category, factKey string) string {
	ns := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	return uuid.NewSHA1(ns, []byte(userID+"\x00"+category+"\x00"+factKey)).String()
}

func FactEmbedText(category, factKey, factValue string) string {
	return fmt.Sprintf("%s: %s — %s", category, factKey, factValue)
}

func (l *LongTermStore) UpsertFacts(ctx context.Context, userID, conversationID string, facts []Fact) (int64, error) {
	if len(facts) == 0 {
		return 0, nil
	}

	const stmt = `INSERT INTO user_facts (user_id, category, fact_key, fact_value, confidence, source_conversation_id)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE fact_value = VALUES(fact_value), confidence = VALUES(confidence),
source_conversation_id = VALUES(source_conversation_id)`

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var affected int64
	for _, f := range facts {
		res, err := tx.ExecContext(ctx, stmt, userID, f.Category, f.FactKey, f.FactValue, f.Confidence, conversationID)
		if err != nil {
			return 0, fmt.Errorf("upsert fact %q: %w", f.FactKey, err)
		}
		n, _ := res.RowsAffected()
		affected += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	// Dual-write: embed facts and upsert to Qdrant for semantic retrieval.
	texts := make([]string, len(facts))
	for i, f := range facts {
		texts[i] = FactEmbedText(f.Category, f.FactKey, f.FactValue)
	}
	vectors, err := l.embedder.Embed(ctx, texts)
	if err != nil {
		return affected, fmt.Errorf("embed facts (mysql ok, qdrant skipped): %w", err)
	}

	now := time.Now().Unix()
	points := make([]*pb.PointStruct, len(facts))
	for i, f := range facts {
		vec32 := make([]float32, len(vectors[i]))
		for j, v := range vectors[i] {
			vec32[j] = float32(v)
		}
		points[i] = &pb.PointStruct{
			Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: FactPointID(userID, f.Category, f.FactKey)}},
			Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: vec32}}},
			Payload: map[string]*pb.Value{
				"user_id":    {Kind: &pb.Value_StringValue{StringValue: userID}},
				"category":   {Kind: &pb.Value_StringValue{StringValue: f.Category}},
				"fact_key":   {Kind: &pb.Value_StringValue{StringValue: f.FactKey}},
				"fact_value": {Kind: &pb.Value_StringValue{StringValue: f.FactValue}},
				"content":    {Kind: &pb.Value_StringValue{StringValue: texts[i]}},
				"created_at": {Kind: &pb.Value_IntegerValue{IntegerValue: now}},
			},
		}
	}

	if err := l.vectorDB.Upsert(ctx, l.cfg.FactsCollection, points); err != nil {
		return affected, fmt.Errorf("qdrant upsert facts (mysql ok): %w", err)
	}

	l.log.Info("facts upserted", "user_id", userID, "count", len(facts), "mysql_affected", affected)
	return affected, nil
}

func (l *LongTermStore) GetFacts(ctx context.Context, userID string) ([]Fact, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, user_id, category, fact_key, fact_value, confidence, source_conversation_id, created_at, updated_at
		 FROM user_facts WHERE user_id = ? ORDER BY category, fact_key`, userID)
	if err != nil {
		return nil, fmt.Errorf("query facts: %w", err)
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		var f Fact
		var srcConv sql.NullString
		if err := rows.Scan(&f.ID, &f.UserID, &f.Category, &f.FactKey, &f.FactValue,
			&f.Confidence, &srcConv, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		f.SourceConversationID = srcConv.String
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

func (l *LongTermStore) DeleteFact(ctx context.Context, factID int64) error {
	var userID, category, factKey string
	err := l.db.QueryRowContext(ctx,
		`SELECT user_id, category, fact_key FROM user_facts WHERE id = ?`, factID,
	).Scan(&userID, &category, &factKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("fact id %d not found", factID)
		}
		return fmt.Errorf("lookup fact: %w", err)
	}

	if _, err := l.db.ExecContext(ctx, `DELETE FROM user_facts WHERE id = ?`, factID); err != nil {
		return fmt.Errorf("delete fact from mysql: %w", err)
	}

	pointID := FactPointID(userID, category, factKey)
	if err := l.vectorDB.DeletePoints(ctx, l.cfg.FactsCollection, []string{pointID}); err != nil {
		return fmt.Errorf("delete fact from qdrant (mysql ok): %w", err)
	}

	l.log.Info("fact deleted", "fact_id", factID, "user_id", userID, "key", factKey)
	return nil
}

func (l *LongTermStore) SaveSummary(ctx context.Context, userID, conversationID, summary string) error {
	vec, err := l.embedder.EmbedQuery(ctx, summary)
	if err != nil {
		return fmt.Errorf("embed summary: %w", err)
	}

	vec32 := make([]float32, len(vec))
	for i, v := range vec {
		vec32[i] = float32(v)
	}

	now := time.Now().Unix()
	id := uuid.New().String()

	point := &pb.PointStruct{
		Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
		Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: vec32}}},
		Payload: map[string]*pb.Value{
			"user_id":         {Kind: &pb.Value_StringValue{StringValue: userID}},
			"conversation_id": {Kind: &pb.Value_StringValue{StringValue: conversationID}},
			"summary":         {Kind: &pb.Value_StringValue{StringValue: summary}},
			"content":         {Kind: &pb.Value_StringValue{StringValue: summary}},
			"created_at":      {Kind: &pb.Value_IntegerValue{IntegerValue: now}},
		},
	}

	if err := l.vectorDB.Upsert(ctx, l.cfg.SummaryCollection, []*pb.PointStruct{point}); err != nil {
		return err
	}
	l.log.Info("summary saved", "user_id", userID, "conversation_id", conversationID, "length", len(summary))
	return nil
}

func (l *LongTermStore) RecallFacts(ctx context.Context, userID, query string, topK int) ([]Fact, error) {
	vec, err := l.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query for facts: %w", err)
	}

	filter := userFilter(userID)
	results, err := l.vectorDB.SearchWithFilter(ctx, l.cfg.FactsCollection, vec, topK, filter)
	if err != nil {
		return nil, fmt.Errorf("recall facts: %w", err)
	}

	facts := make([]Fact, 0, len(results))
	for _, r := range results {
		facts = append(facts, Fact{
			UserID:    userID,
			Category:  r.Payload["category"],
			FactKey:   r.Payload["fact_key"],
			FactValue: r.Payload["fact_value"],
			Score:     float64(r.Score),
		})
	}
	l.log.Debug("facts recalled", "user_id", userID, "results", len(facts))
	return facts, nil
}

func (l *LongTermStore) RecallSummaries(ctx context.Context, userID, query string, topK int) ([]ConversationSummary, error) {
	vec, err := l.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	filter := userFilter(userID)
	results, err := l.vectorDB.SearchWithFilter(ctx, l.cfg.SummaryCollection, vec, topK, filter)
	if err != nil {
		return nil, fmt.Errorf("recall summaries: %w", err)
	}

	summaries := make([]ConversationSummary, 0, len(results))
	for _, r := range results {
		summaries = append(summaries, ConversationSummary{
			UserID:  userID,
			Summary: r.Content,
			Score:   float64(r.Score),
		})
	}
	l.log.Debug("summaries recalled", "user_id", userID, "results", len(summaries))
	return summaries, nil
}

func userFilter(userID string) *pb.Filter {
	return &pb.Filter{
		Must: []*pb.Condition{
			{
				ConditionOneOf: &pb.Condition_Field{
					Field: &pb.FieldCondition{
						Key: "user_id",
						Match: &pb.Match{
							MatchValue: &pb.Match_Keyword{Keyword: userID},
						},
					},
				},
			},
		},
	}
}

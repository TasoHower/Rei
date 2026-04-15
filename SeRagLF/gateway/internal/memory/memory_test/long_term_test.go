package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	pb "github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/config"
	"seraglf/internal/memory"
	"seraglf/internal/qdrant"
	"seraglf/internal/testutil"
)

func newTestLongTerm(t *testing.T) (*memory.LongTermStore, sqlmock.Sqlmock, *testutil.MockEmbedder, *testutil.MockVectorStore) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	embedder := &testutil.MockEmbedder{}
	vectorDB := &testutil.MockVectorStore{}
	cfg := config.MemoryConfig{
		SummaryCollection: "_memory",
		FactsCollection:   "_user_facts",
	}

	store := memory.NewLongTermStore(db, vectorDB, embedder, cfg, newTestLogger())
	return store, mock, embedder, vectorDB
}

func TestEnsureCollections_AlreadyExist(t *testing.T) {
	store, _, _, vectorDB := newTestLongTerm(t)
	vectorDB.ListCollectionsFn = func(_ context.Context) ([]string, error) {
		return []string{"_memory", "_user_facts"}, nil
	}
	createCalled := false
	vectorDB.CreateCollectionFn = func(_ context.Context, _ string, _ uint64, _ string) error {
		createCalled = true
		return nil
	}

	err := store.EnsureCollections(context.Background(), 1536)
	assert.NoError(t, err)
	assert.False(t, createCalled, "should not create collections that already exist")
}

func TestEnsureCollections_Create(t *testing.T) {
	store, _, _, vectorDB := newTestLongTerm(t)
	vectorDB.ListCollectionsFn = func(_ context.Context) ([]string, error) {
		return []string{}, nil
	}
	created := make([]string, 0)
	vectorDB.CreateCollectionFn = func(_ context.Context, name string, _ uint64, _ string) error {
		created = append(created, name)
		return nil
	}

	err := store.EnsureCollections(context.Background(), 1536)
	assert.NoError(t, err)
	assert.Contains(t, created, "_memory")
	assert.Contains(t, created, "_user_facts")
}

func TestUpsertFacts_Success(t *testing.T) {
	store, mock, _, _ := newTestLongTerm(t)
	ctx := context.Background()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO user_facts").
		WithArgs("user1", "preference", "language", "Go", 0.9, "conv-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	facts := []memory.Fact{
		{Category: "preference", FactKey: "language", FactValue: "Go", Confidence: 0.9},
	}

	affected, err := store.UpsertFacts(ctx, "user1", "conv-1", facts)
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpsertFacts_Empty(t *testing.T) {
	store, _, _, _ := newTestLongTerm(t)

	affected, err := store.UpsertFacts(context.Background(), "user1", "conv-1", nil)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), affected)
}

func TestUpsertFacts_MySQLError(t *testing.T) {
	store, mock, _, _ := newTestLongTerm(t)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO user_facts").
		WithArgs("user1", "preference", "lang", "Go", 0.9, "conv-1").
		WillReturnError(errors.New("mysql error"))
	mock.ExpectRollback()

	facts := []memory.Fact{
		{Category: "preference", FactKey: "lang", FactValue: "Go", Confidence: 0.9},
	}

	_, err := store.UpsertFacts(context.Background(), "user1", "conv-1", facts)
	assert.Error(t, err)
}

func TestGetFacts(t *testing.T) {
	store, mock, _, _ := newTestLongTerm(t)

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "user_id", "category", "fact_key", "fact_value",
		"confidence", "source_conversation_id", "created_at", "updated_at",
	}).
		AddRow(1, "user1", "preference", "language", "Go", 0.9, "conv-1", now, now).
		AddRow(2, "user1", "background", "role", "engineer", 0.85, "conv-1", now, now)

	mock.ExpectQuery("SELECT .+ FROM user_facts WHERE user_id").
		WithArgs("user1").
		WillReturnRows(rows)

	facts, err := store.GetFacts(context.Background(), "user1")
	require.NoError(t, err)
	require.Len(t, facts, 2)
	assert.Equal(t, "preference", facts[0].Category)
	assert.Equal(t, "language", facts[0].FactKey)
	assert.Equal(t, "Go", facts[0].FactValue)
	assert.Equal(t, "background", facts[1].Category)
}

func TestDeleteFact_Success(t *testing.T) {
	store, mock, _, vectorDB := newTestLongTerm(t)
	deletedPoints := make([]string, 0)
	vectorDB.DeletePointsFn = func(_ context.Context, _ string, ids []string) error {
		deletedPoints = append(deletedPoints, ids...)
		return nil
	}

	mock.ExpectQuery("SELECT user_id, category, fact_key FROM user_facts WHERE id").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "category", "fact_key"}).
			AddRow("user1", "preference", "language"))
	mock.ExpectExec("DELETE FROM user_facts WHERE id").
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.DeleteFact(context.Background(), 42)
	assert.NoError(t, err)
	assert.Len(t, deletedPoints, 1)
}

func TestDeleteFact_NotFound(t *testing.T) {
	store, mock, _, _ := newTestLongTerm(t)

	mock.ExpectQuery("SELECT user_id, category, fact_key FROM user_facts WHERE id").
		WithArgs(int64(999)).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "category", "fact_key"}))

	err := store.DeleteFact(context.Background(), 999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRecallFacts(t *testing.T) {
	store, _, _, vectorDB := newTestLongTerm(t)
	vectorDB.SearchWithFilterFn = func(_ context.Context, _ string, _ []float64, _ int, _ *pb.Filter) ([]qdrant.SearchResult, error) {
		return []qdrant.SearchResult{
			{
				Content: "preference: language - Go",
				Score:   0.95,
				Payload: map[string]string{
					"category":   "preference",
					"fact_key":   "language",
					"fact_value": "Go",
				},
			},
		}, nil
	}

	facts, err := store.RecallFacts(context.Background(), "user1", "what language", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Equal(t, "preference", facts[0].Category)
	assert.Equal(t, "language", facts[0].FactKey)
	assert.Equal(t, "Go", facts[0].FactValue)
}

func TestRecallSummaries(t *testing.T) {
	store, _, _, vectorDB := newTestLongTerm(t)
	vectorDB.SearchWithFilterFn = func(_ context.Context, _ string, _ []float64, _ int, _ *pb.Filter) ([]qdrant.SearchResult, error) {
		return []qdrant.SearchResult{
			{Content: "discussed Go testing", Score: 0.88},
		}, nil
	}

	summaries, err := store.RecallSummaries(context.Background(), "user1", "testing", 5)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "discussed Go testing", summaries[0].Summary)
}

func TestFactPointID(t *testing.T) {
	id1 := memory.FactPointID("user1", "preference", "language")
	id2 := memory.FactPointID("user1", "preference", "language")
	id3 := memory.FactPointID("user2", "preference", "language")

	assert.Equal(t, id1, id2, "same input should produce same ID")
	assert.NotEqual(t, id1, id3, "different input should produce different ID")
	assert.NotEmpty(t, id1)
}

func TestFactEmbedText(t *testing.T) {
	text := memory.FactEmbedText("preference", "language", "Go")
	assert.Equal(t, "preference: language \u2014 Go", text)
}

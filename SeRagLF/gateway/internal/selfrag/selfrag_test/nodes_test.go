package selfrag_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seraglf/internal/qdrant"
	"seraglf/internal/selfrag"
	"seraglf/internal/testutil"
)

func newTestNodes(graderResp string, graderErr error, genResp string, genErr error) *selfrag.Nodes {
	grader := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			if graderErr != nil {
				return nil, graderErr
			}
			return &schema.Message{Content: graderResp}, nil
		},
	}
	generator := &testutil.MockChatModel{
		GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) {
			if genErr != nil {
				return nil, genErr
			}
			return &schema.Message{Content: genResp}, nil
		},
	}
	embedder := &testutil.MockEmbedder{}
	vectorDB := &testutil.MockVectorStore{
		SearchFn: func(_ context.Context, _ string, _ []float64, _ int) ([]qdrant.SearchResult, error) {
			return []qdrant.SearchResult{
				{Content: "doc1 content", Source: "source1", Score: 0.9},
				{Content: "doc2 content", Source: "source2", Score: 0.8},
			}, nil
		},
	}
	log := slog.Default()
	return selfrag.NewNodes(grader, generator, embedder, vectorDB, log)
}

func baseState() *selfrag.State {
	return &selfrag.State{
		Question:   "What is Go?",
		Collection: "test",
		MaxRetries: 3,
		TopK:       5,
	}
}

func TestRetrieve_Success(t *testing.T) {
	nodes := newTestNodes("", nil, "", nil)
	s := baseState()

	result, err := nodes.Retrieve(context.Background(), s)
	require.NoError(t, err)
	assert.Len(t, result.Documents, 2)
	assert.Equal(t, "doc1 content", result.Documents[0].Content)
	assert.Equal(t, "source1", result.Documents[0].Source)
}

func TestRetrieve_EmbedError(t *testing.T) {
	embedder := &testutil.MockEmbedder{
		EmbedQueryFn: func(_ context.Context, _ string) ([]float64, error) {
			return nil, errors.New("embed failure")
		},
	}
	nodes := selfrag.NewNodes(
		&testutil.MockChatModel{GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) { return nil, nil }},
		&testutil.MockChatModel{GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) { return nil, nil }},
		embedder,
		&testutil.MockVectorStore{},
		slog.Default(),
	)
	s := baseState()

	result, err := nodes.Retrieve(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, result.Documents)
	assert.NotEmpty(t, result.TraceSteps)
}

func TestRetrieve_SearchError(t *testing.T) {
	vectorDB := &testutil.MockVectorStore{
		SearchFn: func(_ context.Context, _ string, _ []float64, _ int) ([]qdrant.SearchResult, error) {
			return nil, errors.New("search failure")
		},
	}
	nodes := selfrag.NewNodes(
		&testutil.MockChatModel{GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) { return nil, nil }},
		&testutil.MockChatModel{GenerateFn: func(_ context.Context, _ []*schema.Message) (*schema.Message, error) { return nil, nil }},
		&testutil.MockEmbedder{},
		vectorDB,
		slog.Default(),
	)
	s := baseState()

	result, err := nodes.Retrieve(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, result.Documents)
}

func TestGradeDocuments_AllRelevant(t *testing.T) {
	nodes := newTestNodes(`{"relevant": true}`, nil, "", nil)
	s := baseState()
	s.Documents = []selfrag.RetrievedDoc{
		{Content: "doc1", Source: "s1"},
		{Content: "doc2", Source: "s2"},
	}

	result, err := nodes.GradeDocuments(context.Background(), s)
	require.NoError(t, err)
	assert.Len(t, result.Documents, 2)
}

func TestGradeDocuments_NoneRelevant(t *testing.T) {
	nodes := newTestNodes(`{"relevant": false}`, nil, "", nil)
	s := baseState()
	s.Documents = []selfrag.RetrievedDoc{
		{Content: "doc1", Source: "s1"},
		{Content: "doc2", Source: "s2"},
	}

	result, err := nodes.GradeDocuments(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, result.Documents)
}

func TestGradeDocuments_GraderError(t *testing.T) {
	nodes := newTestNodes("", errors.New("grader error"), "", nil)
	s := baseState()
	s.Documents = []selfrag.RetrievedDoc{
		{Content: "doc1", Source: "s1"},
	}

	result, err := nodes.GradeDocuments(context.Background(), s)
	require.NoError(t, err)
	assert.Len(t, result.Documents, 1, "on error, document should be kept with 0.5 score")
	assert.Equal(t, 0.5, result.Documents[0].RelevanceScore)
}

func TestGenerate_Success(t *testing.T) {
	nodes := newTestNodes("", nil, "Go is a programming language.", nil)
	s := baseState()
	s.Documents = []selfrag.RetrievedDoc{{Content: "Go info"}}

	result, err := nodes.Generate(context.Background(), s)
	require.NoError(t, err)
	assert.Equal(t, "Go is a programming language.", result.Generation)
}

func TestGenerate_Error(t *testing.T) {
	nodes := newTestNodes("", nil, "", errors.New("gen error"))
	s := baseState()
	s.Documents = []selfrag.RetrievedDoc{{Content: "Go info"}}

	result, err := nodes.Generate(context.Background(), s)
	require.NoError(t, err)
	assert.Empty(t, result.Generation)
	assert.NotEmpty(t, result.TraceSteps)
}

func TestCheckHallucination_Grounded(t *testing.T) {
	nodes := newTestNodes(`{"grounded": true}`, nil, "", nil)
	s := baseState()
	s.Generation = "answer"
	s.Documents = []selfrag.RetrievedDoc{{Content: "doc"}}

	result, err := nodes.CheckHallucination(context.Background(), s)
	require.NoError(t, err)
	assert.True(t, result.Grounded)
}

func TestCheckHallucination_NotGrounded(t *testing.T) {
	nodes := newTestNodes(`{"grounded": false}`, nil, "", nil)
	s := baseState()
	s.Generation = "answer"
	s.Documents = []selfrag.RetrievedDoc{{Content: "doc"}}

	result, err := nodes.CheckHallucination(context.Background(), s)
	require.NoError(t, err)
	assert.False(t, result.Grounded)
}

func TestCheckHallucination_Error(t *testing.T) {
	nodes := newTestNodes("", errors.New("grader error"), "", nil)
	s := baseState()
	s.Generation = "answer"
	s.Documents = []selfrag.RetrievedDoc{{Content: "doc"}}

	result, err := nodes.CheckHallucination(context.Background(), s)
	require.NoError(t, err)
	assert.True(t, result.Grounded, "on error, should default to grounded=true")
}

func TestCheckHallucination_InvalidJSON_ContainsFalse(t *testing.T) {
	nodes := newTestNodes("no it is not grounded, false", nil, "", nil)
	s := baseState()
	s.Generation = "answer"
	s.Documents = []selfrag.RetrievedDoc{{Content: "doc"}}

	result, err := nodes.CheckHallucination(context.Background(), s)
	require.NoError(t, err)
	assert.False(t, result.Grounded, "text containing 'false' should be treated as not grounded")
}

func TestGradeAnswer_Useful(t *testing.T) {
	nodes := newTestNodes(`{"useful": true}`, nil, "", nil)
	s := baseState()
	s.Generation = "answer"

	result, err := nodes.GradeAnswer(context.Background(), s)
	require.NoError(t, err)
	assert.True(t, result.AnswerUseful)
}

func TestGradeAnswer_NotUseful(t *testing.T) {
	nodes := newTestNodes(`{"useful": false}`, nil, "", nil)
	s := baseState()
	s.Generation = "answer"

	result, err := nodes.GradeAnswer(context.Background(), s)
	require.NoError(t, err)
	assert.False(t, result.AnswerUseful)
}

func TestGradeAnswer_InvalidJSON_ContainsFalse(t *testing.T) {
	nodes := newTestNodes("The answer is not useful, false", nil, "", nil)
	s := baseState()
	s.Generation = "answer"

	result, err := nodes.GradeAnswer(context.Background(), s)
	require.NoError(t, err)
	assert.False(t, result.AnswerUseful, "text containing 'false' should be treated as not useful")
}

func TestGradeAnswer_InvalidJSON_NoFalse(t *testing.T) {
	nodes := newTestNodes("yes the answer is great", nil, "", nil)
	s := baseState()
	s.Generation = "answer"

	result, err := nodes.GradeAnswer(context.Background(), s)
	require.NoError(t, err)
	assert.True(t, result.AnswerUseful, "text without 'false' should be treated as useful")
}

func TestGradeAnswer_Error(t *testing.T) {
	nodes := newTestNodes("", errors.New("grader down"), "", nil)
	s := baseState()
	s.Generation = "answer"

	result, err := nodes.GradeAnswer(context.Background(), s)
	require.NoError(t, err)
	assert.True(t, result.AnswerUseful, "on error, should default to useful=true")
}

func TestTransformQuery(t *testing.T) {
	nodes := newTestNodes("What are the features of Go?", nil, "", nil)
	s := baseState()
	oldRetries := s.Retries

	result, err := nodes.TransformQuery(context.Background(), s)
	require.NoError(t, err)
	assert.Equal(t, "What are the features of Go?", result.Question)
	assert.Equal(t, oldRetries+1, result.Retries)
}

func TestTransformQuery_Error(t *testing.T) {
	nodes := newTestNodes("", errors.New("rewrite error"), "", nil)
	s := baseState()
	originalQ := s.Question

	result, err := nodes.TransformQuery(context.Background(), s)
	require.NoError(t, err)
	assert.Equal(t, originalQ, result.Question, "question should not change on error")
	assert.Equal(t, 1, result.Retries, "retries should still increment")
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"with surrounding text", `Sure! {"key": "val"} here`, `{"key": "val"}`},
		{"no JSON", "plain text", "plain text"},
		{"just JSON", `{"a": 1}`, `{"a": 1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selfrag.ExtractJSON(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

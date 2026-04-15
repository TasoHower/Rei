package testutil

import (
	"context"

	pb "github.com/qdrant/go-client/qdrant"

	"seraglf/internal/qdrant"
)

type MockVectorStore struct {
	SearchFn           func(ctx context.Context, collection string, vector []float64, topK int) ([]qdrant.SearchResult, error)
	SearchWithFilterFn func(ctx context.Context, collection string, vector []float64, topK int, filter *pb.Filter) ([]qdrant.SearchResult, error)
	UpsertFn           func(ctx context.Context, collection string, points []*pb.PointStruct) error
	DeletePointsFn     func(ctx context.Context, collection string, pointIDs []string) error
	CreateCollectionFn func(ctx context.Context, name string, dim uint64, distance string) error
	ListCollectionsFn  func(ctx context.Context) ([]string, error)
}

func (m *MockVectorStore) Search(ctx context.Context, collection string, vector []float64, topK int) ([]qdrant.SearchResult, error) {
	if m.SearchFn != nil {
		return m.SearchFn(ctx, collection, vector, topK)
	}
	return nil, nil
}

func (m *MockVectorStore) SearchWithFilter(ctx context.Context, collection string, vector []float64, topK int, filter *pb.Filter) ([]qdrant.SearchResult, error) {
	if m.SearchWithFilterFn != nil {
		return m.SearchWithFilterFn(ctx, collection, vector, topK, filter)
	}
	return nil, nil
}

func (m *MockVectorStore) Upsert(ctx context.Context, collection string, points []*pb.PointStruct) error {
	if m.UpsertFn != nil {
		return m.UpsertFn(ctx, collection, points)
	}
	return nil
}

func (m *MockVectorStore) DeletePoints(ctx context.Context, collection string, pointIDs []string) error {
	if m.DeletePointsFn != nil {
		return m.DeletePointsFn(ctx, collection, pointIDs)
	}
	return nil
}

func (m *MockVectorStore) CreateCollection(ctx context.Context, name string, dim uint64, distance string) error {
	if m.CreateCollectionFn != nil {
		return m.CreateCollectionFn(ctx, name, dim, distance)
	}
	return nil
}

func (m *MockVectorStore) ListCollections(ctx context.Context) ([]string, error) {
	if m.ListCollectionsFn != nil {
		return m.ListCollectionsFn(ctx)
	}
	return nil, nil
}

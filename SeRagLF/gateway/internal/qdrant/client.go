package qdrant

import (
	"context"
	"fmt"

	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type SearchResult struct {
	Content string
	Source  string
	Score   float32
	Payload map[string]string
}

type VectorStore interface {
	Search(ctx context.Context, collection string, vector []float64, topK int) ([]SearchResult, error)
	SearchWithFilter(ctx context.Context, collection string, vector []float64, topK int, filter *pb.Filter) ([]SearchResult, error)
	Upsert(ctx context.Context, collection string, points []*pb.PointStruct) error
	DeletePoints(ctx context.Context, collection string, pointIDs []string) error
	CreateCollection(ctx context.Context, name string, dim uint64, distance string) error
	ListCollections(ctx context.Context) ([]string, error)
}

type Client struct {
	conn        *grpc.ClientConn
	points      pb.PointsClient
	collections pb.CollectionsClient
}

func NewClient(addr, apiKey string) (*Client, error) {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial qdrant %s: %w", addr, err)
	}
	return &Client{
		conn:        conn,
		points:      pb.NewPointsClient(conn),
		collections: pb.NewCollectionsClient(conn),
	}, nil
}

func (c *Client) Search(ctx context.Context, collection string, vector []float64, topK int) ([]SearchResult, error) {
	vec32 := make([]float32, len(vector))
	for i, v := range vector {
		vec32[i] = float32(v)
	}

	limit := uint64(topK)
	withPayload := true
	resp, err := c.points.Search(ctx, &pb.SearchPoints{
		CollectionName: collection,
		Vector:         vec32,
		Limit:          limit,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: withPayload}},
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant search: %w", err)
	}

	return parseSearchResults(resp.Result), nil
}

func (c *Client) SearchWithFilter(ctx context.Context, collection string, vector []float64, topK int, filter *pb.Filter) ([]SearchResult, error) {
	vec32 := make([]float32, len(vector))
	for i, v := range vector {
		vec32[i] = float32(v)
	}

	limit := uint64(topK)
	withPayload := true
	req := &pb.SearchPoints{
		CollectionName: collection,
		Vector:         vec32,
		Limit:          limit,
		Filter:         filter,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: withPayload}},
	}

	resp, err := c.points.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("qdrant search with filter: %w", err)
	}

	return parseSearchResults(resp.Result), nil
}

func parseSearchResults(scored []*pb.ScoredPoint) []SearchResult {
	results := make([]SearchResult, 0, len(scored))
	for _, r := range scored {
		sr := SearchResult{Score: r.Score, Payload: make(map[string]string)}
		if p := r.Payload; p != nil {
			for k, v := range p {
				if s := v.GetStringValue(); s != "" {
					sr.Payload[k] = s
				}
			}
			sr.Content = sr.Payload["content"]
			sr.Source = sr.Payload["source"]
		}
		results = append(results, sr)
	}
	return results
}

func (c *Client) Upsert(ctx context.Context, collection string, points []*pb.PointStruct) error {
	wait := true
	_, err := c.points.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: collection,
		Wait:           &wait,
		Points:         points,
	})
	return err
}

func (c *Client) DeletePoints(ctx context.Context, collection string, pointIDs []string) error {
	ids := make([]*pb.PointId, len(pointIDs))
	for i, id := range pointIDs {
		ids[i] = &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: id}}
	}
	wait := true
	_, err := c.points.Delete(ctx, &pb.DeletePoints{
		CollectionName: collection,
		Wait:           &wait,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{Ids: ids},
			},
		},
	})
	return err
}

func (c *Client) CreateCollection(ctx context.Context, name string, dim uint64, distance string) error {
	dist := pb.Distance_Cosine
	switch distance {
	case "euclid":
		dist = pb.Distance_Euclid
	case "dot":
		dist = pb.Distance_Dot
	}
	_, err := c.collections.Create(ctx, &pb.CreateCollection{
		CollectionName: name,
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{Size: dim, Distance: dist},
			},
		},
	})
	return err
}

func (c *Client) ListCollections(ctx context.Context) ([]string, error) {
	resp, err := c.collections.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.Collections))
	for _, col := range resp.Collections {
		names = append(names, col.Name)
	}
	return names, nil
}

func (c *Client) DeleteCollection(ctx context.Context, name string) error {
	_, err := c.collections.Delete(ctx, &pb.DeleteCollection{CollectionName: name})
	return err
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

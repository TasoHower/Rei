package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	pb "github.com/qdrant/go-client/qdrant"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"seraglf/internal/ingest"
)

// -------------------- param structs --------------------

type SelfRAGQueryArgs struct {
	Question   string `json:"question" jsonschema:"End-user question to answer using retrieved knowledge. Prefer concrete, self-contained wording."`
	Collection string `json:"collection" jsonschema:"Existing Qdrant collection name with embedded chunks. Call list_collections first if the name is unknown."`
	MaxRetries int    `json:"max_retries" jsonschema:"Max Self-RAG reflection iterations when retrieval or generation quality is insufficient. Use 0 or omit for server default."`
	TopK       int    `json:"top_k" jsonschema:"Vector hits to retrieve per step. Use 0 or omit for server default."`
}

type IngestDocumentsArgs struct {
	SourcePath   string `json:"source_path" jsonschema:"Path to one file readable by the gateway process (.txt, .md, .html). In Docker, mount a volume or bake files into the image; host paths outside the container are not visible."`
	Collection   string `json:"collection" jsonschema:"Target collection; must exist unless your workflow creates it first via create_collection."`
	ChunkSize    int    `json:"chunk_size" jsonschema:"Chunk size in characters. Use 0 or omit for server default."`
	ChunkOverlap int    `json:"chunk_overlap" jsonschema:"Overlap between chunks in characters. Use 0 or omit for server default."`
}

type SearchDocumentsArgs struct {
	Query      string `json:"query" jsonschema:"Natural-language query or keywords for dense vector search; not expanded by an LLM."`
	Collection string `json:"collection" jsonschema:"Collection to search."`
	TopK       int    `json:"top_k" jsonschema:"Maximum hits to return. Use 0 or omit for server default."`
}

type CollectionCreateArgs struct {
	Name     string `json:"name" jsonschema:"New unique collection identifier (knowledge base name)."`
	Distance string `json:"distance" jsonschema:"Qdrant distance metric, e.g. cosine. Omit for server default matching the embedding model."`
}

type CollectionNameArgs struct {
	Name string `json:"name" jsonschema:"Exact collection name. Permanently removes all points; cannot be undone."`
}

// -------------------- registration --------------------

func registerTools(server *mcp.Server, deps *Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "self_rag_query",
		Title:       "Self-RAG grounded answer",
		Description: "End-to-end RAG answer with retrieval, relevance grading, answer generation, and hallucination-style checks. Use when the user needs a synthesized answer grounded in a collection—not just raw passages.\n\nPrefer search_documents when you only need ranked chunks without an LLM answer. Requires an existing populated collection (ingest_documents or prior data).\n\nReturns JSON with the pipeline result including answer text, retries used, and related metadata.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
		},
	}, selfRAGHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ingest_documents",
		Title:       "Ingest file into collection",
		Description: "Parse a single file from disk, split into chunks, embed, and upsert vectors into the named collection.\n\nUse after create_collection when loading new knowledge. Idempotent only at the point level (new UUIDs per chunk); repeated runs duplicate content unless you manage sources externally.\n\nReturns chunk and document counts on success.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
		},
	}, ingestHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_documents",
		Title:       "Semantic vector search",
		Description: "Embed the query and return the top similar chunks from a collection. Fast path with no LLM grading or final answer.\n\nUse for inspection, citation gathering, or when the client will compose the answer. For a full grounded reply, use self_rag_query instead.\n\nReturns JSON: results array and total count.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
			IdempotentHint:  true,
		},
	}, searchHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_collection",
		Title:       "Create vector collection",
		Description: "Create an empty Qdrant collection sized for the configured embedding dimension. Call before first ingest into a new knowledge base name.\n\nDoes not ingest data. Pair with ingest_documents afterward.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
		},
	}, createCollectionHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_collections",
		Title:       "List collections",
		Description: "Return all collection names visible to the gateway. Call before create_collection to avoid duplicates, or to pick a target for search or ingest.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(false),
			IdempotentHint:  true,
		},
	}, listCollectionsHandler(deps))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_collection",
		Title:       "Delete collection",
		Description: "Drop an entire collection and all vectors. Irreversible.\n\nUse only when the user explicitly requests removal or cleanup. Confirm the exact name via list_collections if unsure.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			OpenWorldHint:   ptrBool(false),
			DestructiveHint: ptrBool(true),
		},
	}, deleteCollectionHandler(deps))
}

// -------------------- handlers --------------------

func selfRAGHandler(deps *Deps) mcp.ToolHandlerFor[SelfRAGQueryArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SelfRAGQueryArgs) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		deps.Log.Info("tool called", "tool", "self_rag_query", "collection", args.Collection, "question_length", len(args.Question))

		maxRetries := args.MaxRetries
		if maxRetries <= 0 {
			maxRetries = deps.Cfg.Defaults.MaxRetries
		}
		topK := args.TopK
		if topK <= 0 {
			topK = deps.Cfg.Defaults.TopK
		}

		result, err := deps.Pipeline.Run(ctx, args.Question, args.Collection, maxRetries, topK)
		if err != nil {
			deps.Log.Error("self_rag_query failed", "error", err)
			return errResult(fmt.Sprintf("self_rag_query failed: %v", err)), nil, nil
		}
		deps.Log.Info("tool completed", "tool", "self_rag_query", "retries", result.Retries, "duration_ms", time.Since(start).Milliseconds())
		return jsonResult(result)
	}
}

func ingestHandler(deps *Deps) mcp.ToolHandlerFor[IngestDocumentsArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args IngestDocumentsArgs) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		deps.Log.Info("tool called", "tool", "ingest_documents", "source", args.SourcePath, "collection", args.Collection)
		chunkSize := args.ChunkSize
		if chunkSize <= 0 {
			chunkSize = deps.Cfg.Defaults.ChunkSize
		}
		chunkOverlap := args.ChunkOverlap
		if chunkOverlap <= 0 {
			chunkOverlap = deps.Cfg.Defaults.ChunkOverlap
		}

		docs, err := ingest.ParseFile(args.SourcePath)
		if err != nil {
			return errResult(fmt.Sprintf("parse failed: %v", err)), nil, nil
		}

		var allChunks []ingest.Chunk
		for _, doc := range docs {
			chunks := ingest.ChunkText(doc.Content, chunkSize, chunkOverlap, doc.Metadata)
			allChunks = append(allChunks, chunks...)
		}

		if len(allChunks) == 0 {
			return jsonResult(map[string]any{"status": "success", "chunks_created": 0})
		}

		texts := make([]string, len(allChunks))
		for i, c := range allChunks {
			texts[i] = c.Content
		}
		vectors, err := deps.Embedder.Embed(ctx, texts)
		if err != nil {
			return errResult(fmt.Sprintf("embed failed: %v", err)), nil, nil
		}

		points := make([]*pb.PointStruct, len(allChunks))
		for i, c := range allChunks {
			vec32 := make([]float32, len(vectors[i]))
			for j, v := range vectors[i] {
				vec32[j] = float32(v)
			}
			id := uuid.New().String()
			source := ""
			if c.Metadata != nil {
				source = c.Metadata["source"]
			}
			points[i] = &pb.PointStruct{
				Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
				Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: vec32}}},
				Payload: map[string]*pb.Value{
					"content": {Kind: &pb.Value_StringValue{StringValue: c.Content}},
					"source":  {Kind: &pb.Value_StringValue{StringValue: source}},
				},
			}
		}

		if err := deps.VectorDB.Upsert(ctx, args.Collection, points); err != nil {
			return errResult(fmt.Sprintf("upsert failed: %v", err)), nil, nil
		}

		deps.Log.Info("tool completed", "tool", "ingest_documents", "docs", len(docs), "chunks", len(allChunks), "duration_ms", time.Since(start).Milliseconds())
		return jsonResult(map[string]any{
			"status":           "success",
			"documents_parsed": len(docs),
			"chunks_created":   len(allChunks),
			"collection":       args.Collection,
		})
	}
}

func searchHandler(deps *Deps) mcp.ToolHandlerFor[SearchDocumentsArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SearchDocumentsArgs) (*mcp.CallToolResult, any, error) {
		topK := args.TopK
		if topK <= 0 {
			topK = deps.Cfg.Defaults.TopK
		}
		vec, err := deps.Embedder.EmbedQuery(ctx, args.Query)
		if err != nil {
			return errResult(fmt.Sprintf("embed query failed: %v", err)), nil, nil
		}
		results, err := deps.VectorDB.Search(ctx, args.Collection, vec, topK)
		if err != nil {
			return errResult(fmt.Sprintf("search failed: %v", err)), nil, nil
		}
		return jsonResult(map[string]any{"results": results, "total": len(results)})
	}
}

func createCollectionHandler(deps *Deps) mcp.ToolHandlerFor[CollectionCreateArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CollectionCreateArgs) (*mcp.CallToolResult, any, error) {
		dist := args.Distance
		if dist == "" {
			dist = deps.Cfg.Defaults.Distance
		}
		dim := uint64(deps.Cfg.Defaults.EmbeddingDim)
		if err := deps.VectorDB.CreateCollection(ctx, args.Name, dim, dist); err != nil {
			return errResult(fmt.Sprintf("create collection failed: %v", err)), nil, nil
		}
		return jsonResult(map[string]any{"created": args.Name, "dimension": dim, "distance": dist})
	}
}

func listCollectionsHandler(deps *Deps) mcp.ToolHandlerFor[struct{}, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		names, err := deps.VectorDB.ListCollections(ctx)
		if err != nil {
			return errResult(fmt.Sprintf("list collections failed: %v", err)), nil, nil
		}
		return jsonResult(map[string]any{"collections": names})
	}
}

func deleteCollectionHandler(deps *Deps) mcp.ToolHandlerFor[CollectionNameArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CollectionNameArgs) (*mcp.CallToolResult, any, error) {
		if err := deps.VectorDB.DeleteCollection(ctx, args.Name); err != nil {
			return errResult(fmt.Sprintf("delete failed: %v", err)), nil, nil
		}
		return jsonResult(map[string]any{"deleted": args.Name, "status": "success"})
	}
}

// -------------------- helpers --------------------

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %v", err)), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}

func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

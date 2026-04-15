package mcpserver

import (
	"context"
	"encoding/json"

	"seraglf/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerResources(server *mcp.Server, cfg *config.Config) {
	server.AddResource(&mcp.Resource{
		Name:        "config",
		URI:         "seraglf://config",
		Description: "Sanitized runtime config for debugging MCP clients: transport, HTTP port, model names, Qdrant address, embedding dimension, memory collection names, and limits. Secrets (API keys, DSN passwords) are omitted.",
		MIMEType:    "application/json",
	}, configResource(cfg))

	server.AddResource(&mcp.Resource{
		Name:        "health",
		URI:         "seraglf://health",
		Description: "Lightweight liveness payload from the MCP process. For full HTTP readiness use GET /health on the gateway when using streamable HTTP transport.",
		MIMEType:    "application/json",
	}, healthResource())
}

func configResource(cfg *config.Config) mcp.ResourceHandler {
	return func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		sanitized := map[string]any{
			"mcp_transport":               cfg.Server.MCPTransport,
			"http_port":                   cfg.Server.HTTPPort,
			"llm_provider":                cfg.LLM.Provider,
			"grader_model":                cfg.LLM.GraderModel,
			"generator_model":             cfg.LLM.GeneratorModel,
			"embedding_model":             cfg.Embedding.Model,
			"qdrant_address":              cfg.Qdrant.Address,
			"default_top_k":               cfg.Defaults.TopK,
			"embedding_dim":               cfg.Defaults.EmbeddingDim,
			"memory_short_term_ttl_hours": cfg.Memory.ShortTermTTLHours,
			"memory_max_messages":         cfg.Memory.MaxConversationMessages,
			"memory_summary_collection":   cfg.Memory.SummaryCollection,
			"memory_facts_collection":     cfg.Memory.FactsCollection,
		}
		data, _ := json.Marshal(sanitized)
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(data),
			}},
		}, nil
	}
}

func healthResource() mcp.ResourceHandler {
	return func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		data, _ := json.Marshal(map[string]string{"status": "ok"})
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     string(data),
			}},
		}, nil
	}
}

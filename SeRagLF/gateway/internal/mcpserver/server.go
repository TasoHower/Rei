package mcpserver

import (
	"log/slog"

	"seraglf/internal/config"
	"seraglf/internal/embedding"
	"seraglf/internal/memory"
	"seraglf/internal/qdrant"
	"seraglf/internal/selfrag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Deps struct {
	Cfg       *config.Config
	Pipeline  *selfrag.Pipeline
	Embedder  *embedding.Service
	VectorDB  *qdrant.Client
	ShortTerm *memory.ShortTermStore
	LongTerm  *memory.LongTermStore
	Extractor *memory.Extractor
	Log       *slog.Logger
}

func New(deps *Deps) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "seraglf",
			Version: "0.6.0",
		},
		&mcp.ServerOptions{},
	)

	registerTools(server, deps)
	registerMemoryTools(server, deps)
	registerResources(server, deps.Cfg)

	return server
}

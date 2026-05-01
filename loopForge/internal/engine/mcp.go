package engine

import (
	"context"

	"github.com/TasoHower/Rei/loopForge/pkg/mcp/cfg"
)

// MCPConnector manages MCP server sessions and tool discovery.
type MCPConnector interface {
	Connect(ctx context.Context, profile cfg.MCPServerProfile) (MCPSession, error)
}

// MCPSession is one active MCP connection for tools/list and tools/call.
type MCPSession interface {
	ServerID() string
	ListMappedTools(ctx context.Context) ([]cfg.MCPMappedTool, error)
	CallTool(ctx context.Context, mcpToolName string, argumentsJSON string) (string, error)
	Close() error
}

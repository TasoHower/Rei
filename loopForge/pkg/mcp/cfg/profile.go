package cfg

// MCPTransportKind selects how the engine connects to an MCP server.
type MCPTransportKind string

const (
	MCPTransportStdio          MCPTransportKind = "stdio"
	MCPTransportStreamableHTTP MCPTransportKind = "streamable_http"
)

// MCPServerProfile is one MCP server connection definition.
type MCPServerProfile struct {
	ID         string
	Transport  MCPTransportKind
	Command    []string
	URL        string
	Env        map[string]string
	Headers    map[string]string
	ToolPrefix string
}

// MCPMappedTool maps an MCP tool listing entry to an exposed tool name.
type MCPMappedTool struct {
	ServerID    string
	MCPToolName string
	ExposedName string
	InputSchema string
}

// MCPResourceRef identifies a resource for optional prefetch or read paths.
type MCPResourceRef struct {
	ServerID string
	URI      string
}

// MCPPromptRef identifies a prompt template from an MCP server.
type MCPPromptRef struct {
	ServerID string
	Name     string
}

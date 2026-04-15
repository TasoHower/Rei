package defaults

import "time"

// DevServer bind defaults (local-only; production should disable or authenticate).
const (
	DevServerHost = "127.0.0.1"
	DevServerPort = 52538
)

// HTTP timeouts for MCP streamable HTTP clients (baseline).
const (
	MCPHTTPDialTimeout  = 10 * time.Second
	MCPHTTPReadTimeout  = 120 * time.Second
	MCPHTTPWriteTimeout = 30 * time.Second
)

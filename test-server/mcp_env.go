package main

import (
	"log/slog"
	"os"
	"strings"

	"loopforge/pkg/mcp/cfg"
)

// logMCPStartup prints which MCP endpoint test-server will use (default seraglf on :8080).
func logMCPStartup(logger *slog.Logger) {
	if logger == nil {
		return
	}
	if strings.TrimSpace(os.Getenv("TEST_SERVER_DISABLE_MCP")) == "1" {
		logger.Info("test-server MCP disabled", "env", "TEST_SERVER_DISABLE_MCP=1")
		return
	}
	profs, _ := mcpAgentExtras()
	if len(profs) == 0 {
		return
	}
	p := profs[0]
	logger.Info("test-server MCP endpoint", "mcp_url", p.URL, "mcp_id", p.ID)
}

// Default streamable HTTP MCP for local testing: seraglf on port 8080.
// Override with LOOPFORGE_MCP_TEST_URL or TEST_SERVER_MCP_URL. Set TEST_SERVER_DISABLE_MCP=1 to skip MCP.
const defaultSeraglfMCPURL = "http://127.0.0.1:8080/mcp"

// MCP is configured via the same env vars as loopForge integration tests (when set).
// If no URL env is set, the default points at local seraglf unless TEST_SERVER_DISABLE_MCP=1.
func mcpAgentExtras() (profiles []cfg.MCPServerProfile, systemSuffix string) {
	if strings.TrimSpace(os.Getenv("TEST_SERVER_DISABLE_MCP")) == "1" {
		return nil, ""
	}
	u := strings.TrimSpace(os.Getenv("LOOPFORGE_MCP_TEST_URL"))
	if u == "" {
		u = strings.TrimSpace(os.Getenv("TEST_SERVER_MCP_URL"))
	}
	if u == "" {
		u = defaultSeraglfMCPURL
	}
	prefix := strings.TrimSpace(os.Getenv("LOOPFORGE_MCP_TEST_PREFIX"))
	if prefix == "" {
		prefix = strings.TrimSpace(os.Getenv("TEST_SERVER_MCP_PREFIX"))
	}
	if prefix == "" {
		prefix = "gmp__"
	}
	id := strings.TrimSpace(os.Getenv("TEST_SERVER_MCP_ID"))
	if id == "" {
		id = "google-maps"
	}
	profiles = []cfg.MCPServerProfile{{
		ID:         id,
		Transport:  cfg.MCPTransportStreamableHTTP,
		URL:        u,
		ToolPrefix: prefix,
	}}
	systemSuffix = `

[MCP / seraglf]
You have additional tools whose names may be prefixed (e.g. gmp__). Use them per each tool description; call prerequisite tools such as retrieve-instructions when required.`
	return profiles, systemSuffix
}

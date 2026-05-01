package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/TasoHower/rei/loopForge/pkg/mcp/cfg"
)

// MCPFields holds MCP server settings from the test UI (custom mode).
type MCPFields struct {
	ID         string            `json:"id"`
	Transport  string            `json:"transport"` // streamable_http | stdio
	URL        string            `json:"url"`
	Command    []string          `json:"command"`
	Headers    map[string]string `json:"headers"`
	Env        map[string]string `json:"env"`
	ToolPrefix string            `json:"tool_prefix"`
	Allowlist  []string          `json:"allowlist"`
}

func trimStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// resolveMCP returns profiles and system prompt suffix for this chat request.
// mcp_mode: "" or "env" -> env defaults (mcpAgentExtras); "off" -> none; "custom" -> MCPFields.
func (r *ChatRequest) resolveMCP() (profiles []cfg.MCPServerProfile, systemSuffix string, err error) {
	mode := strings.TrimSpace(strings.ToLower(r.MCPMode))
	if mode == "" {
		mode = "env"
	}
	switch mode {
	case "env":
		p, s := mcpAgentExtras()
		return p, s, nil
	case "off":
		return nil, "", nil
	case "custom":
		return buildMCPFromFields(r.MCP)
	default:
		return nil, "", fmt.Errorf("invalid mcp_mode %q (use env, off, or custom)", r.MCPMode)
	}
}

func buildMCPFromFields(f *MCPFields) (profiles []cfg.MCPServerProfile, systemSuffix string, err error) {
	if f == nil {
		return nil, "", fmt.Errorf("mcp object is required when mcp_mode is custom")
	}
	t := strings.TrimSpace(strings.ToLower(f.Transport))
	var kind cfg.MCPTransportKind
	switch t {
	case "streamable_http", "http", "":
		kind = cfg.MCPTransportStreamableHTTP
	case "stdio":
		kind = cfg.MCPTransportStdio
	default:
		return nil, "", fmt.Errorf("invalid mcp.transport %q (use streamable_http or stdio)", f.Transport)
	}

	id := strings.TrimSpace(f.ID)
	if id == "" {
		id = "mcp"
	}
	prefix := strings.TrimSpace(f.ToolPrefix)
	if prefix == "" {
		prefix = "mcp__"
	}
	allow := trimStrings(f.Allowlist)

	p := cfg.MCPServerProfile{
		ID:            id,
		Transport:     kind,
		ToolPrefix:    prefix,
		ToolAllowlist: allow,
	}
	if len(f.Headers) > 0 {
		p.Headers = f.Headers
	}
	if len(f.Env) > 0 {
		p.Env = f.Env
	}

	switch kind {
	case cfg.MCPTransportStreamableHTTP:
		u := strings.TrimSpace(f.URL)
		if u == "" {
			return nil, "", fmt.Errorf("mcp.url is required for streamable_http transport")
		}
		p.URL = u
	case cfg.MCPTransportStdio:
		cmd := trimStrings(f.Command)
		if len(cmd) == 0 {
			return nil, "", fmt.Errorf("mcp.command is required for stdio transport (non-empty argv)")
		}
		p.Command = cmd
	}

	suffix := `

[MCP]
You have additional tools whose names are prefixed (see ToolPrefix). Follow each tool description; call prerequisite tools when required.`
	return []cfg.MCPServerProfile{p}, suffix, nil
}

// logMCPStartup prints which MCP endpoint test-server will use (default test-mcp on :9089).
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

// Default streamable HTTP MCP for local testing: test-mcp on port 9089.
// Override with LOOPFORGE_MCP_TEST_URL or TEST_SERVER_MCP_URL. Set TEST_SERVER_DISABLE_MCP=1 to skip MCP.
const defaultTestMCPURL = "http://127.0.0.1:9089/mcp"

// MCP is configured via the same env vars as loopForge integration tests (when set).
// If no URL env is set, the default points at local test-mcp unless TEST_SERVER_DISABLE_MCP=1.
func mcpAgentExtras() (profiles []cfg.MCPServerProfile, systemSuffix string) {
	if strings.TrimSpace(os.Getenv("TEST_SERVER_DISABLE_MCP")) == "1" {
		return nil, ""
	}
	u := strings.TrimSpace(os.Getenv("LOOPFORGE_MCP_TEST_URL"))
	if u == "" {
		u = strings.TrimSpace(os.Getenv("TEST_SERVER_MCP_URL"))
	}
	if u == "" {
		u = defaultTestMCPURL
	}
	prefix := strings.TrimSpace(os.Getenv("LOOPFORGE_MCP_TEST_PREFIX"))
	if prefix == "" {
		prefix = strings.TrimSpace(os.Getenv("TEST_SERVER_MCP_PREFIX"))
	}
	if prefix == "" {
		prefix = "tm__"
	}
	id := strings.TrimSpace(os.Getenv("TEST_SERVER_MCP_ID"))
	if id == "" {
		id = "test-mcp"
	}
	profiles = []cfg.MCPServerProfile{{
		ID:         id,
		Transport:  cfg.MCPTransportStreamableHTTP,
		URL:        u,
		ToolPrefix: prefix,
	}}
	systemSuffix = `

[MCP / test-mcp]
You have additional arithmetic tools whose names are prefixed (e.g. tm__add, tm__subtract, tm__multiply, tm__divide). Use them for calculations; arguments are JSON numbers "a" and "b".`
	return profiles, systemSuffix
}

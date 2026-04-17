package mcp

import (
	"encoding/json"
	"fmt"
	"slices"

	"loopforge/pkg/mcp/cfg"
)

// ExposedToolName is the model-visible tool name: ToolPrefix + MCP protocol tool name.
func ExposedToolName(toolPrefix, mcpToolName string) string {
	return toolPrefix + mcpToolName
}

// AllowedByAllowlist returns true if mcpToolName is allowed. Empty allowlist allows all.
func AllowedByAllowlist(allowlist []string, mcpToolName string) bool {
	if len(allowlist) == 0 {
		return true
	}
	return slices.Contains(allowlist, mcpToolName)
}

// MappedToolParts holds everything needed to build model.ToolInfo for an MCP tool except Handle.
type MappedToolParts struct {
	Mapped      *cfg.MCPMappedTool
	Parameters  map[string]interface{}
	Description string
}

// BuildMappedToolParts constructs mapping metadata from a tools/list entry (description + inputSchema).
// ServerID and toolPrefix identify the server; mcpToolName is the protocol-side name.
func BuildMappedToolParts(serverID, toolPrefix, mcpToolName, description string, inputSchema any) (*MappedToolParts, error) {
	if serverID == "" {
		return nil, fmt.Errorf("mcp mapped tool: empty serverID")
	}
	if mcpToolName == "" {
		return nil, fmt.Errorf("mcp mapped tool: empty mcpToolName")
	}
	exposed := ExposedToolName(toolPrefix, mcpToolName)
	params, err := NormalizeInputSchema(inputSchema)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("mcp mapped tool: marshal schema: %w", err)
	}
	m := &cfg.MCPMappedTool{
		ServerID:    serverID,
		MCPToolName: mcpToolName,
		ExposedName: exposed,
		InputSchema: string(raw),
	}
	return &MappedToolParts{
		Mapped:      m,
		Parameters:  params,
		Description: description,
	}, nil
}

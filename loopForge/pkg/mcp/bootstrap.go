package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"loopforge/pkg/mcp/cfg"
	"loopforge/pkg/model"
	applog "loopforge/pkg/log"
)

// BootstrapToolInfos connects to each MCP profile, runs tools/list, maps tools to model ToolInfo
// values with Handles that call tools/call on the right session. Tool exposure names are
// ToolPrefix + MCP tool name (see [ExposedToolName]).
//
// The returned stop function closes all sessions (and stdio subprocesses); call it when the
// agent or process no longer needs MCP (typically defer stop() after success).
func BootstrapToolInfos(ctx context.Context, profiles ...cfg.MCPServerProfile) ([]*model.ToolInfo, func(), error) {
	if len(profiles) == 0 {
		return nil, nil, fmt.Errorf("mcp: BootstrapToolInfos requires at least one MCPServerProfile")
	}
	var all []*model.ToolInfo
	var stops []func()
	stopAll := func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}

	for i := range profiles {
		p := profiles[i]
		session, stop, err := ConnectClientSession(ctx, p)
		if err != nil {
			applog.Default().Error("mcp: bootstrap connect failed",
				"server_id", p.ID,
				"transport", string(p.Transport),
				"err", err,
			)
			stopAll()
			return nil, nil, err
		}
		stops = append(stops, stop)

		serverID := strings.TrimSpace(p.ID)
		if serverID == "" {
			serverID = fmt.Sprintf("mcp%d", i)
		}
		tools, err := listSessionTools(ctx, session)
		if err != nil {
			applog.Default().Error("mcp: bootstrap list_tools failed",
				"server_id", serverID,
				"transport", string(p.Transport),
				"err", err,
			)
			stopAll()
			return nil, nil, err
		}
		if len(tools) == 0 {
			applog.Default().Warn("mcp: bootstrap tools/list empty",
				"server_id", serverID,
				"transport", string(p.Transport),
			)
		}

		for _, t := range tools {
			if t == nil || t.Name == "" {
				continue
			}
			if !AllowedByAllowlist(p.ToolAllowlist, t.Name) {
				continue
			}
			parts, err := BuildMappedToolParts(serverID, p.ToolPrefix, t.Name, t.Description, t.InputSchema)
			if err != nil {
				applog.Default().Error("mcp: bootstrap map tool failed",
					"server_id", serverID,
					"mcp_tool", t.Name,
					"err", err,
				)
				stopAll()
				return nil, nil, err
			}
			mcpName := t.Name
			sess := session
			handle := func(ctx context.Context, argumentsJSON string) (string, error) {
				return callMCPTool(ctx, sess, mcpName, argumentsJSON)
			}
			all = append(all, parts.ToToolInfo(handle))
		}
	}

	return all, stopAll, nil
}

func listSessionTools(ctx context.Context, session *sdkmcp.ClientSession) ([]*sdkmcp.Tool, error) {
	var out []*sdkmcp.Tool
	var cursor string
	for {
		res, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

func callMCPTool(ctx context.Context, session *sdkmcp.ClientSession, mcpToolName, argumentsJSON string) (string, error) {
	var args map[string]any
	s := strings.TrimSpace(argumentsJSON)
	if s != "" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			return "", fmt.Errorf("mcp tool %q arguments: %w", mcpToolName, err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      mcpToolName,
		Arguments: args,
	})
	if err != nil {
		return "", err
	}
	return formatCallToolResult(res), nil
}

func formatCallToolResult(res *sdkmcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err == nil {
			return string(b)
		}
	}
	var b strings.Builder
	for _, c := range res.Content {
		if c == nil {
			continue
		}
		if tc, ok := c.(*sdkmcp.TextContent); ok && tc != nil {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

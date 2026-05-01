// Package debug is the SDK debug entry point: protocol-level helpers for development and
// troubleshooting (currently MCP: ping, tools/list, tools/call). Import as [loopforge/debug].
// It shares [loopforge/pkg/mcp.ConnectClientSession] with the integration path; failures log at
// WARN/ERROR via [loopforge/pkg/log.Default].
package debug

import (
	"context"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcppkg "github.com/TasoHower/rei/loopForge/pkg/mcp"
	"github.com/TasoHower/rei/loopForge/pkg/mcp/cfg"
	lferrors "github.com/TasoHower/rei/loopForge/pkg/errors"
	applog "github.com/TasoHower/rei/loopForge/pkg/log"
)

// Conn wraps an MCP client session for debug operations.
type Conn struct {
	session   *sdkmcp.ClientSession
	serverID  string
	transport cfg.MCPTransportKind
}

// ToolListing is one row from tools/list (MCP tool name and metadata).
type ToolListing struct {
	Name        string
	Description string
	InputSchema any
}

// CallToolOutcome summarizes tools/call for debugging (text content and error flag).
type CallToolOutcome struct {
	IsError           bool
	Text              string
	StructuredContent any
}

// Dial connects to an MCP server described by profile and returns a Conn plus stop
// to release the session (and subprocess for stdio).
func Dial(ctx context.Context, profile cfg.MCPServerProfile) (*Conn, func(), error) {
	log := applog.Default()
	sid := strings.TrimSpace(profile.ID)
	if sid == "" {
		sid = "(no id)"
	}
	tport := profile.Transport
	session, stop, err := mcppkg.ConnectClientSession(ctx, profile)
	if err != nil {
		log.Error("mcp debug: dial failed",
			"server_id", sid,
			"transport", string(tport),
			"err", err,
		)
		return nil, nil, err
	}
	return &Conn{session: session, serverID: sid, transport: tport}, stop, nil
}

// Ping sends an MCP ping request.
func (c *Conn) Ping(ctx context.Context) error {
	log := applog.Default()
	if c == nil || c.session == nil {
		log.Error("mcp debug: ping skipped", "reason", "nil connection")
		return lferrors.ErrDebugNilConnection
	}
	err := c.session.Ping(ctx, nil)
	if err != nil {
		log.Error("mcp debug: ping failed", "server_id", c.serverID, "transport", string(c.transport), "err", err)
	}
	return err
}

// ListTools returns all tools from tools/list (handles pagination).
func (c *Conn) ListTools(ctx context.Context) ([]ToolListing, error) {
	log := applog.Default()
	if c == nil || c.session == nil {
		log.Error("mcp debug: list_tools skipped", "reason", "nil connection")
		return nil, lferrors.ErrDebugNilConnection
	}
	var out []ToolListing
	var cursor string
	for {
		res, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			log.Error("mcp debug: list_tools failed",
				"server_id", c.serverID,
				"transport", string(c.transport),
				"err", err,
			)
			return nil, err
		}
		for _, t := range res.Tools {
			if t == nil {
				continue
			}
			out = append(out, ToolListing{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(out) == 0 {
		log.Warn("mcp debug: list_tools returned zero tools",
			"server_id", c.serverID,
			"transport", string(c.transport),
		)
	}
	return out, nil
}

// CallTool invokes tools/call using the MCP protocol tool name (not the prefixed LLM exposure name).
func (c *Conn) CallTool(ctx context.Context, mcpToolName string, arguments map[string]any) (*CallToolOutcome, error) {
	log := applog.Default()
	if c == nil || c.session == nil {
		log.Error("mcp debug: call_tool skipped", "reason", "nil connection", "tool", mcpToolName)
		return nil, lferrors.ErrDebugNilConnection
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	res, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      mcpToolName,
		Arguments: arguments,
	})
	if err != nil {
		log.Error("mcp debug: call_tool transport failed",
			"server_id", c.serverID,
			"transport", string(c.transport),
			"tool", mcpToolName,
			"err", err,
		)
		return nil, err
	}
	out := &CallToolOutcome{
		IsError:           res.IsError,
		StructuredContent: res.StructuredContent,
		Text:              joinTextContent(res.Content),
	}
	if res != nil && res.IsError {
		textSample := out.Text
		if len(textSample) > 200 {
			textSample = textSample[:200] + "..."
		}
		log.Warn("mcp debug: call_tool returned IsError from server",
			"server_id", c.serverID,
			"tool", mcpToolName,
			"text_sample", textSample,
		)
	}
	return out, nil
}

func joinTextContent(parts []sdkmcp.Content) string {
	var b strings.Builder
	for _, p := range parts {
		if p == nil {
			continue
		}
		if tc, ok := p.(*sdkmcp.TextContent); ok && tc != nil {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

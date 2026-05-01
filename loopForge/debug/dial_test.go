package debug

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/TasoHower/Rei/loopForge/pkg/mcp/cfg"
	lferrors "github.com/TasoHower/Rei/loopForge/pkg/errors"
)

// SeraglfDockerURL is the streamable HTTP MCP endpoint for a local seraglf container
// (SeRagLF gateway serves MCP at /mcp). Example:
//
//	export LOOPFORGE_SERAGLF_MCP_URL=http://127.0.0.1:18080/mcp
const envSeraglfMCPURL = "LOOPFORGE_SERAGLF_MCP_URL"

func TestDialPingListCallStreamableHTTP(t *testing.T) {
	base := os.Getenv(envSeraglfMCPURL)
	if base == "" {
		t.Skipf("set %s to run against docker seraglf (e.g. http://127.0.0.1:18080/mcp)", envSeraglfMCPURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, stop, err := Dial(ctx, cfg.MCPServerProfile{
		ID:        "seraglf",
		Transport: cfg.MCPTransportStreamableHTTP,
		URL:       base,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer stop()

	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	tools, err := conn.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool from seraglf")
	}
	if !slices.ContainsFunc(tools, func(t ToolListing) bool { return t.Name == "list_collections" }) {
		t.Fatalf("expected list_collections in tools/list, got %+v", toolNames(tools))
	}

	out, err := conn.CallTool(ctx, "list_collections", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out == nil {
		t.Fatal("CallTool: nil outcome")
	}
	if out.IsError {
		t.Fatalf("list_collections returned tool error: %s", out.Text)
	}

	t.Logf("listed %d tools, first: %q", len(tools), tools[0].Name)
}

func toolNames(tools []ToolListing) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

func TestNilConnErrors(t *testing.T) {
	var c *Conn
	if err := c.Ping(context.Background()); !errors.Is(err, lferrors.ErrDebugNilConnection) {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := c.ListTools(context.Background()); !errors.Is(err, lferrors.ErrDebugNilConnection) {
		t.Fatalf("ListTools: %v", err)
	}
	if _, err := c.CallTool(context.Background(), "x", nil); !errors.Is(err, lferrors.ErrDebugNilConnection) {
		t.Fatalf("CallTool: %v", err)
	}
}

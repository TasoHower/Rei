package agent

import (
	"context"
	"fmt"

	"github.com/TasoHower/Rei/loopForge/pkg/mcp"
	"github.com/TasoHower/Rei/loopForge/pkg/mcp/cfg"
	"github.com/TasoHower/Rei/loopForge/pkg/model"
)

// MCPBinding holds ToolInfos produced from MCP discovery plus a Stop func for session teardown.
// Prefer [WithMCPServerProfiles] on the [Agent] so discovery runs per run with automatic cleanup;
// use AttachMCP when you need ToolInfos without starting a run (e.g. custom merging).
//
//	b, err := agent.AttachMCP(ctx, profile)
//	if err != nil { ... }
//	defer b.Stop()
//	ag := agent.New(chat, agent.WithToolInfos(append(localTools, b.ToolInfos...)))
type MCPBinding struct {
	ToolInfos []*model.ToolInfo
	Stop      func()
}

// AttachMCP runs [mcp.BootstrapToolInfos] for one or more server profiles and returns
// merged ToolInfos suitable for [WithToolInfos]. Call Stop when finished.
func AttachMCP(ctx context.Context, profiles ...cfg.MCPServerProfile) (*MCPBinding, error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("agent: AttachMCP requires at least one MCPServerProfile")
	}
	infos, stop, err := mcp.BootstrapToolInfos(ctx, profiles...)
	if err != nil {
		return nil, err
	}
	return &MCPBinding{ToolInfos: infos, Stop: stop}, nil
}

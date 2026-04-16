package transfer

import (
	"encoding/json"
	"fmt"
	"strings"

	"loopforge/pkg/model"
)

const toolPrefix = "transfer_to_"

// BuildTools generates one ToolInfo per allowed transfer target.
// These tools carry no Handle — the Orchestrator intercepts them via
// agent.ToolInterceptor as control-flow signals.
func BuildTools(current string, registry *Registry) []*model.ToolInfo {
	targets := registry.TransferTargetsFor(current)
	if len(targets) == 0 {
		return nil
	}

	tools := make([]*model.ToolInfo, 0, len(targets))
	for _, name := range targets {
		cfg, ok := registry.Get(name)
		if !ok {
			continue
		}

		desc := cfg.Description
		if desc == "" {
			desc = cfg.SystemInstructions
		}
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}

		tools = append(tools, &model.ToolInfo{
			Name:        toolPrefix + name,
			Description: fmt.Sprintf("Transfer the conversation to the %q agent. %s", name, desc),
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Why this transfer is needed",
					},
				},
				"required": []string{"reason"},
			},
		})
	}
	return tools
}

// IsTransferTool returns true if the tool call targets a transfer_to_{name}
// tool. It satisfies agent.ToolInterceptor and can be assigned directly.
func IsTransferTool(tc model.ToolCallPart) bool {
	return strings.HasPrefix(tc.Name, toolPrefix)
}

// TargetAgent extracts the destination agent name from a transfer tool name.
func TargetAgent(toolName string) string {
	return strings.TrimPrefix(toolName, toolPrefix)
}

// ExtractReason parses the "reason" field from the tool call arguments JSON.
func ExtractReason(argsJSON string) string {
	var args struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return argsJSON
	}
	return args.Reason
}

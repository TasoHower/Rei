package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"loopforge/pkg/model"
)

const transferToolPrefix = "transfer_to_"

// buildTransferTools generates one ToolInfo per handoff target registered on
// current. These tools carry no Handle — the Runner intercepts them via
// ToolInterceptor as control-flow signals.
func buildTransferTools(current *RunnerAgent) []*model.ToolInfo {
	targets := current.Handoffs()
	if len(targets) == 0 {
		return nil
	}

	tools := make([]*model.ToolInfo, 0, len(targets))
	for _, t := range targets {
		desc := t.Description
		if desc == "" {
			desc = t.SystemInstructions
		}
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}

		tools = append(tools, &model.ToolInfo{
			Name:        transferToolPrefix + t.Name,
			Description: fmt.Sprintf("Transfer the conversation to the %q agent. %s", t.Name, desc),
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

// buildTransferPrompt generates a system prompt segment describing the
// available transfer targets. Returns "" if there are no handoffs.
func buildTransferPrompt(current *RunnerAgent) string {
	targets := current.Handoffs()
	if len(targets) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n## Multi-Agent Transfer\n")
	b.WriteString("You are part of a multi-agent system. ")
	b.WriteString("When the user's request is better handled by another agent, ")
	b.WriteString("call the corresponding transfer tool with a reason.\n\n")
	b.WriteString("Available agents:\n")
	for _, t := range targets {
		desc := t.Description
		if desc == "" {
			desc = t.SystemInstructions
		}
		if len(desc) > 120 {
			desc = desc[:120] + "..."
		}
		fmt.Fprintf(&b, "- `%s%s` — %s\n", transferToolPrefix, t.Name, desc)
	}
	return b.String()
}

// isTransferTool returns true if the tool call targets a transfer_to_{name} tool.
func isTransferTool(tc model.ToolCallPart) bool {
	return strings.HasPrefix(tc.Name, transferToolPrefix)
}

// targetAgent extracts the destination agent name from a transfer tool name.
func targetAgent(toolName string) string {
	return strings.TrimPrefix(toolName, transferToolPrefix)
}

// extractReason parses the "reason" field from the tool call arguments JSON.
func extractReason(argsJSON string) string {
	var args struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return argsJSON
	}
	return args.Reason
}

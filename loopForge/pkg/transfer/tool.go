package transfer

import (
	"encoding/json"
	"fmt"
	"strings"

	"loopforge/pkg/agent"
	"loopforge/pkg/model"
)

const toolPrefix = "transfer_to_"

// BuildTools generates one ToolInfo per handoff target registered on current.
// These tools carry no Handle — the Orchestrator intercepts them via
// agent.ToolInterceptor as control-flow signals.
func BuildTools(current *agent.RunnerAgent) []*model.ToolInfo {
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
			Name:        toolPrefix + t.Name,
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

// BuildTransferPrompt generates a system prompt segment describing the
// available transfer targets. Returns "" if there are no handoffs.
func BuildTransferPrompt(current *agent.RunnerAgent) string {
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
		fmt.Fprintf(&b, "- `%s%s` — %s\n", toolPrefix, t.Name, desc)
	}
	return b.String()
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

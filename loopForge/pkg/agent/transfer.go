package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"loopforge/pkg/model"
)

const TransferToolPrefix = "transfer_to_"

// BuildTransferTools generates one ToolInfo per handoff target registered on
// current. These tools carry no Handle — the Runner intercepts them via
// ToolInterceptor as control-flow signals.
func BuildTransferTools(current *Agent) []*model.ToolInfo {
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
			Name:        TransferToolPrefix + t.Name,
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
func BuildTransferPrompt(current *Agent) string {
	targets := current.Handoffs()
	if len(targets) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n## Multi-Agent Delegation Protocol\n")
	b.WriteString("You operate within a multi-agent system composed of specialized agents. ")
	b.WriteString("Your responsibility is to solve requests directly when possible, ")
	b.WriteString("and delegate tasks when another agent is better suited.\n\n")

	b.WriteString("### When to Transfer\n")
	b.WriteString("Use a transfer tool when ANY of the following applies:\n")
	b.WriteString("- Another agent has clearer domain expertise for the request.\n")
	b.WriteString("- The task requires capabilities, tools, or permissions you do not have.\n")
	b.WriteString("- The request can be parallelized into independent subtasks.\n")
	b.WriteString("- Specialized handling would improve speed, quality, or reliability.\n")
	b.WriteString("- You are uncertain and another agent is explicitly designed for this area.\n\n")

	b.WriteString("### When NOT to Transfer\n")
	b.WriteString("Do NOT transfer when:\n")
	b.WriteString("- You can complete the task accurately yourself.\n")
	b.WriteString("- Transfer adds unnecessary latency or complexity.\n")
	b.WriteString("- The request is trivial, conversational, or requires continuity best handled here.\n\n")

	b.WriteString("### Transfer Rules\n")
	b.WriteString("- Choose the single best agent first unless multi-agent execution is clearly beneficial.\n")
	b.WriteString("- Provide a concise reason describing the goal, context, and expected output.\n")
	b.WriteString("- Preserve important user constraints, preferences, and prior context.\n")
	b.WriteString("- Do not expose internal routing logic to the user.\n")
	b.WriteString("- If no agent is suitable, continue handling the request yourself.\n\n")

	b.WriteString("### After Transfer\n")
	b.WriteString("- Integrate the returned result into a coherent final answer.\n")
	b.WriteString("- Validate outputs before presenting them.\n")
	b.WriteString("- If needed, perform follow-up transfers iteratively.\n\n")

	b.WriteString("Available agents:\n")
	for _, t := range targets {
		desc := t.Description
		if desc == "" {
			desc = t.SystemInstructions
		}
		if len(desc) > 120 {
			desc = desc[:120] + "..."
		}
		fmt.Fprintf(&b, "- `%s%s` — %s\n", TransferToolPrefix, t.Name, desc)
	}
	return b.String()
}

// IsTransferTool returns true if the tool call targets a transfer_to_{name} tool.
func IsTransferTool(tc model.ToolCallPart) bool {
	return strings.HasPrefix(tc.Name, TransferToolPrefix)
}

// TargetAgent extracts the destination agent name from a transfer tool name.
func TargetAgent(toolName string) string {
	return strings.TrimPrefix(toolName, TransferToolPrefix)
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

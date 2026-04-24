package agent

import (
	"context"
	"strings"

	"loopforge/internal/defaults"
	lferrors "loopforge/pkg/errors"
	"loopforge/pkg/mcp"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/tool"
)

// mergedToolInfos returns ToolInfos + mcpToolInfos + ExtraTools + extraRuntime in the same
// order as bindModel uses for WithTools. Use this list when resolving tool
// execution (tool.Invoke) so names registered only on ExtraTools/runtime
// extras (e.g. var_set) still match.
func (a *Agent) mergedToolInfos(extraRuntime ...*model.ToolInfo) []*model.ToolInfo {
	n := len(a.ToolInfos) + len(a.mcpToolInfos) + len(a.ExtraTools) + len(extraRuntime)
	if n == 0 {
		return nil
	}
	merged := make([]*model.ToolInfo, 0, n)
	merged = append(merged, a.ToolInfos...)
	merged = append(merged, a.mcpToolInfos...)
	merged = append(merged, a.ExtraTools...)
	merged = append(merged, extraRuntime...)
	return merged
}

func (a *Agent) resetMCPSession() {
	if a.mcpStop != nil {
		a.mcpStop()
		a.mcpStop = nil
	}
	a.mcpToolInfos = nil
}

// bindModel merges ExtraTools and optional extraRuntimeTools with ToolInfos,
// bootstraps MCP tools when MCPServerProfiles is set, validates bindings, and
// returns a tool-bound model ready for streaming.
func (a *Agent) bindModel(ctx context.Context, extraRuntimeTools ...*model.ToolInfo) (model.ToolCallingChatModel, *lferrors.SetupError) {
	a.resetMCPSession()

	if len(a.MCPServerProfiles) > 0 {
		infos, stop, err := mcp.BootstrapToolInfos(ctx, a.MCPServerProfiles...)
		if err != nil {
			return nil, lferrors.NewSetupError("invalid_config", err.Error())
		}
		a.mcpStop = stop
		a.mcpToolInfos = infos
	}

	allTools := a.mergedToolInfos(extraRuntimeTools...)

	toValidate := make([]*model.ToolInfo, 0, len(a.ToolInfos)+len(a.mcpToolInfos))
	toValidate = append(toValidate, a.ToolInfos...)
	toValidate = append(toValidate, a.mcpToolInfos...)
	if len(toValidate) > 0 {
		if err := tool.ValidateBindings(toValidate, a.Executor); err != nil {
			a.resetMCPSession()
			return nil, lferrors.NewSetupError("invalid_config", err.Error())
		}
	}

	if len(allTools) > 0 {
		m, err := a.ChatModel.WithTools(allTools)
		if err != nil {
			a.resetMCPSession()
			return nil, lferrors.NewSetupError("tool_bind", err.Error())
		}
		return m, nil
	}
	return a.ChatModel, nil
}

// ResolveSpawnMaxDepth returns the maximum allowed [exchange.RunRef.Depth] of a
// *new* child (reject when parentDepth+1 >= this value, using defaults and request override).
func ResolveSpawnMaxDepth(req *request.RuntimeRequest) int {
	d := defaults.SpawnMaxDepthDefault
	if req != nil && req.Options.SpawnMaxDepth != nil && *req.Options.SpawnMaxDepth > 0 {
		d = *req.Options.SpawnMaxDepth
	}
	return d
}

// resolveMaxSteps determines the effective max loop iterations from the
// agent default, falling back to 16, with per-request override.
func (a *Agent) resolveMaxSteps(req *request.RuntimeRequest) int {
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 16
	}
	if req.Options.MaxSteps != nil && *req.Options.MaxSteps > 0 {
		maxSteps = *req.Options.MaxSteps
	}
	return maxSteps
}

// resolveCallOptions builds the effective call options and model name from
// agent defaults merged with per-request overrides.
func (a *Agent) resolveCallOptions(req *request.RuntimeRequest) ([]model.CallOption, string) {
	opts := append([]model.CallOption(nil), a.CallOptions...)
	modelName := a.ModelName
	if req.Options.Model != "" {
		modelName = req.Options.Model
		opts = append(opts, model.WithModel(modelName))
	}
	return opts, modelName
}

// buildMessages constructs the initial message slice from either inherited
// conversation history or fresh system + user messages.
// resolvedUser is the user message content after optional builder and VarStore
// substitution; it is ignored when inheritedMsgs is non-nil.
func (a *Agent) buildMessages(inheritedMsgs []*model.Message, req *request.RuntimeRequest, resolvedUser string) []*model.Message {
	if inheritedMsgs != nil {
		return replaceSystemMessage(inheritedMsgs, a.SystemInstructions)
	}
	msgs := make([]*model.Message, 0, 4)
	if strings.TrimSpace(a.SystemInstructions) != "" {
		msgs = append(msgs, &model.Message{Role: model.RoleSystem, Content: strings.TrimSpace(a.SystemInstructions)})
	}
	msgs = append(msgs, &model.Message{Role: model.RoleUser, Content: resolvedUser})
	return msgs
}

// replaceFirstUserMessage returns a copy of msgs with the first user message
// content replaced.
func replaceFirstUserMessage(msgs []*model.Message, content string) []*model.Message {
	out := make([]*model.Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if out[i] != nil && out[i].Role == model.RoleUser {
			nm := *out[i]
			nm.Content = content
			out[i] = &nm
			break
		}
	}
	return out
}

// replaceSystemMessage returns a copy of msgs with the first system message
// replaced (or prepended) with the new system instructions.
func replaceSystemMessage(msgs []*model.Message, system string) []*model.Message {
	out := make([]*model.Message, 0, len(msgs)+1)
	systemSet := false

	for _, m := range msgs {
		if m.Role == model.RoleSystem && !systemSet {
			if strings.TrimSpace(system) != "" {
				out = append(out, &model.Message{Role: model.RoleSystem, Content: strings.TrimSpace(system)})
			}
			systemSet = true
			continue
		}
		out = append(out, m)
	}

	if !systemSet && strings.TrimSpace(system) != "" {
		result := make([]*model.Message, 0, len(out)+1)
		result = append(result, &model.Message{Role: model.RoleSystem, Content: strings.TrimSpace(system)})
		result = append(result, out...)
		return result
	}
	return out
}

func runIDFrom(req *request.RuntimeRequest) string {
	if req.SessionID != "" {
		return req.SessionID
	}
	return "run"
}

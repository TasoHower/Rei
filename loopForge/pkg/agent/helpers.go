package agent

import (
	"strings"

	lferrors "loopforge/pkg/errors"
	"loopforge/pkg/model"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/tool"
)

// mergedToolInfos returns ToolInfos + ExtraTools + extraRuntime in the same
// order as bindModel uses for WithTools. Use this list when resolving tool
// execution (tool.Invoke) so names registered only on ExtraTools/runtime
// extras (e.g. var_set) still match.
func (a *Agent) mergedToolInfos(extraRuntime ...*model.ToolInfo) []*model.ToolInfo {
	nExtra := len(a.ExtraTools) + len(extraRuntime)
	if nExtra == 0 {
		return a.ToolInfos
	}
	merged := make([]*model.ToolInfo, 0, len(a.ToolInfos)+nExtra)
	merged = append(merged, a.ToolInfos...)
	merged = append(merged, a.ExtraTools...)
	merged = append(merged, extraRuntime...)
	return merged
}

// bindModel merges ExtraTools and optional extraRuntimeTools with ToolInfos,
// validates bindings, and returns a tool-bound model ready for streaming.
func (a *Agent) bindModel(extraRuntimeTools ...*model.ToolInfo) (model.ToolCallingChatModel, *lferrors.SetupError) {
	allTools := a.mergedToolInfos(extraRuntimeTools...)

	// Validate only the agent's own tools (ExtraTools have no Handle by design).
	if len(a.ToolInfos) > 0 {
		if err := tool.ValidateBindings(a.ToolInfos, a.Executor); err != nil {
			return nil, lferrors.NewSetupError("invalid_config", err.Error())
		}
	}

	if len(allTools) > 0 {
		m, err := a.ChatModel.WithTools(allTools)
		if err != nil {
			return nil, lferrors.NewSetupError("tool_bind", err.Error())
		}
		return m, nil
	}
	return a.ChatModel, nil
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
func (a *Agent) buildMessages(inheritedMsgs []*model.Message, req *request.RuntimeRequest) []*model.Message {
	if inheritedMsgs != nil {
		return replaceSystemMessage(inheritedMsgs, a.SystemInstructions)
	}
	msgs := make([]*model.Message, 0, 4)
	if strings.TrimSpace(a.SystemInstructions) != "" {
		msgs = append(msgs, &model.Message{Role: model.RoleSystem, Content: strings.TrimSpace(a.SystemInstructions)})
	}
	msgs = append(msgs, &model.Message{Role: model.RoleUser, Content: req.UserMessage})
	return msgs
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

package agent

import (
	"strings"

	"loopforge/pkg/model"
	"loopforge/pkg/runtime/request"
	"loopforge/pkg/tool"
)

// loopSetupError is a structured error from the pre-loop setup phase.
type loopSetupError struct {
	code string
	msg  string
}

// bindModel merges ExtraTools with ToolInfos, validates bindings, and returns
// a tool-bound model ready for streaming.
func (a *Agent) bindModel() (model.ToolCallingChatModel, *loopSetupError) {
	allTools := a.ToolInfos
	if len(a.ExtraTools) > 0 {
		merged := make([]*model.ToolInfo, 0, len(a.ToolInfos)+len(a.ExtraTools))
		merged = append(merged, a.ToolInfos...)
		merged = append(merged, a.ExtraTools...)
		allTools = merged
	}

	// Validate only the agent's own tools (ExtraTools have no Handle by design).
	if len(a.ToolInfos) > 0 {
		if err := tool.ValidateBindings(a.ToolInfos, a.Executor); err != nil {
			return nil, &loopSetupError{"invalid_config", err.Error()}
		}
	}

	if len(allTools) > 0 {
		m, err := a.ChatModel.WithTools(allTools)
		if err != nil {
			return nil, &loopSetupError{"tool_bind", err.Error()}
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

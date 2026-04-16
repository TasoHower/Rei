package agent

import (
	"context"

	"loopforge/pkg/runtime/request"
	"loopforge/pkg/variable"
)

// SystemPromptBuildContext is passed to SystemPromptBuilder when composing the
// effective system prompt before each model call.
type SystemPromptBuildContext struct {
	Ctx      context.Context
	Request  *request.RuntimeRequest
	VarStore *variable.VarStore
	Agent    *Agent
	// Step is the current loop iteration and starts from 0.
	Step int
	// BaseSystemPrompt is Agent.SystemInstructions before any dynamic composition.
	BaseSystemPrompt string
	// VariablePromptBlock is the VarStore prompt block snapshot before running
	// SystemPromptBuilder in this step.
	VariablePromptBlock string
}

// SystemPromptBuilder composes the final system prompt text.
// ReplaceDoubleBraceParams is applied after this callback using values from
// VarStore. If nil, RunLoop uses the default system prompt composition.
type SystemPromptBuilder func(SystemPromptBuildContext) (string, error)

package agent

import (
	"context"

	"github.com/TasoHower/Rei/loopForge/pkg/runtime/request"
	"github.com/TasoHower/Rei/loopForge/pkg/skill"
	"github.com/TasoHower/Rei/loopForge/pkg/variable"
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
	// MCPPromptFragment is reserved for MCP prompts/list injection (empty when not wired).
	MCPPromptFragment string
	// ResolvedSkills is the per-run snapshot of skills bound to this loop.
	ResolvedSkills []skill.SkillSpec
	// VariablePromptBlock is the VarStore prompt block snapshot before running
	// SystemPromptBuilder in this step.
	VariablePromptBlock string
}

// SystemPromptBuilder composes the final system prompt text.
// ReplaceDoubleBraceParams is applied after this callback using values from
// VarStore. If nil, RunLoop uses the default system prompt composition.
type SystemPromptBuilder func(SystemPromptBuildContext) (string, error)

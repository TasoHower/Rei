package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"loopforge/pkg/model"
)

const ShellToolName = "execute_shell_script"

type shellToolArgs struct {
	Skill  string   `json:"skill"`
	Script string   `json:"script"`
	Args   []string `json:"args"`
}

// ShellTool returns a ToolInfo that runs bundle scripts via ShellSkillJobRunner.
// Script lookup uses reg.Get(skillName); the registry must contain the skill definition.
func ShellTool(reg *SkillRegistry, runner SkillJobRunner) *model.ToolInfo {
	if runner == nil {
		runner = &ShellSkillJobRunner{}
	}
	return &model.ToolInfo{
		Name:        ShellToolName,
		Description: "Runs a shell script path relative to the named skill bundle. Only paths under that bundle are allowed.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skill": map[string]interface{}{
					"type":        "string",
					"description": "Logical skill name from the loaded skill registry.",
				},
				"script": map[string]interface{}{
					"type":        "string",
					"description": "Path to a .sh file relative to the skill bundle root (directory containing SKILL.md).",
				},
				"args": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "string",
					},
					"description": "Optional arguments passed to the script.",
				},
			},
			"required": []string{"skill", "script"},
		},
		Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
			var raw shellToolArgs
			if err := json.Unmarshal([]byte(argumentsJSON), &raw); err != nil {
				return "", fmt.Errorf("parse arguments: %w", err)
			}
			raw.Skill = strings.TrimSpace(raw.Skill)
			raw.Script = strings.TrimSpace(raw.Script)
			if raw.Skill == "" || raw.Script == "" {
				return "", fmt.Errorf("skill and script are required")
			}
			if reg == nil {
				return "", fmt.Errorf("skill registry is not available")
			}
			sp, err := reg.Get(raw.Skill)
			if err != nil {
				return "", fmt.Errorf("unknown skill %q: %w", raw.Skill, err)
			}
			root := sp.BundleRoot()
			if root == "" {
				return "", fmt.Errorf("skill %q has no bundle root", raw.Skill)
			}
			job := SkillJob{
				WorkingDir: root,
				Entry:      raw.Script,
				Args:       raw.Args,
			}
			return runner.Run(ctx, job)
		},
	}
}

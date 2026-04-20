package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"loopforge/pkg/model"
)

const LoadSkillToolName = "load_skill"

// LoadSkillTool registers a tool that loads an additional skill from the registry into the
// current run via appendLoaded (e.g. appends to LoopState.ExtraSkills). Default off at the Agent.
func LoadSkillTool(reg *SkillRegistry, appendLoaded func(SkillSpec)) *model.ToolInfo {
	return &model.ToolInfo{
		Name:        LoadSkillToolName,
		Description: "Loads a skill by logical name from the registry for the remainder of this run.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Logical skill name matching SKILL.md front matter.",
				},
			},
			"required": []string{"name"},
		},
		Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
			_ = ctx
			var payload struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
				return "", fmt.Errorf("parse arguments: %w", err)
			}
			n := strings.TrimSpace(payload.Name)
			if n == "" {
				return "", fmt.Errorf("name is required")
			}
			if reg == nil {
				return "", fmt.Errorf("skill registry is not available")
			}
			sp, err := reg.Get(n)
			if err != nil {
				return "", err
			}
			if appendLoaded != nil {
				appendLoaded(*sp)
			}
			return fmt.Sprintf("loaded skill %q", sp.Name), nil
		},
	}
}

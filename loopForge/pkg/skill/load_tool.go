package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/TasoHower/rei/loopForge/pkg/model"
	"github.com/TasoHower/rei/loopForge/pkg/tool/autoreg"
)

const LoadSkillToolName = "load_skill"

type loadSkillParams struct {
	Name string `json:"name" description:"Logical skill name matching SKILL.md front matter."`
}

// LoadSkillTool registers a tool that loads an additional skill from the registry into the
// current run via appendLoaded (e.g. appends to LoopState.ExtraSkills). Default off at the Agent.
func LoadSkillTool(reg *SkillRegistry, appendLoaded func(SkillSpec)) *model.ToolInfo {
	return autoreg.NewToolFromStruct(LoadSkillToolName,
		"Loads a skill by logical name from the registry for the remainder of this run.",
		func(ctx context.Context, p loadSkillParams) (string, error) {
			_ = ctx
			n := strings.TrimSpace(p.Name)
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
	)
}

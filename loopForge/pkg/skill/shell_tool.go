package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/TasoHower/rei/loopForge/pkg/model"
	"github.com/TasoHower/rei/loopForge/pkg/tool/autoreg"
)

const ShellToolName = "execute_shell_script"

type shellToolArgs struct {
	Skill  string   `json:"skill"               description:"Logical skill name from the loaded skill registry."`
	Script string   `json:"script"              description:"Path to a .sh file relative to the skill bundle root (directory containing SKILL.md)."`
	Args   []string `json:"args,omitempty"       description:"Optional arguments passed to the script."`
}

// ShellTool returns a ToolInfo that runs bundle scripts via ShellSkillJobRunner.
// Script lookup uses reg.Get(skillName); the registry must contain the skill definition.
func ShellTool(reg *SkillRegistry, runner SkillJobRunner) *model.ToolInfo {
	if runner == nil {
		runner = &ShellSkillJobRunner{}
	}
	return autoreg.NewToolFromStruct(ShellToolName,
		"Runs a shell script path relative to the named skill bundle. Only paths under that bundle are allowed.",
		func(ctx context.Context, p shellToolArgs) (string, error) {
			p.Skill = strings.TrimSpace(p.Skill)
			p.Script = strings.TrimSpace(p.Script)
			if p.Skill == "" || p.Script == "" {
				return "", fmt.Errorf("skill and script are required")
			}
			if reg == nil {
				return "", fmt.Errorf("skill registry is not available")
			}
			sp, err := reg.Get(p.Skill)
			if err != nil {
				return "", fmt.Errorf("unknown skill %q: %w", p.Skill, err)
			}
			root := sp.BundleRoot()
			if root == "" {
				return "", fmt.Errorf("skill %q has no bundle root", p.Skill)
			}
			job := SkillJob{
				WorkingDir: root,
				Entry:      p.Script,
				Args:       p.Args,
			}
			return runner.Run(ctx, job)
		},
	)
}

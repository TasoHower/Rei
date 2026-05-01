package agent

import (
	"fmt"
	"strings"

	"github.com/TasoHower/Rei/loopForge/pkg/skill"
)

func (a *Agent) resolveSkills() ([]skill.SkillSpec, error) {
	if len(a.SkillNames) == 0 {
		return nil, nil
	}
	if a.SkillRegistry == nil {
		return nil, fmt.Errorf("skill registry is required when skill names are set")
	}
	if err := skill.ValidateSkillBindings(a.SkillNames, a.SkillRegistry); err != nil {
		return nil, err
	}
	out := make([]skill.SkillSpec, 0, len(a.SkillNames))
	for _, name := range a.SkillNames {
		n := strings.TrimSpace(name)
		sp, err := a.SkillRegistry.Get(n)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", n, err)
		}
		out = append(out, *sp)
	}
	return out, nil
}

func mergeSkillLists(base []skill.SkillSpec, st *LoopState) []skill.SkillSpec {
	if st == nil || len(st.ExtraSkills) == 0 {
		return base
	}
	out := make([]skill.SkillSpec, 0, len(base)+len(st.ExtraSkills))
	out = append(out, base...)
	out = append(out, st.ExtraSkills...)
	return out
}

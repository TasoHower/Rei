package runner

import (
	"context"

	"github.com/TasoHower/Rei/loopForge/pkg/agent"
	"github.com/TasoHower/Rei/loopForge/pkg/skill"
)

// WithSkillRegistry sets a shared registry used when the entry agent has SkillNames
// but no SkillRegistry (same pattern as WithVarStore).
func WithSkillRegistry(reg *skill.SkillRegistry) RunOption {
	return func(r *Runner) {
		r.skillRegistry = reg
	}
}

// WithSkillPath registers directories to scan for SKILL.md on first use (memoized per Runner).
// Path order matches [skill.SkillRegistry.LoadFromPaths].
func WithSkillPath(paths ...string) RunOption {
	return func(r *Runner) {
		if len(paths) == 0 {
			r.skillPaths = nil
			return
		}
		r.skillPaths = append([]string(nil), paths...)
	}
}

func (r *Runner) resolveSkillRegistry(ctx context.Context) (*skill.SkillRegistry, error) {
	if r.skillRegistry != nil {
		return r.skillRegistry, nil
	}
	if len(r.skillPaths) == 0 {
		return nil, nil
	}
	r.pathRegMu.Lock()
	defer r.pathRegMu.Unlock()
	if r.pathLoadedReg != nil || r.pathLoadErr != nil {
		return r.pathLoadedReg, r.pathLoadErr
	}
	reg := skill.NewRegistry()
	err := reg.LoadFromPaths(ctx, r.skillPaths)
	r.pathLoadedReg = reg
	r.pathLoadErr = err
	return reg, err
}

func (r *Runner) prepareAgent(ctx context.Context, a *agent.Agent) error {
	if a == nil {
		return nil
	}
	if a.SkillRegistry != nil {
		return nil
	}
	reg, err := r.resolveSkillRegistry(ctx)
	if err != nil {
		return err
	}
	a.SkillRegistry = reg
	return nil
}

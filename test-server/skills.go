package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/TasoHower/Rei/loopForge/pkg/skill"
)

var (
	skillRegMu sync.RWMutex
	skillReg   *skill.SkillRegistry
)

func getSkillRegistry() *skill.SkillRegistry {
	skillRegMu.RLock()
	defer skillRegMu.RUnlock()
	return skillReg
}

func setSkillRegistry(reg *skill.SkillRegistry) {
	skillRegMu.Lock()
	defer skillRegMu.Unlock()
	skillReg = reg
}

// skillPathDirs returns directories to scan for SKILL.md. Includes LOOPFORGE_SKILL_PATH (OS list)
// and ./skills under the current working directory.
func skillPathDirs() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	if e := os.Getenv("LOOPFORGE_SKILL_PATH"); e != "" {
		for _, p := range filepath.SplitList(e) {
			add(p)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "skills"))
	}
	return out
}

func initSkillRegistry(ctx context.Context) {
	paths := skillPathDirs()
	if len(paths) == 0 {
		slog.Default().Warn("skill registry: no paths configured")
		return
	}
	reg := skill.NewRegistry()
	if err := reg.LoadFromPaths(ctx, paths); err != nil {
		slog.Default().Error("skill registry LoadFromPaths failed", "err", err)
		return
	}
	setSkillRegistry(reg)
	slog.Default().Info("skill registry ready",
		"paths", paths,
		"generation", reg.Generation(),
		"count", len(reg.List()),
	)
}

func reloadSkillRegistry(ctx context.Context) error {
	paths := skillPathDirs()
	if len(paths) == 0 {
		return nil
	}
	reg := skill.NewRegistry()
	if err := reg.LoadFromPaths(ctx, paths); err != nil {
		return err
	}
	setSkillRegistry(reg)
	return nil
}

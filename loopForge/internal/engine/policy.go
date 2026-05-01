package engine

import (
	"github.com/TasoHower/Rei/loopForge/internal/defaults"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/budget"
)

// defaultLoopPolicy implements LoopPolicy with configurable limits.
type defaultLoopPolicy struct {
	maxSteps            int
	maxSpawnDepth       int
	maxConcurrentSpawns int
	budgetCounter       *budget.BudgetCounter
}

// NewDefaultLoopPolicy creates a LoopPolicy with defaults from internal/defaults.
// budgetTokensMax and costUSDMax are passed through to NewBudgetCounter;
// zero values mean unlimited.
func NewDefaultLoopPolicy(budgetTokensMax int64, costUSDMax float64) LoopPolicy {
	return &defaultLoopPolicy{
		maxSteps:            defaults.MaxStepsDefault,
		maxSpawnDepth:       defaults.SpawnMaxDepthDefault,
		maxConcurrentSpawns: defaults.MaxConcurrentSpawnsDefault,
		budgetCounter:       budget.NewBudgetCounter(budgetTokensMax, costUSDMax),
	}
}

// NewCustomLoopPolicy creates a LoopPolicy with explicit overrides.
// Pass zero for any field to use the corresponding default.
func NewCustomLoopPolicy(maxSteps, maxSpawnDepth, maxConcurrentSpawns int, bc *budget.BudgetCounter) LoopPolicy {
	if maxSteps <= 0 {
		maxSteps = defaults.MaxStepsDefault
	}
	if maxSpawnDepth <= 0 {
		maxSpawnDepth = defaults.SpawnMaxDepthDefault
	}
	if maxConcurrentSpawns <= 0 {
		maxConcurrentSpawns = defaults.MaxConcurrentSpawnsDefault
	}
	if bc == nil {
		bc = budget.NewBudgetCounter(0, 0)
	}
	return &defaultLoopPolicy{
		maxSteps:            maxSteps,
		maxSpawnDepth:       maxSpawnDepth,
		maxConcurrentSpawns: maxConcurrentSpawns,
		budgetCounter:       bc,
	}
}

func (p *defaultLoopPolicy) AllowStep(state *RunState) bool {
	return state.Step < p.MaxSteps()
}

func (p *defaultLoopPolicy) AllowSpawn(parent *RunState, depth int) bool {
	if depth >= p.MaxSpawnDepth() {
		return false
	}
	if p.MaxConcurrentSpawns() > 0 && parent.Children != nil && parent.Children.ActiveCount() >= p.MaxConcurrentSpawns() {
		return false
	}
	return true
}

func (p *defaultLoopPolicy) MaxSteps() int                    { return p.maxSteps }
func (p *defaultLoopPolicy) MaxSpawnDepth() int               { return p.maxSpawnDepth }
func (p *defaultLoopPolicy) MaxConcurrentSpawns() int         { return p.maxConcurrentSpawns }
func (p *defaultLoopPolicy) BudgetCounter() *budget.BudgetCounter { return p.budgetCounter }

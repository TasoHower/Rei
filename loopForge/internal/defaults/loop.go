package defaults

// Loop and run limits (I-phase baseline; tune in later milestones).
const (
	MaxStepsDefault            = 64
	SpawnMaxDepthDefault       = 2
	MaxConcurrentSpawnsDefault = 4
)

// SpawnBudgetTokenSubtree is a soft cap for optional subtree token budgets (0 = disabled).
const SpawnBudgetTokenSubtree = int64(0)

// NetworkStrategyDefault applies when RunMode is network and no override is set.
const NetworkStrategyDefault = "sequential"

package defaults

// Loop and run limits (I-phase baseline; tune in later milestones).
const (
	MaxStepsDefault            = 64
	SpawnMaxDepthDefault       = 2
	MaxConcurrentSpawnsDefault = 4

	// SpawnBudgetTokenTreeDefault is the default tree-wide token budget (0 = unlimited).
	SpawnBudgetTokenTreeDefault = int64(0)
	// SpawnBudgetUSDTreeDefault is the default tree-wide cost budget in USD (0 = unlimited).
	SpawnBudgetUSDTreeDefault = float64(0)
)

// SpawnBudgetTokenSubtree is a soft cap for optional subtree token budgets (0 = disabled).
const SpawnBudgetTokenSubtree = int64(0)

// NetworkStrategyDefault applies when RunMode is network and no override is set.
const NetworkStrategyDefault = "sequential"

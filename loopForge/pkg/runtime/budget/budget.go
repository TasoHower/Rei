package budget

import "sync"

// BudgetCounter is a tree-wide shared budget counter for tokens and cost.
// All child runs under the same root share one instance.
type BudgetCounter struct {
	mu         sync.Mutex
	TokensUsed int64
	CostUSD    float64
	TokensMax  int64
	CostUSDMax float64
}

// NewBudgetCounter creates a counter with the given limits.
// Zero limits mean unlimited.
func NewBudgetCounter(tokensMax int64, costUSDMax float64) *BudgetCounter {
	return &BudgetCounter{
		TokensMax:  tokensMax,
		CostUSDMax: costUSDMax,
	}
}

// Allow checks whether the budget can accommodate the given additional tokens and cost.
// If the budget is exceeded, it returns false and does NOT consume.
func (b *BudgetCounter) Allow(tokens int64, costUSD float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.TokensMax > 0 && b.TokensUsed+tokens > b.TokensMax {
		return false
	}
	if b.CostUSDMax > 0 && b.CostUSD+costUSD > b.CostUSDMax {
		return false
	}
	return true
}

// Consume adds consumed tokens and cost to the budget.
// The caller should have called Allow first.
func (b *BudgetCounter) Consume(tokens int64, costUSD float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.TokensUsed += tokens
	b.CostUSD += costUSD
}

// Used returns the current usage without locking complications.
// It acquires the lock, so it's safe for concurrent use.
func (b *BudgetCounter) Used() (tokens int64, costUSD float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.TokensUsed, b.CostUSD
}

// Limits returns the configured limits.
func (b *BudgetCounter) Limits() (tokensMax int64, costUSDMax float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.TokensMax, b.CostUSDMax
}

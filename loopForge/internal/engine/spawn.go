package engine

import (
	"context"
	"fmt"

	"github.com/TasoHower/rei/loopForge/pkg/runtime/budget"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/exchange"
)

// Spawner creates and runs child agent loops under policy constraints.
type Spawner interface {
	Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error)
}

// LoopPolicy enforces max steps, spawn depth, concurrent spawn limits,
// and tree-wide budget caps per run.
type LoopPolicy interface {
	AllowStep(state *RunState) bool
	AllowSpawn(parent *RunState, depth int) bool
	MaxSteps() int
	MaxSpawnDepth() int
	// MaxConcurrentSpawns returns the maximum number of concurrent child runs.
	// Zero means unlimited.
	MaxConcurrentSpawns() int
	// BudgetCounter returns the tree-wide shared budget counter for the run.
	BudgetCounter() *budget.BudgetCounter
}

// engineSpawner wraps an inner Spawner (typically the agent's defaultSpawner)
// with RunState tracking: ChildRegistry registration, budget consumption,
// and lifecycle management.
//
// It implements the same signature as agent.Spawner so it can be injected
// directly into Agent.Spawner by the Runner.
type engineSpawner struct {
	inner    Spawner
	runState *RunState
}

// NewEngineSpawner creates a Spawner decorator that registers children in the
// given RunState's ChildRegistry and consumes metrics into the BudgetCounter.
// The inner spawner handles the actual child RunLoop lifecycle.
// The returned *engineSpawner satisfies both engine.Spawner and agent.Spawner
// (identical method signatures).
func NewEngineSpawner(inner Spawner, runState *RunState) *engineSpawner {
	return &engineSpawner{
		inner:    inner,
		runState: runState,
	}
}

// Spawn delegates to the inner spawner with child context tracking.
// After the child completes, it registers a SpawnHandle for observability
// and consumes token/cost metrics into the tree-wide BudgetCounter.
func (s *engineSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error) {
	// Create independent child context so we can cancel independently of the caller
	childCtx, childCancel := context.WithCancel(ctx)
	defer childCancel()

	// Delegate to the inner spawner (blocks until child RunLoop completes)
	result, err := s.inner.Spawn(childCtx, parent, spec)

	// Register a handle for post-completion state tracking
	if result != nil {
		handle := NewSpawnHandle(result.ChildRunRef, childCancel)
		if err != nil {
			handle.SetResult(result)
		}
		if s.runState != nil && s.runState.Children != nil {
			s.runState.Children.Register(result.ChildRunRef.RunID, handle)
			s.runState.Children.Unregister(result.ChildRunRef.RunID)
		}

		// Roll up child metrics into tree-wide budget counter (§3.8)
		if s.runState != nil && s.runState.BudgetCounter != nil && result.Metrics.TotalTokens > 0 {
			s.runState.BudgetCounter.Consume(result.Metrics.TotalTokens, 0)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("engine spawn: %w", err)
	}
	return result, nil
}

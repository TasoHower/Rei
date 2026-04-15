package engine

import (
	"context"

	"loopforge/pkg/runtime/exchange"
)

// Spawner creates and runs child agent loops under policy constraints.
type Spawner interface {
	Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error)
}

// LoopPolicy enforces max steps, spawn depth, and optional stop predicates per run.
type LoopPolicy interface {
	AllowStep(state *RunState) bool
	AllowSpawn(parent *RunState, depth int) bool
	MaxSteps() int
	MaxSpawnDepth() int
}

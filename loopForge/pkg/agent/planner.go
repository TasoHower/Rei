package agent

import (
	"context"

	"loopforge/pkg/runtime/action"
	"loopforge/pkg/runtime/exchange"
	"loopforge/pkg/runtime/request"
)

// Planner emits the next conceptual loop action, often backed by LLM tool routing
// (abstractions.md section 2.4). It is not a graph orchestrator.
type Planner interface {
	Plan(ctx context.Context, in PlannerInput) (*action.LoopAction, error)
}

// PlannerInput carries minimal context for one planner decision.
type PlannerInput struct {
	RunRef  exchange.RunRef
	Step    int
	Session request.RuntimeRequest
}

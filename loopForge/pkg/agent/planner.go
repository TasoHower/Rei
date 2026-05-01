package agent

import (
	"context"

	"github.com/TasoHower/rei/loopForge/pkg/runtime/action"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/exchange"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/request"
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

package engine

import "loopforge/pkg/agent"

// Planner emits the next conceptual loop action (see pkg/agent).
type Planner = agent.Planner

// PlannerInput carries minimal context for a planner decision.
type PlannerInput = agent.PlannerInput

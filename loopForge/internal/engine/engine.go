package engine

import (
	"loopforge/pkg/agent"
)

// Engine executes user-facing runs and owns orchestration boundaries.
// It satisfies agent.Agent (abstractions.md section 2.4).
type Engine interface {
	agent.Agent
}

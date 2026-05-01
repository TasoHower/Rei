package engine

import (
	"context"

	"github.com/TasoHower/rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/exchange"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/request"
)

// AgentRunner runs one agent loop for a run reference (root or child).
// RunLoop returns a receive-only channel of RuntimeEvent for streaming model output and lifecycle signals to the client
// (e.g. EventAnswer with AnswerPayload.Delta for assistant text). The channel is closed after a terminal
// EventQueryEnd (QueryEndPayload.Outcome) or EventError. On non-nil error from RunLoop, the channel is nil.
type AgentRunner interface {
	RunLoop(ctx context.Context, ref exchange.RunRef, req *request.RuntimeRequest) (<-chan event.RuntimeEvent, error)
}

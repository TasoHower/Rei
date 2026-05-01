package agent

import (
	"context"

	"github.com/TasoHower/Rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/request"
)

// Runnable is the minimal runnable agent contract (abstractions.md section 2.4).
// Run starts execution and returns a read-only channel that streams RuntimeEvents.
// The channel is closed when the run finishes; the last meaningful event is
// EventQueryEnd whose QueryEndPayload carries the final RuntimeOutcome.
// Errors are delivered as EventError events, not as Go error returns.
type Runnable interface {
	Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent
}

package engine

import (
	"context"

	"github.com/TasoHower/rei/loopForge/pkg/runtime/event"
)

// RuntimeEventSink receives streaming runtime events (SSE, logs, dev UI).
type RuntimeEventSink interface {
	Emit(ctx context.Context, ev event.RuntimeEvent) error
}

package engine

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/TasoHower/rei/loopForge/pkg/runtime/exchange"
	"github.com/TasoHower/rei/loopForge/pkg/runtime/outcome"
)

// ChildState describes the current lifecycle phase of a child Agent run.
type ChildState int32

const (
	ChildStateRunning   ChildState = iota // child RunLoop is executing
	ChildStateComplete                     // child RunLoop finished normally
	ChildStateFailed                       // child RunLoop finished with error
	ChildStateCancelled                    // parent Terminate cancelled the child
)

// SpawnHandle is the parent-side control handle for a child Agent.
// The parent may cancel the child via Shutdown, query its lifecycle state,
// or block on Result for the final outcome.
//
// Shutdown and SetResult are idempotent: whoever arrives first via closeOnce
// determines the terminal state.
type SpawnHandle struct {
	Ref   exchange.RunRef
	state atomic.Int32 // ChildState

	cancel context.CancelFunc
	done   chan struct{}   // closed when goroutine exits
	result atomic.Value    // *exchange.SpawnResult (available after terminal state)

	closeOnce sync.Once
}

// NewSpawnHandle creates a handle in Running state.
func NewSpawnHandle(ref exchange.RunRef, cancel context.CancelFunc) *SpawnHandle {
	h := &SpawnHandle{
		Ref:    ref,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	h.state.Store(int32(ChildStateRunning))
	return h
}

// Shutdown cancels the child context without waiting for the goroutine to exit.
// The child may take some time to observe ctx.Done and return.
func (h *SpawnHandle) Shutdown(reason outcome.TerminationReason) {
	h.closeOnce.Do(func() {
		h.state.Store(int32(ChildStateCancelled))
		h.cancel()
		close(h.done)
	})
}

// Done returns a channel that is closed when the child goroutine exits.
func (h *SpawnHandle) Done() <-chan struct{} { return h.done }

// State returns the current lifecycle state (thread-safe).
func (h *SpawnHandle) State() ChildState {
	return ChildState(h.state.Load())
}

// SetResult stores the final SpawnResult and transitions to Complete or Failed.
// Called by the spawn goroutine on exit. Idempotent with Shutdown.
func (h *SpawnHandle) SetResult(res *exchange.SpawnResult) {
	h.result.Store(res)
	if h.State() == ChildStateRunning {
		s := ChildStateComplete
		if res.Status == exchange.SpawnFailed || res.Status == exchange.SpawnRejected {
			s = ChildStateFailed
		}
		h.state.Store(int32(s))
	}
	h.closeOnce.Do(func() {
		close(h.done)
	})
}

// Result blocks until the child is done, then returns the final result.
func (h *SpawnHandle) Result(ctx context.Context) (*exchange.SpawnResult, error) {
	select {
	case <-h.done:
		if v := h.result.Load(); v != nil {
			return v.(*exchange.SpawnResult), nil
		}
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

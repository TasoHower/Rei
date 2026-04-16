// Package iface defines chat model contracts; data types live in pkg/model/types.
package iface

import (
	"context"

	"loopforge/pkg/model/types"
)

// BaseChatModel is the narrow chat surface (compare: eino BaseChatModel: Generate + Stream).
type BaseChatModel interface {
	Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error)
	Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error)
}

// ToolCallingChatModel extends BaseChatModel with immutable tool binding (compare: eino ToolCallingChatModel).
//
// Concurrency: implementations must be safe for concurrent use. Generate,
// Stream, and WithTools may be called from multiple goroutines simultaneously
// (e.g. when the Orchestrator runs the same agent graph for multiple sessions).
// WithTools must return a new instance without mutating the receiver.
type ToolCallingChatModel interface {
	BaseChatModel

	// WithTools returns a new instance with tools bound; the receiver must not be mutated.
	WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error)
}

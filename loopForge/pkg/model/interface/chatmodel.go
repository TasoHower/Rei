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
type ToolCallingChatModel interface {
	BaseChatModel

	// WithTools returns a new instance with tools bound; the receiver must not be mutated.
	WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error)
}

package engine

import (
	"context"

	"loopforge/pkg/runtime/tool"
)

// ToolHandler is a single callable tool surface (local, MCP-backed, or builtin).
type ToolHandler interface {
	Descriptor() tool.ToolDescriptor
	Call(ctx context.Context, call tool.ToolCall) (*tool.ToolResult, error)
}

// ToolRegistry resolves tool names to handlers and lists model-facing descriptors.
type ToolRegistry interface {
	Register(key tool.ToolRegistryKey, h ToolHandler) error
	Descriptors() []tool.ToolDescriptor
	Call(ctx context.Context, name string, call tool.ToolCall) (*tool.ToolResult, error)
}

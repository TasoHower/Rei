package tool

import (
	"context"
	"fmt"

	lferrors "loopforge/pkg/errors"
)

// MapToolExecutor dispatches by tool name to handlers (argumentsJSON is the model payload).
type MapToolExecutor map[string]func(ctx context.Context, argumentsJSON string) (string, error)

// Execute implements ToolExecutor.
func (m MapToolExecutor) Execute(ctx context.Context, name string, argumentsJSON string) (string, error) {
	if m == nil {
		return "", fmt.Errorf("%w", lferrors.ErrExecutorNil)
	}
	fn, ok := m[name]
	if !ok || fn == nil {
		return "", fmt.Errorf("%w: %q", lferrors.ErrUnknownTool, name)
	}
	return fn(ctx, argumentsJSON)
}

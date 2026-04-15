package types

import "context"

// ToolCallHandler runs when the model invokes a function tool; argumentsJSON is the model-produced JSON payload.
type ToolCallHandler func(ctx context.Context, argumentsJSON string) (string, error)

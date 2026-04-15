package tool

import "context"

// ToolExecutor runs a single function-calling invocation from the model (argumentsJSON is the raw JSON payload).
type ToolExecutor interface {
	Execute(ctx context.Context, name string, argumentsJSON string) (content string, err error)
}

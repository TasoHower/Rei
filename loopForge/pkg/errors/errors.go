// Package errors defines all sentinel errors returned by loopForge.
//
// SDK consumers should use [errors.Is] or [errors.As] to identify specific
// failure conditions:
//
//	if errors.Is(err, lferrors.ErrInvalidConfig) { ... }
//	var te *lferrors.ToolError
//	if errors.As(err, &te) { ... }
package errors

import "errors"

// ---------------------------------------------------------------------------
// Configuration / setup errors
// ---------------------------------------------------------------------------

// ErrInvalidConfig is returned when an Agent or Runner is misconfigured
// (e.g. ChatModel is nil, tool binding validation failed).
var ErrInvalidConfig = errors.New("loopforge: invalid configuration")

// ErrToolBind is returned when WithTools fails during the pre-loop setup phase.
var ErrToolBind = errors.New("loopforge: tool bind failed")

// ErrInvalidRequest is returned when a nil or malformed RuntimeRequest is passed.
var ErrInvalidRequest = errors.New("loopforge: invalid request")

// ---------------------------------------------------------------------------
// Runtime / execution errors
// ---------------------------------------------------------------------------

// ErrGenerate is returned when the LLM call (stream or generate) fails.
var ErrGenerate = errors.New("loopforge: LLM generate failed")

// ErrToolExec is returned when a tool call execution fails inside the agent loop.
var ErrToolExec = errors.New("loopforge: tool execution failed")

// ErrMaxTransfers is returned when the Runner's transfer loop exceeds the
// configured maximum number of agent-to-agent handoffs.
var ErrMaxTransfers = errors.New("loopforge: max agent transfers exceeded")

// ErrInvalidTransfer is returned when a transfer targets an agent name that
// is not registered in the current agent's handoffs list.
var ErrInvalidTransfer = errors.New("loopforge: invalid transfer target")

// ErrStreamEmpty is returned when a model stream ends without producing any
// text content or a final response object.
var ErrStreamEmpty = errors.New("loopforge: stream ended without content or final response")

// ---------------------------------------------------------------------------
// Tool-layer errors
// ---------------------------------------------------------------------------

// ErrNoHandler is returned by [tool.Invoke] when no handler can be found for
// a tool call (no ToolInfo.Handle, no ToolCallPart.Handle, no ToolExecutor).
var ErrNoHandler = errors.New("loopforge: no handler for tool")

// ErrUnknownTool is returned by [tool.MapToolExecutor] when the requested tool
// name is not registered in the map.
var ErrUnknownTool = errors.New("loopforge: unknown tool")

// ErrExecutorNil is returned by [tool.MapToolExecutor] when the executor itself
// is nil.
var ErrExecutorNil = errors.New("loopforge: tool executor is nil")

// ErrUnsupportedRole is returned when a message with an unrecognised role is
// encountered during model adapter conversion.
var ErrUnsupportedRole = errors.New("loopforge: unsupported message role")

// ---------------------------------------------------------------------------
// Variable-store errors
// ---------------------------------------------------------------------------

// ErrNotPresent is returned by [variable.GetRequired] when the requested key is
// missing from the store or its stored value cannot be assigned to the target
// type.
var ErrNotPresent = errors.New("loopforge: variable key missing or type mismatch")

// ErrReadOnly is returned by [variable.VarStore.AgentSet] when the LLM attempts
// to write a key that carries the const_ prefix.
var ErrReadOnly = errors.New("loopforge: variable is read-only (const_ prefix)")

// ErrNoStore is returned by the var_set tool handler when no VarStore is
// available in either the closure or the context.
var ErrNoStore = errors.New("loopforge: no variable store in context")

// ---------------------------------------------------------------------------
// Debug (see loopforge/debug)
// ---------------------------------------------------------------------------

// ErrDebugNilConnection is returned by debug.Conn methods when the receiver
// or underlying session is nil. The module root package loopforge re-exports
// this sentinel as loopforge.ErrDebugNilConnection.
var ErrDebugNilConnection = errors.New("loopforge: debug connection is nil")

// ---------------------------------------------------------------------------
// Structured error types (use errors.As to extract fields)
// ---------------------------------------------------------------------------

// SetupError is emitted as an event ErrorPayload and wraps one of
// [ErrInvalidConfig] or [ErrToolBind]. It carries the original error message
// from the underlying failure.
type SetupError struct {
	// Code is the short machine-readable code ("invalid_config" | "tool_bind").
	Code string
	// Msg is the human-readable description of what went wrong.
	Msg string
	// Unwrap returns the sentinel that best matches Code.
	err error
}

func (e *SetupError) Error() string { return e.Code + ": " + e.Msg }
func (e *SetupError) Unwrap() error { return e.err }

// NewSetupError creates a SetupError and wires the correct sentinel for Is/As.
func NewSetupError(code, msg string) *SetupError {
	var sentinel error
	switch code {
	case "tool_bind":
		sentinel = ErrToolBind
	default:
		sentinel = ErrInvalidConfig
	}
	return &SetupError{Code: code, Msg: msg, err: sentinel}
}

// ToolError wraps a tool execution failure and carries the tool name.
// Unwrap returns [ErrToolExec] so errors.Is(err, ErrToolExec) works.
type ToolError struct {
	// Name is the tool that failed.
	Name string
	// Cause is the underlying error returned by the handler.
	Cause error
}

func (e *ToolError) Error() string { return "tool " + e.Name + ": " + e.Cause.Error() }
func (e *ToolError) Unwrap() error { return e.Cause }

// Is reports true for both [ErrToolExec] (category sentinel) and the Cause.
func (e *ToolError) Is(target error) bool {
	return target == ErrToolExec
}

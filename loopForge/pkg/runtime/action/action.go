package action

// LoopActionType is one conceptual step in the agent loop (planner output).
type LoopActionType string

const (
	LoopActionTool       LoopActionType = "tool"
	LoopActionSpawnAgent LoopActionType = "spawn_agent"
	LoopActionFinish     LoopActionType = "finish"
)

// LoopAction is one planner decision for a single iteration (test-friendly contract).
type LoopAction struct {
	Type         LoopActionType
	Tool         string
	Input        string
	Prompt       string
	AllowedTools []string
	Result       string
}

package events

import (
	"context"

	"github.com/tasohower/rei/core"
)

// 任务状态。
const (
	TaskRunning     = "running"
	TaskCompleted   = "completed"
	TaskFailed      = "failed"
	TaskInterrupted = "interrupted"
	TaskAbandoned   = "abandoned"
)

// TaskStarted 在后台任务登记之后、子运行开始之前同步发出：
// 因此 task_started 一定先于该子运行的 agent_start。
type TaskStarted struct {
	Base
	TaskID string `json:"task_id"`
	Name   string `json:"name"`
	Prompt string `json:"prompt,omitempty"`
	Async  bool   `json:"async"`
}

func (e *TaskStarted) Type() EventType { return TaskStartedType }

// TaskMessage 是后台任务的一次输入（例如 agent_send 的新指令）。
type TaskMessage struct {
	Base
	TaskID string `json:"task_id"`
	Text   string `json:"text"`
}

func (e *TaskMessage) Type() EventType { return TaskMessageType }

// TaskEnd 是后台任务的终态。Result 已经过长度截断，Usage 是该任务自身的用量。
type TaskEnd struct {
	Base
	TaskID string     `json:"task_id"`
	Status string     `json:"status"`
	Result string     `json:"result,omitempty"`
	Err    string     `json:"err,omitempty"`
	Usage  core.Usage `json:"usage"`
}

func (e *TaskEnd) Type() EventType { return TaskEndType }

// TaskResultDelivered 表示任务结果已被注入主会话上下文。
type TaskResultDelivered struct {
	Base
	TaskID string `json:"task_id"`
	Result string `json:"result,omitempty"`
	Status string `json:"status"`
}

func (e *TaskResultDelivered) Type() EventType { return TaskResultDeliveredType }

// ---------------------------------------------------------------- 运行上下文

// RunInfo 是随 ctx 传递的运行身份。工具（尤其是子 agent 工具）需要它来：
//  1. 生成带 parent_run_id 的子事件；
//  2. 判断还能不能继续下钻（Depth 与 MaxDepth）。
type RunInfo struct {
	RunID       string
	ParentRunID string
	Depth       int
	// Emit 是当前运行的处理器。子 agent 复用同一个 handler 就能自然接入事件流。
	Emit EventHandler
	// Asker / Approver 由当前运行注入，供工具（如 ask_user）直接使用。
	Asker    Asker
	Approver Approver
	// Deliver 把后台任务终态投递给「发起它的那个运行」。
	// 为 nil 时退回 Registry 的全局回调（即投递给主会话）。
	Deliver func(TaskEnd)
}

type runInfoKey struct{}

// WithRunInfo 把运行身份放进 ctx。
func WithRunInfo(ctx context.Context, info RunInfo) context.Context {
	return context.WithValue(ctx, runInfoKey{}, info)
}

// RunInfoFrom 取出运行身份。顶层工具调用之外（例如单测直接调 Call）拿不到值是正常的。
func RunInfoFrom(ctx context.Context) (RunInfo, bool) {
	info, ok := ctx.Value(runInfoKey{}).(RunInfo)
	return info, ok
}

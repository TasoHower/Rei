// Package events 是一次运行的公开契约。
//
// 每一步都是事件：模型调用、正文增量、工具执行、审批、压缩、预算超限与会话生命周期。
// 宿主嵌入 Rei 的方式只有一个 —— 给 Session.Run / AgentLoop.RunStream 传一个
// EventHandler；没有回调迷宫，也没有隐式全局状态。
package events

import (
	"context"
	"time"

	"github.com/tasohower/rei/core"
)

// EventType 是运行事件的封闭集合。
type EventType string

const (
	AgentStartType EventType = "agent_start"
	AgentEndType   EventType = "agent_end"

	LLMStartType       EventType = "llm_start"
	LLMEndType         EventType = "llm_end"
	LLMErrorType       EventType = "llm_error"
	ContentChunkType   EventType = "content_chunk"
	ReasoningChunkType EventType = "reasoning_chunk"

	ToolRunStartType        EventType = "tool_run_start"
	ToolRunEndType          EventType = "tool_run_end"
	ToolApprovalType        EventType = "tool_approval_requested"
	ToolApprovalDoneType    EventType = "tool_approval_resolved"
	GoalAlignmentRemindType EventType = "goal_alignment_reminder"

	CompressStartType EventType = "compress_start"
	CompressEndType   EventType = "compress_end"

	AskUserQuestionType EventType = "ask_user_question"

	SessionClosedType         EventType = "session_closed"
	SessionBudgetExceededType EventType = "session_budget_exceeded"
	SessionRunErrorType       EventType = "session_run_error"
	SessionPersistErrorType   EventType = "session_persist_error"
	UserInputsConsumedType    EventType = "user_inputs_consumed"

	TaskStartedType         EventType = "task_started"
	TaskMessageType         EventType = "task_message"
	TaskEndType             EventType = "task_end"
	TaskResultDeliveredType EventType = "task_result_delivered"

	PlanGeneratedType     EventType = "plan_generated"
	PlanUpdatedType       EventType = "plan_updated"
	PlanStepStartedType   EventType = "plan_step_started"
	PlanStepCompletedType EventType = "plan_step_completed"
)

// Event 是处理器收到的唯一类型。所有实现都内嵌 Base，因此事件自带上下文信封。
//
// 访问器命名为 Envelope 而不是 Base：内嵌字段的类型名本身就是 Base，
// 同名方法会和字段冲突。
type Event interface {
	Type() EventType
	Envelope() *Base
}

// EventHandler 消费事件。它必须是非阻塞的：Rei 在运行 goroutine 上同步调用。
type EventHandler func(ctx context.Context, e Event)

// Base 是所有事件共享的信封：哪一次运行、父运行、运行内序号与时间。
//
// ParentRunID 与 Depth 是事件树的唯一来源：子 agent / 后台任务的事件都带着
// 发起方的 run_id，因此投影端不需要额外的父子表就能还原调用树。
type Base struct {
	RunID       string    `json:"run_id,omitempty"`
	ParentRunID string    `json:"parent_run_id,omitempty"`
	RequestID   string    `json:"request_id,omitempty"`
	Index       int       `json:"index"`
	Depth       int       `json:"depth,omitempty"`
	Time        time.Time `json:"time"`
}

// Envelope 让所有内嵌 Base 的事件都满足信封访问器。
func (b *Base) Envelope() *Base { return b }

// Emit 在处理器非 nil 时投递事件。
func Emit(ctx context.Context, h EventHandler, e Event) {
	if h != nil {
		h(ctx, e)
	}
}

// ---------------------------------------------------------------- 运行级事件

// AgentStart 开启一次运行。
type AgentStart struct {
	Base
	Model        string `json:"model"`
	MessageCount int    `json:"message_count"`
}

func (e *AgentStart) Type() EventType { return AgentStartType }

// AgentEnd 结束一次运行。FinishReason 取 core.Finish* 常量。
type AgentEnd struct {
	Base
	FinishReason string     `json:"finish_reason"`
	FinalText    string     `json:"final_text,omitempty"`
	Usage        core.Usage `json:"usage"`
	DurationMS   int64      `json:"duration_ms"`
}

func (e *AgentEnd) Type() EventType { return AgentEndType }

// GoalAlignmentReminder 在迭代护栏触发时发出。
type GoalAlignmentReminder struct {
	Base
	Iteration int `json:"iteration"`
}

func (e *GoalAlignmentReminder) Type() EventType { return GoalAlignmentRemindType }

// ---------------------------------------------------------------- 模型级事件

// LLMStart 开启一次模型调用。
type LLMStart struct {
	Base
	Model        string `json:"model"`
	MessageCount int    `json:"message_count"`
}

func (e *LLMStart) Type() EventType { return LLMStartType }

// LLMEnd 用用量与结束原因关闭一次模型调用。
type LLMEnd struct {
	Base
	Model        string     `json:"model"`
	FinishReason string     `json:"finish_reason"`
	Usage        core.Usage `json:"usage"`
	DurationMS   int64      `json:"duration_ms"`
	// RequestID 是上游响应 id（OpenAI 的 id / Anthropic 的 message.id），
	// trace 详情可以按它回查这次的原始事件。
	RequestID string `json:"request_id,omitempty"`
}

func (e *LLMEnd) Type() EventType { return LLMEndType }

// LLMError 报告一次失败的尝试。Retryable 告诉宿主后面是否还会重试。
type LLMError struct {
	Base
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Attempt   int    `json:"attempt"`
}

func (e *LLMError) Type() EventType { return LLMErrorType }

// ContentChunk 是正文增量。增量永远不落盘。
type ContentChunk struct {
	Base
	Text string `json:"text"`
}

func (e *ContentChunk) Type() EventType { return ContentChunkType }

// ReasoningChunk 是思考增量。
type ReasoningChunk struct {
	Base
	Text string `json:"text"`
}

func (e *ReasoningChunk) Type() EventType { return ReasoningChunkType }

// ---------------------------------------------------------------- 工具级事件

// ToolRunStart 开启一次工具执行。
type ToolRunStart struct {
	Base
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (e *ToolRunStart) Type() EventType { return ToolRunStartType }

// ToolRunEnd 关闭一次工具执行。
type ToolRunEnd struct {
	Base
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated,omitempty"`
	// Usage 是工具内部消耗的用量（子 agent、内部摘要），已并入运行账本。
	Usage      core.Usage `json:"usage,omitempty"`
	DurationMS int64      `json:"duration_ms"`
}

func (e *ToolRunEnd) Type() EventType { return ToolRunEndType }

// ToolApprovalRequested 通过 Approver 询问人类某个工具调用是否放行。
// 运行会阻塞，直到审批被解决、超时或被取消。
type ToolApprovalRequested struct {
	Base
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Reason    string `json:"reason,omitempty"`
}

func (e *ToolApprovalRequested) Type() EventType { return ToolApprovalType }

// ToolApprovalResolved 报告审批结果。
type ToolApprovalResolved struct {
	Base
	CallID   string `json:"call_id"`
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

func (e *ToolApprovalResolved) Type() EventType { return ToolApprovalDoneType }

// ---------------------------------------------------------------- 上下文事件

// CompressStart 宣告一次压缩。
type CompressStart struct {
	Base
	MessageCount int    `json:"message_count"`
	Reason       string `json:"reason"`
}

func (e *CompressStart) Type() EventType { return CompressStartType }

// CompressEnd 报告压缩结果。Err 非空表示压缩失败、原上下文被保留。
type CompressEnd struct {
	Base
	MessagesBefore int        `json:"messages_before"`
	MessagesAfter  int        `json:"messages_after"`
	Usage          core.Usage `json:"usage"`
	Err            string     `json:"err,omitempty"`
}

func (e *CompressEnd) Type() EventType { return CompressEndType }

// Option 是结构化提问里的一个选项。
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Question 是给人类的一个结构化问题。
type Question struct {
	ID          string   `json:"id"`
	Header      string   `json:"header"`
	Question    string   `json:"question"`
	Options     []Option `json:"options,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
}

// AskUserQuestion 在工具需要人类输入时发出。
type AskUserQuestion struct {
	Base
	CallID    string     `json:"call_id"`
	Questions []Question `json:"questions"`
}

func (e *AskUserQuestion) Type() EventType { return AskUserQuestionType }

// ---------------------------------------------------------------- 会话事件

// SessionClosed 在优雅关闭时发出。
type SessionClosed struct {
	Base
	Reason string `json:"reason,omitempty"`
}

func (e *SessionClosed) Type() EventType { return SessionClosedType }

// SessionBudgetExceeded 表示成本预算拒绝了下一轮。
type SessionBudgetExceeded struct {
	Base
	BudgetMicroUSD int64 `json:"budget_micro_usd"`
	UsedMicroUSD   int64 `json:"used_micro_usd"`
}

func (e *SessionBudgetExceeded) Type() EventType { return SessionBudgetExceededType }

// SessionRunError 表示一次运行以错误结束。
type SessionRunError struct {
	Base
	Message string `json:"message"`
}

func (e *SessionRunError) Type() EventType { return SessionRunErrorType }

// SessionPersistError 表示检查点写入失败。失败绝不静默：宿主能看到，
// 并自行决定继续还是停下。
type SessionPersistError struct {
	Base
	Message string `json:"message"`
}

func (e *SessionPersistError) Type() EventType { return SessionPersistErrorType }

// UserInputsConsumed 报告本次运行消费了哪些排队输入。
type UserInputsConsumed struct {
	Base
	Count int `json:"count"`
}

func (e *UserInputsConsumed) Type() EventType { return UserInputsConsumedType }

// ---------------------------------------------------------------- 审批契约

// ApprovalRequest 是 Approver 收到的内容。
type ApprovalRequest struct {
	CallID    string `json:"call_id"`
	ToolName  string `json:"name"`
	Arguments string `json:"arguments"`
	Reason    string `json:"reason,omitempty"`
}

// Decision 是人类对 ApprovalRequest 的回答。Arguments 为 nil 表示沿用原参数。
type Decision struct {
	Approved  bool
	Reason    string
	Arguments *string
}

// Approver 决定某个工具调用是否放行。实现应当阻塞到人类应答或 ctx 结束。
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (Decision, error)
}

// Asker 回答结构化提问。返回空切片表示人类放弃了这次提问。
type Asker interface {
	Ask(ctx context.Context, questions []Question) ([]string, error)
}

// ApprovalPolicy 决定哪些工具调用需要人类审批。
//
// 与 Approver 的分工：Approver 负责「怎么问」，Policy 负责「问不问」。
// 没有 Policy 时，挂了 Approver 就等价于「每个调用都问」。
type ApprovalPolicy interface {
	RequiresApproval(toolName string, readOnly bool) bool
}

// PolicyFunc 让函数直接实现 ApprovalPolicy。
type PolicyFunc func(toolName string, readOnly bool) bool

// RequiresApproval 实现 ApprovalPolicy。
func (f PolicyFunc) RequiresApproval(toolName string, readOnly bool) bool {
	return f(toolName, readOnly)
}

// ReadOnlyPolicy 放行只读工具，其余都要审批。
func ReadOnlyPolicy() ApprovalPolicy {
	return PolicyFunc(func(_ string, readOnly bool) bool { return !readOnly })
}

// ToolsPolicy 只对指定工具要求审批。
func ToolsPolicy(names ...string) ApprovalPolicy {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return PolicyFunc(func(name string, _ bool) bool { return set[name] })
}

package exchange

import (
	"time"

	"github.com/TasoHower/Rei/loopForge/pkg/runtime/event"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/outcome"
	"github.com/TasoHower/Rei/loopForge/pkg/runtime/tool"
)

// Lifecycle 子运行（child run）的生命周期模式。当前仅实现 [LifecycleEphemeral]；
// [LifecycleLinger] 预留给后续版本。
type Lifecycle string

const (
	// LifecycleEphemeral 短暂子运行：子运行结束即释放相关资源（当前实现仅支持此模式）
	LifecycleEphemeral Lifecycle = "ephemeral"
	// LifecycleLinger 滞留子 run：子运行结束后仍保留/挂起（未实现，占位）
	LifecycleLinger Lifecycle = "linger"
)

// MemoryDigest 从父运行向子运行透传的紧凑短期记忆摘要。
type MemoryDigest struct {
	// Summary 供子运行理解上下文的短文本摘要
	Summary string
	// KVs 键值对形式的附加上下文
	KVs map[string]string
}

// RunRef 在引擎中标识一次 agent 循环（loop）执行。
type RunRef struct {
	// RunID 本次运行的唯一标识
	RunID string
	// AgentRole 承担该次运行的角色名
	AgentRole string
	// ParentRunID 父运行的 RunID，根运行通常为空
	ParentRunID string
	// Depth 从根运行起算的派生深度（0 表示根或顶层）
	Depth int
}

// NetworkTurn 描述一个 agent 槽位（slot）的输出，用于综合（synthesis）或流入下一槽位。
type NetworkTurn struct {
	// SlotName 槽位名称
	SlotName string
	// RunRef 产生本回合输出的运行引用
	RunRef RunRef
	// InputHint 本回合输入侧提示/约束说明
	InputHint string
	// OutputText 模型产出的主文本
	OutputText string
	// ToolCalls 本回合内的工具调用列表
	ToolCalls []tool.ToolCall
}

// SpawnSpec 父运行向子运行传递的跨 agent 派生（spawn）契约。
type SpawnSpec struct {
	// Task 交给子运行执行的任务说明
	Task string
	// SystemAddendum 在系统侧追加的说明（与主系统提示合并或附加）
	SystemAddendum string
	// SkillIDs 子运行可加载的技能 ID 列表
	SkillIDs []string
	// ToolAllowlist 子运行允许调用的工具名白名单
	ToolAllowlist []string
	// LoopOverrides 子运行循环的覆盖参数
	LoopOverrides LoopOverrides
	// ModelOverride 非空时覆盖子运行使用的模型
	ModelOverride string

	// MemoryDigest 向子运行透传的父侧短期记忆（可选）
	MemoryDigest *MemoryDigest
	// Lifecycle 子运行生命周期模式
	Lifecycle Lifecycle
	// AllowChildSpawn 是否允许子运行再派生子运行
	AllowChildSpawn bool
	// OutputCh, when non-nil, receives child RuntimeEvent values forwarded to the parent
	// event stream for real-time observability (e.g. tool calls, streaming text).
	OutputCh chan<- *event.RuntimeEvent `json:"-"`
}

// LoopOverrides 对单个子运行循环的调参（覆盖默认配置）。
type LoopOverrides struct {
	// MaxSteps 非空时覆盖单轮循环最大步数
	MaxSteps *int
	// Timeout 非空时覆盖子运行总超时
	Timeout *time.Duration
}

// ChildToolCall 记录子运行中一次工具调用的名称、参数和结果。
type ChildToolCall struct {
	// Name 工具名称
	Name string `json:"name"`
	// Arguments 调用工具时传入的 JSON 参数
	Arguments string `json:"arguments"`
	// Output 工具返回的 JSON 结果文本
	Output string `json:"output,omitempty"`
	// IsError 工具是否返回错误
	IsError bool `json:"is_error"`
}

// SpawnResult 作为 spawn 工具的结果内容返回给父侧 agent。
type SpawnResult struct {
	// ChildRunRef 子运行的运行引用
	ChildRunRef RunRef
	// Status 子运行完成状态（见 [SpawnStatus]）
	Status SpawnStatus
	// FinalText 子运行结束时的最终文本（成功或部分场景下的总结）
	FinalText string
	// Error 失败时的结构化错误信息（无失败时为 nil）
	Error *SpawnError
	// Metrics 子运行的运行指标（如 token 等，由 engine 填写）
	Metrics outcome.RunMetrics
	// ChildToolCalls 子运行过程中的工具调用记录列表（按调用顺序排列）
	ChildToolCalls []ChildToolCall `json:"child_tool_calls,omitempty"`
}

// SpawnStatus 子运行结束时的状态。
type SpawnStatus string

const (
	// SpawnCompleted 子运行正常完成
	SpawnCompleted SpawnStatus = "completed"
	// SpawnFailed 子运行因错误失败
	SpawnFailed SpawnStatus = "failed"
	// SpawnRejected 子运行被拒绝（如策略、限额未通过）
	SpawnRejected SpawnStatus = "rejected"
)

// SpawnError 在 spawn 结果中承载的结构化失败信息。
type SpawnError struct {
	// Code 稳定可分类的错误码（供程序与日志使用）
	Code string
	// Message 供人阅读的错误说明
	Message string
}

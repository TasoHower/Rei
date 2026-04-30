# loopForge 关键抽象结构

> 本文档描述仓库 **当前实现** 中的契约与分层，并给出 **与源码一致的代码与结构示例**（代码块内为英文标识符与注释）。  
> 三层引擎架构设计见 `doc/decision/engine-layering.md`（ADR）；事件语义设计见 `doc/decision/event-semantics.md`（ADR）；spawn 隔离机制见 `doc/decision/spawn-isolation.md`（ADR）。

---

## 0. 总览：三层引擎架构

```
                    +------------------+
                    |  接入层 / CLI /  |
                    |  test-server     |
                    +--------+---------+
                             | *request.RuntimeRequest
                             v
              ╔═════════════╧══════════════╗
              ║   pkg/runner（入口装配层）    ║
              ║   Runner.Run()              ║
              ║   NewRunner() → Runnable    ║
              ║   装配 model 适配、spawn 配置 ║
              ╚═════════════╤══════════════╝
                            │ 注入 engineSpawner
              ╔═════════════╧══════════════╗
              ║   internal/engine（引擎核心层）║
              ║   RunState / ChildRegistry  ║
              ║   engineSpawner             ║
              ║   BudgetCounter             ║
              ║   SpawnHandle               ║
              ╚═════════════╤══════════════╝
                spawn ref   │  handoff ref
                    +-------++-------+
                    |                |
                    v                v
              ╔═════╧════════╗ ╔═════╧══════════╗
              ║  pkg/agent   ║ ║  bindModel:    ║
              ║  (纯净 loop层)║ ║  ToolInfos +   ║
              ║  Agent       ║ ║  mcpToolInfos  ║
              ║  RunLoop     ║ ║  + ExtraTools  ║
              ║  Spawner(if) ║ ╚════╤═══════════╝
              ╚═════╤════════╝     │ MCPServerProfiles
                    │              │ → mcp.BootstrapToolInfos
                    v              v
              ╔═════╧════════╗ ╔═══╧══════════╗
              ║  pkg/model   ║ ║  pkg/mcp     ║
              ║  ToolCalling ║ ║  + pkg/mcp   ║
              ║  ChatModel   ║ ║  /cfg        ║
              ╚═════╤════════╝ ╚══════════════╝
                    │
                    v
              ╔═════╧════════╗
              ║  pkg/tool    ║
              ║  Invoke      ║
              ╚══════════════╝
```

**依赖方向**：`pkg/runner` → `internal/engine` → `pkg/agent`（仅引用 `Spawner` 接口）；`pkg/agent` → `pkg/model` / `pkg/mcp` / `pkg/tool` / `pkg/variable`。

**引擎分层策略**：详见 `doc/decision/engine-layering.md`（ADR）。

---

## 1. 命名与分层

| 层次 | 含义 | 代码落点 |
|------|------|----------|
| **入口层** | 对外暴露 `Runnable` 接口；装配模型适配与引擎 Spawner | `pkg/runner/` |
| **引擎层** | 管理运行状态、子树生命周期、预算、child registry | `internal/engine/` |
| **Agent loop 层** | Agent 定义、RunLoop、DefaultSpawner、工具构建 | `pkg/agent/` |
| **模型层** | ChatModel 抽象、适配器（openai / deepseek）、ToolInfo schema | `pkg/model/`、`pkg/model/adapters/*` |
| **MCP 层** | MCP 客户端、BootstrapToolInfos、cfg 配置 | `pkg/mcp/`、`pkg/mcp/cfg/` |
| **工具层** | ToolCall 分派与执行 | `pkg/tool/` |
| **变量层** | 会话级 VarStore、var_set 工具、{{key}} 替换 | `pkg/variable/` |
| **运行时边界** | 请求/事件/结果/跨 Agent 契约类型 | `pkg/runtime/request`、`pkg/runtime/event`、`pkg/runtime/outcome`、`pkg/runtime/exchange` |

---

## 2. `Runnable`、`Runner` 与事件消费

### 2.1 Runnable 接口

`pkg/agent/interface.go`：
```go
type Runnable interface {
	Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent
}
```

`Agent` 和 `Runner` 都满足该接口。

### 2.2 典型用法

用 `runner.NewRunner` 包装入口 Agent，从 channel 读事件直到 `query_end` 或 `error`：

```go
r := runner.NewRunner(entryAgent,
	runner.WithMaxTransfers(10),            // 最多 10 次手递手 transfer（防循环）
	runner.WithVarStore(sharedStore),       // 注入共享变量存储，多 Agent 共享状态
	runner.WithSpawnConfig(2, 4),           // spawn 最大深度 2，最多 4 个并行子 Agent
	runner.WithBudgetLimits(100000, 0.5),   // 子树 token 上限 10 万，USD 上限 0.5
)

ch := r.Run(ctx, &request.RuntimeRequest{
	SessionID:   "sess-1",
	UserMessage: "Hello",
	RunMode:     request.RunModeSingleAgent,  // 单 Agent 模式（无需 Network 编排）
	Options:     request.RuntimeOptions{Model: "deepseek-chat"},
})

for ev := range ch {
	switch ev.Type {
	case event.EventQueryEnd:
		if p := ev.QueryEnd(); p != nil && p.Outcome != nil {
			_ = p.Outcome.FinalText  // 拿到最终回答文本
		}
	case event.EventError:
		if p := ev.Error(); p != nil {
			_ = p.Message  // 运行时错误信息
		}
	}
}
```

### 2.3 Runner.Run() 内部

`Runner.Run()` 做三件事：
1. 构造 `RunState`（深度 0）和 `LoopPolicy`
2. 将 `agent.NewDefaultSpawner` 包装为 `engine.NewEngineSpawner`（注入子树预算 & 生命周期管理）
3. 调用 `Agent.Run()`（如果 entryAgent 有 handoff，由 `Runner` 在内部切换 Agent 并维护 `LoopState`）

---

## 3. `Agent`：字段与构造

### 3.1 Agent 结构体

`pkg/agent/agent.go`：
```go
type Agent struct {
	Name               string                          // Agent 标识名（如 "triage"、"math_expert"）
	Description        string                          // 对 LLM 可见的自我描述（transfer 时用到）
	ModelName          string                          // 模型名（如 "deepseek-chat"），供 Runner 选择适配器
	SystemInstructions string                          // 系统指令/提示词
	ChatModel          model.ToolCallingChatModel      // LLM 调用接口（支持 tool_calls）
	ToolInfos          []*model.ToolInfo               // 本地注册的工具列表（含 Handle 回调）
	Executor           tool.ToolExecutor               // 备选工具执行器（ToolInfo 未命中时使用）
	MCPServerProfiles  []cfg.MCPServerProfile          // MCP 服务器配置（Agent 启动时自动 Bootstrap）
	MaxSteps           int                             // ReAct 循环最大步数
	CallOptions        []model.CallOption              // LLM 调用参数（温度、top_p 等）
	ExtraTools         []*model.ToolInfo               // 注入工具（如 transfer_to_*、var_set），不参与 ValidateBindings
	ToolInterceptor    ToolInterceptor                 // 工具调用拦截器（返回 true 时拦截，用于 handoff）
	Variable           bool                            // 启用 var_set 工具和 [Variables] 注入
	SystemPromptBuilder SystemPromptBuilder            // 自定义 system prompt 组装管线
	SkillNames         []string                        // 绑定的技能包名称列表
	SkillRegistry      *skill.SkillRegistry            // 技能包注册表
	SkillShellTool     bool                            // 启用技能 shell 脚本执行工具
	LoadSkillTool      bool                            // 启用动态加载技能的工具
	SpawnEnabled       bool                            // 是否允许 spawn 子 Agent
	ChildAgentBuilder  func(*exchange.SpawnSpec) *Agent // 根据 SpawnSpec 构造子 Agent 模板
	Spawner            Spawner                         // Spawner 接口实现（可被 engineSpawner 包装注入预算管理）
	handoffs           []*Agent                        // transfer 目标 Agent 列表（通过 AddHandoff 注册）
}
```

### 3.2 关键 Option

`pkg/agent/options.go`：
```go
func New(chat model.ToolCallingChatModel, opts ...Option) *Agent  // 构造 Agent，opts 按顺序应用

func WithName(name string) Option                     // 设置 Agent 标识名
func WithDescription(d string) Option                 // 设置对 LLM 可见的描述
func WithSystemInstructions(s string) Option          // 设置系统提示词
func WithModelName(m string) Option                   // 设置模型名（供模型适配层选择）
func WithToolInfos(infos []*model.ToolInfo) Option    // 注册本地工具（含 Handle 回调）
func WithMCPServerProfiles(profiles ...cfg.MCPServerProfile) Option  // 挂载 MCP 服务器
func WithMaxSteps(n int) Option                       // 设置 ReAct 循环最大步数
func WithCallOptions(opts ...model.CallOption) Option  // 设置 LLM 调用参数
func WithExtraTools(tools []*model.ToolInfo) Option   // 添加注入工具（跳过 ValidateBindings）
func WithToolInterceptor(fn ToolInterceptor) Option    // 设置工具调用拦截器
func WithVariable() Option                            // 启用 var_set 工具和 [Variables] 注入
func WithSpawn(childBuilder func(*exchange.SpawnSpec) *Agent) Option  // 启用 spawn 子 Agent 能力
func WithSkills(reg *skill.SkillRegistry, names ...string) Option     // 绑定技能包
func WithSkillShellTool(enable bool) Option           // 启用技能 shell 脚本执行
func WithLoadSkillTool(enable bool) Option            // 启用动态加载技能的工具
```

### 3.3 构造示例

```go
chat := larkadapter.NewLarkChatModel(apiKey, "", modelName)

ag := agent.New(chat,
	agent.WithName("main"),
	agent.WithToolInfos(local),
	agent.WithMCPServerProfiles(cfg.MCPServerProfile{
		ID:         "seraglf",
		Transport:  cfg.MCPTransportStreamableHTTP,
		URL:        "http://127.0.0.1:8080/mcp",
		ToolPrefix: "seraglf__",
	}),
	agent.WithMaxSteps(32),
)
```

---

## 4. RunLoop 主路径

`pkg/agent/loop.go` — **自实现**的 Agent loop：

```go
func (a *Agent) RunLoop(
	ctx context.Context,
	req *request.RuntimeRequest,       // 用户请求（含消息、模型参数）
	ch chan<- *event.RuntimeEvent,     // 事件输出 channel（SSE 从此回传给前端）
	inheritedMsgs []*model.Message,    // 继承的历史消息（transfer 时传入上一 Agent 的消息）
	state *LoopState,                  // 跨 Agent 状态累加器（metrics、transfer chain）
) *InterceptedCall                     // 返回拦截的工具调用（handoff 时使用），nil 表示正常结束
```

核心逻辑：
```
for step := 0; step < maxSteps; step++:
  1. 拼 system prompt（含 [Variables]、Skills、MCP Prompts）
  2. emit(call_llm_start) → 含 tools、model、system_prompt
  3. Stream → emit(answer deltas) ... → emit(call_llm_end)
  4. 若 LLM 无 tool_calls → 结束
  5. 若有 tool_calls → executeToolCalls：
     a. 对每个 tool_call: emit(tool_call_start) → tool.Invoke → emit(tool_call_end)
     b. 若 spawn_subagent → 通过 Spawner 异步/同步执行子 Run
     c. tool message append 到消息列表 → 进入下一轮
  6. 无 tool_calls 后：emit(query_end)
```

最终返回 `*InterceptedCall`（若 ToolInterceptor 拦截了某工具调用）或 nil。

---

## 5. Spawn 机制：两条路径

### 5.1 同步路径

`makeSpawnHandler`（`spawn_tool.go`）在有 eventCh 参数时为异步，无 eventCh 时为同步。

**同步**：父 Agent 阻塞等待 `sp.Spawn()` 返回 `SpawnResult`，结果 JSON 序列化后作为工具调用返回值给父 LLM。

**异步**（eventCh != nil）：立即返回 fake `SpawnResult`，子 Agent 在后台 goroutine 中运行：

```go
if eventCh != nil {
	loop.AddAsyncSpawn()                    // 增加异步 spawn 计数器
	go func() {
		defer loop.DoneAsyncSpawn()         // 子完成时递减计数器
		res, err := sp.Spawn(ctx, derefRef(parent), spec)  // 在后台 goroutine 中阻塞等子完成
		if res != nil {
			loop.CollectAsyncSpawnResult(res.FinalText)  // 将子结果文本存入 LoopState
		}
	}()
	return sonic.MarshalString(&exchange.SpawnResult{
		Status: exchange.SpawnCompleted,    // 立即返回空结果给父 LLM，告诉它"子已异步启动"
	})
}
```

父 RunLoop 在结束时 `WaitAsyncSpawns()` → `FlushAsyncSpawnResults()` → 将子结果注入为 user message → `continue` 再走一轮 LLM 总结。

### 5.2 DefaultSpawner.Spawn()

`pkg/agent/spawn.go`：

```go
func (s *DefaultSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error)
```

内部流程：
1. 深度校验：`parent.Depth+1 >= MaxDepth` → `SpawnRejected`
2. 生命周期校验：非 ephemeral → `SpawnRejected`
3. 构造子 Agent（`Clone` + `applySpecToChildAgent` + 强制非流式模型）
4. **独立 goroutine** 中跑子 `RunLoop`，事件写入**本地** channel `ch`
5. 本地事件循环消费全部子事件：仅 `EventStart`/`EventQueryEnd` 通过 `spec.OutputCh` 转发（见 `event-semantics.md` ADR）；`ToolCallStart`/`ToolCallEnd` 记录到 `ChildToolCalls`；`answer`/`call_llm_*` 丢弃
6. 返回 `SpawnResult{Status, FinalText, Metrics, ChildToolCalls}`

### 5.3 Spawn 隔离原则

| 子事件 | 转发到父端 | 说明 |
|--------|-----------|------|
| `EventStart` | ✅ → `agent_transfer(TransferStart)` | 通过 `SpawnSpec.OutputCh` 非阻塞 |
| `EventQueryEnd` | ✅ → `agent_transfer(TransferEnd)` | 同上 |
| `EventAnswer` | 🚫 丢弃 | ReAct 中间文本隔离 |
| `EventCallLLMStart/End` | 🚫 丢弃 | 子模型调用隔离 |
| `EventToolCallStart/End` | 🚫 仅本地记录 | 聚合进 `ChildToolCalls` |

详见 `doc/decision/spawn-isolation.md`（ADR）。

---

## 6. `internal/engine` 引擎层

### 6.1 RunState

`internal/engine/runstate.go`：
```go
type RunState struct {
	RunID           string                    // 当前运行的唯一 ID
	Step            int                       // 当前 ReAct 步数
	LastError       error                     // 最近一次错误
	Termination     outcome.TerminationReason // 终止原因（completed | max_steps | cancelled | error）
	ParentRunID     string                    // 父运行 ID（根 run 为空）
	Depth           int                       // spawn 树深度（根为 0）
	ActiveChildRuns int                       // 当前活跃的子 Agent 数
	AllowSpawn      bool                      // 是否允许 spawn 子 Agent
	Children        *ChildRegistry            // 子 Agent 注册表（支持级联取消）
	BudgetCounter   *budget.BudgetCounter     // 树级共享预算计数器
}
```

### 6.2 engineSpawner

`internal/engine/spawn.go`：
```go
type engineSpawner struct {
	inner    Spawner        // 被包装的 pkg/agent DefaultSpawner（实际跑子 RunLoop）
	runState *RunState      // 当前运行状态（注入预算、child registry）
}

func NewEngineSpawner(inner Spawner, runState *RunState) *engineSpawner  // 构造引擎层 Spawner 包装
```

engineSpawner 在 `Spawn()` 中注入引擎层逻辑：
- 子 Agent 注册到 `ChildRegistry`（支持级联取消）
- 子 Agent token 消耗滚入树级预算

### 6.3 SpawnHandle & ChildRegistry

`internal/engine/spawn_handle.go`：
```go
type SpawnHandle struct {
	Ref    exchange.RunRef       // 子运行的引用信息
	state  atomic.Int32          // 运行状态：Running | Complete | Failed | Cancelled
	cancel context.CancelFunc    // 取消子 Agent 的函数（父结束时调用）
	done   chan struct{}         // 子 goroutine 退出时关闭
	result atomic.Value          // *exchange.SpawnResult
}

func NewSpawnHandle(ref exchange.RunRef, cancel context.CancelFunc) *SpawnHandle  // 注册子 Agent 句柄
func (h *SpawnHandle) Shutdown(reason outcome.TerminationReason)                  // 强制终止子 Agent
func (h *SpawnHandle) Done() <-chan struct{}                                      // 等待子 Agent 结束
func (h *SpawnHandle) State() ChildState                                          // 查询子 Agent 状态
```

`internal/engine/child_registry.go`：
```go
type ChildRegistry struct {
	children map[string]*SpawnHandle  // runID → SpawnHandle
}

func (r *ChildRegistry) Register(id string, h *SpawnHandle)                    // 注册新子 Agent
func (r *ChildRegistry) Unregister(id string) *SpawnHandle                     // 注销子 Agent
func (r *ChildRegistry) TerminateAll(reason outcome.TerminationReason) int     // 父结束时终止所有子 Agents
func (r *ChildRegistry) ActiveCount() int                                      // 当前活跃子 Agent 数
```

### 6.4 LoopPolicy

`internal/engine/policy.go`：
```go
type LoopPolicy interface {
	AllowStep(state *RunState) bool                // 检查是否允许执行下一步（max steps 检查）
	AllowSpawn(parent *RunState, depth int) bool   // 检查是否允许 spawn 子 Agent（深度/并发检查）
	MaxSteps() int                                 // 返回单次 Run 的最大步数
	MaxSpawnDepth() int                            // 返回 spawn 树最大深度
	MaxConcurrentSpawns() int                      // 返回最大并行子 Agent 数
	BudgetCounter() *budget.BudgetCounter          // 返回树级共享预算计数器
}
```

`defaultLoopPolicy` 提供默认实现，由 `Runner` 在 `Run()` 中装配。

---

## 7. `pkg/runner` 入口装配层

`pkg/runner/runner.go`：
```go
type Runner struct {
	entryAgent          *agent.Agent           // 入口 Agent（无 handoff 时直接跑这个 Agent 的 RunLoop）
	maxTransfers        int                    // 最大 transfer 次数（防循环，0 表示禁止 transfer）
	logger              log.Logger
	varStore            *variable.VarStore     // 共享变量存储（跨 Agent transfer 时保持一致的内容）
	skillRegistry       *skill.SkillRegistry   // 技能包注册表
	spawnMaxDepth       *int                   // spawn 树最大深度（nil 时使用默认值 2）
	maxConcurrentSpawns int                    // 最大并行子 Agent 数（默认 4）
	budgetTokens        int64                  // 子树 token 预算上限（0 表示不限制）
	budgetUSD           float64                // 子树成本预算上限（0 表示不限制）
}

func NewRunner(entryAgent *agent.Agent, opts ...RunOption) *Runner  // 构造 Runner，opts 按顺序应用
func (r *Runner) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent  // 启动运行，返回事件流 channel
```

### RunOption

```go
func WithLogger(l log.Logger) RunOption                                // 设置日志器
func WithMaxTransfers(n int) RunOption                                 // 设置最大 transfer 次数
func WithVarStore(s *variable.VarStore) RunOption                      // 注入共享变量存储
func WithSpawnConfig(maxDepth, maxConcurrentSpawns int) RunOption      // 设置 spawn 深度和并行上限
func WithBudgetLimits(maxTokens int64, maxUSD float64) RunOption       // 设置子树预算上限
```

### 模型适配

`pkg/runner/deepseek_default.go`：
```go
func ApplyDeepSeekFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string)  // 将 Agent 的 ChatModel 替换为 DeepSeek 适配器
```

Runner 在构造 Agent 后调用该函数，覆写默认 ChatModel 适配器。

---

## 8. 用户侧契约：`RuntimeRequest` / `RuntimeEvent` / `RuntimeOutcome`

### 8.1 RuntimeRequest

`pkg/runtime/request/request.go`：
```go
type RuntimeRequest struct {
	SessionID   string          // 会话 ID（用于关联多轮对话）
	UserMessage string          // 用户输入的文本
	RunMode     RunMode         // RunModeSingleAgent（单 Agent 模式）| RunModeNetwork（预留）| RunModeInherit
	Options     RuntimeOptions  // 运行时参数（模型、技能、spawn 配置等）
}

type RuntimeOptions struct {
	Model           string      // 模型名（可覆盖 Agent 上的默认模型）
	SkillIDs        []string    // 绑定的技能包 ID 列表
	MaxSteps        *int        // 最大 ReAct 步数（覆盖 Agent 默认值）
	SpawnMaxDepth   *int        // spawn 树最大深度（覆盖默认深度 2）
	NetworkStrategy string      // 预留字段（当前未使用）
}
```

### 8.2 RuntimeEvent

`pkg/runtime/event/event.go`：
```go
type RuntimeEvent struct {
	Type    EventMessageType  // 事件类型（start / answer / tool_call_start / query_end 等）
	RunID   string            // 所属运行 ID
	Step    int               // 当前 ReAct 步数
	Payload EventPayload      // 事件载荷（各类型的结构化数据）
}
```

事件类型枚举：

| 常量 | 字符串值 | 用途 |
|------|---------|------|
| `EventStart` | `"start"` | Run 开始 |
| `EventQuestion` | `"question"` | 用户输入进入本轮 loop |
| `EventAnswer` | `"answer"` | 流式文本 delta；`IsReasoning` 区分推理/普通；`IsFinal` 标记终态覆盖 |
| `EventCallLLMStart` | `"call_llm_start"` | LLM 调用开始（含 tools、system_prompt） |
| `EventCallLLMEnd` | `"call_llm_end"` | LLM 调用结束（含 finish_reason、tokens） |
| `EventToolCallStart` | `"tool_call_start"` | 工具调用开始（含 name、arguments） |
| `EventToolCallEnd` | `"tool_call_end"` | 工具调用结束（含 output、is_error） |
| `EventAgentTransfer` | `"agent_transfer"` | Agent 手递手转移 |
| `EventSpawnStart`    | `"spawn_start"`    | 子 Agent 启动    |
| `EventSpawnEnd`      | `"spawn_end"`      | 子 Agent 结束    |
| `EventVarChange` | `"var_change"` | 变量变更（当前 RunLoop 未统一 emit） |
| `EventQueryEnd` | `"query_end"` | 本轮用户轮次结束（含 RuntimeOutcome） |
| `EventError` | `"error"` | 运行时错误 |

### 8.3 RuntimeOutcome

`pkg/runtime/outcome/outcome.go`：
```go
type RuntimeOutcome struct {
	RunID         string                    // 本次运行的 ID
	FinalText     string                    // 最终回答文本
	Termination   TerminationReason         // 终止原因：completed | max_steps | cancelled | error
	Metrics       RunMetrics                // 运行指标（token 用量、步数等）
	ChildRunIDs   []string                  // 子运行的 ID 列表（用于追踪 spawn 树）
	TransferChain []string                  // transfer 经过的 Agent 名称链
	VarStore      *variable.VarStore        // 运行结束时的变量快照（JSON 序列化时排除）
}

type RunMetrics struct {
	Model        string  `json:"model"`          // 使用的模型名
	InputTokens  int64   `json:"input_tokens"`   // 输入 token 总数（含子树）
	OutputTokens int64   `json:"output_tokens"`  // 输出 token 总数（含子树）
	TotalTokens  int64   `json:"total_tokens"`   // 总 token 数
	TotalCostUSD float64 `json:"total_cost_usd"` // 估算总成本（美元）
	Steps        int     `json:"steps"`          // ReAct 步数
}
```

### 8.4 典型事件顺序（单 Agent，无 error）

```
start → question → call_llm_start → call_llm_end
   → (repeat: tool_call_start → tool_call_end → call_llm_start → call_llm_end)
   → answer (streaming deltas) ...
   → query_end (payload.Outcome)
```

Transfer 模式下会插入 `agent_transfer(TransferStart/TransferEnd)`；spawn 模式下新增 `spawn_start` / `spawn_end` 事件。

---

## 9. `pkg/runtime/exchange`：跨 Agent 契约

### 9.1 RunRef

```go
type RunRef struct {
	RunID       string
	AgentRole   string
	ParentRunID string
	Depth       int
}
```

### 9.2 SpawnSpec

```go
type SpawnSpec struct {
	Task            string                       // 子任务说明
	SystemAddendum  string                       // 追加系统指令
	SkillIDs        []string                     // 技能 ID
	ToolAllowlist   []string                     // 工具白名单
	LoopOverrides   LoopOverrides                // 循环参数覆盖
	ModelOverride   string                       // 模型覆盖
	MemoryDigest    *MemoryDigest                // 父侧短期记忆摘要（可选）
	Lifecycle       Lifecycle                    // ephemeral（已实现）| linger（预留）
	AllowChildSpawn bool                         // 子能否再 spawn（默认 false）
	OutputCh        chan<- *event.RuntimeEvent    // 子→父事件转发通道（非阻塞）
}
```

### 9.3 SpawnResult

```go
type SpawnResult struct {
	ChildRunRef    RunRef
	Status         SpawnStatus   // completed | failed | rejected
	FinalText      string
	Error          *SpawnError
	Metrics        outcome.RunMetrics
	ChildToolCalls []ChildToolCall
}
```

### 9.4 ChildToolCall

```go
type ChildToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Output    string `json:"output,omitempty"`
	IsError   bool   `json:"is_error"`
}
```

---

## 10. `pkg/model`：消息与 ToolInfo

### 10.1 消息类型

```go
type Role string

const (
	RoleUser      Role = "user"       // 用户消息
	RoleAssistant Role = "assistant"   // 模型回复（含 tool_calls）
	RoleSystem    Role = "system"      // 系统提示词
	RoleTool      Role = "tool"        // 工具调用结果
)

type Message struct {
	Role             Role            // 消息角色
	Content          string          // 文本内容
	ToolCalls        []ToolCallPart  // 模型发起的工具调用列表
	ToolCallID       string          // 工具调用 ID（Role=Tool 时关联到某次调用）
	Name             string          // 工具名（Role=Tool 时标识来源工具）
	ReasoningContent string          // 推理内容（适配层透传，前端渲染为"思考过程"气泡）
	InputTokens      int64           // 本条消息的输入 token 计数
	OutputTokens     int64           // 本条消息的输出 token 计数
}
```

### 10.2 ToolInfo

```go
type ToolInfo struct {
	Name        string                 // 工具名（对 LLM 可见，调用时通过此名匹配）
	Description string                 // 工具描述（对 LLM 可见，影响模型选择工具的策略）
	Parameters  map[string]interface{} // JSON Schema 参数描述
	Handle      ToolCallHandler        // 工具执行回调（Go 函数，接收 JSON 参数返回 JSON 结果）
}

type ToolCallHandler func(ctx context.Context, argumentsJSON string) (string, error)

type ToolCallPart struct {
	ID        string          // 工具调用 ID（用于关联 ToolCallStart 和 ToolCallEnd 事件）
	Name      string          // 工具名
	Arguments string          // 参数字符串（JSON）
	Handle    ToolCallHandler // 快捷执行回调（可在此直接调用，跳过 ToolInfo 匹配）
}
```

### 10.3 ChatModel 接口

`pkg/model/interface/chatmodel.go`：
```go
type BaseChatModel interface {
	Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error)  // 非流式生成：一次返回完整回复
	Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error)  // 流式生成：逐 chunk 返回
}

type ToolCallingChatModel interface {
	BaseChatModel
	WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error)  // 绑定工具列表，返回带工具调用能力的模型实例
}
```

当前适配器实现：`pkg/model/adapters/openai/`（OpenAI SDK）、`pkg/model/adapters/deepseek/`（DeepSeek SDK）、`pkg/model/adapters/lark/`（火山方舟/Ark）。

---

## 11. 工具合并顺序与 `bindModel`

`pkg/agent/helpers.go` 中 `mergedToolInfos` 的合并顺序：

1. `ToolInfos`（本地注册的工具）
2. `mcpToolInfos`（MCP 自动发现）
3. `ExtraTools`（如 `transfer_to_*`、`var_set` 等注入工具）
4. 可选 `extraRuntime`（单次 run 动态附加）

发给模型的 `WithTools` 使用**全量 merged**；`ValidateBindings` 只检查 `ToolInfos` + `mcpToolInfos`（不含 `ExtraTools`）。

```
  bindModel
      |
      +-- MCPServerProfiles non-empty? → mcp.BootstrapToolInfos → mcpToolInfos, mcpStop
      |
      +-- ValidateBindings(ToolInfos + mcpToolInfos, Executor)
      |
      +-- ChatModel.WithTools( mergedToolInfos(...) )
```

---

## 12. `tool.Invoke` 解析链

`pkg/tool/dispatch.go`：
```go
func Invoke(ctx context.Context, infos []*model.ToolInfo, ex ToolExecutor, tc model.ToolCallPart) (string, error) {
	// 第 1 优先：ToolCallPart 自带的 Handle（由 spawn_subagent 等内置工具使用）
	if tc.Handle != nil {
		return tc.Handle(ctx, tc.Arguments)
	}
	// 第 2 优先：在 ToolInfo 列表中按名称匹配 Handle（MCP 工具在此命中）
	for _, info := range infos {
		if info.Name != tc.Name { continue }
		if info.Handle != nil {
			return info.Handle(ctx, tc.Arguments)
		}
	}
	// 第 3 优先：ToolExecutor 兜底（Executable 接口，供外部工具注册表使用）
	if ex != nil { return ex.Execute(ctx, tc.Name, tc.Arguments) }
	return "", ErrNoHandler
}
```

MCP 工具在 `BootstrapToolInfos` 中为每个远端 tool 设置 `ToolInfo.Handle`，因此按暴露名匹配落在上述循环中。

---

## 13. MCP：配置与 Bootstrap

### 13.1 MCPServerProfile

`pkg/mcp/cfg/config.go`：
```go
type MCPServerProfile struct {
	ID            string              // 服务器唯一标识
	Transport     MCPTransportKind    // 传输协议：MCPTransportStdio | MCPTransportStreamableHTTP
	Command       []string            // stdio 模式：子进程命令及其参数
	URL           string              // HTTP 模式：服务器 URL
	Env           map[string]string   // 环境变量（透传给子进程或 HTTP header）
	Headers       map[string]string   // HTTP 请求头
	ToolPrefix    string              // 工具名前缀（如 "seraglf__"），用于跨 Server 防冲突
	ToolAllowlist []string            // 允许调用的工具名白名单（空表示全部允许）
}
```

### 13.2 BootstrapToolInfos

`pkg/mcp/bootstrap.go`：
```go
func BootstrapToolInfos(ctx context.Context, profiles ...cfg.MCPServerProfile) ([]*model.ToolInfo, func(), error)  // 连接 MCP Server、ListTools、生成 ToolInfo 列表
```

- 暴露名：`ToolPrefix + MCPToolName`（例如 `seraglf__retrieve`）
- 内部为每个 MCP tool 设置 `ToolInfo.Handle`，调用 `ClientSession.CallTool`

### 13.3 调试包

`loopforge/debug`（内嵌在 `pkg/mcp` 路径中）提供不经 Agent 的 MCP 直调：

```go
import lfdebug "loopforge/debug"   // 别名 lfdebug 避免与 runtime/debug 冲突

conn, stop, err := lfdebug.Dial(ctx, cfg.MCPServerProfile{
	ID:        "probe",
	Transport: cfg.MCPTransportStreamableHTTP,
	URL:       "http://127.0.0.1:8080/mcp",
})
defer stop()

_ = conn.Ping(ctx)                                                    // 探活
tools, _ := conn.ListTools(ctx)                                       // 列出 MCP Server 上的所有工具
res, _ := conn.CallTool(ctx, "mcp_tool_name", map[string]any{...})    // 直接调用 MCP 工具（用原名，非暴露名）
```

---

## 14. Variable（`pkg/variable`）

会话级自定义变量：跨轮次状态、`{{key}}` 占位符替换、`[Variables]` 注入。

| 类型 | 作用 |
|------|------|
| **`VarStore`** | 线程安全的键值容器 |
| **`Manifest` / `Spec`** | 静态清单：声明 key、描述、Default、Visitable |
| **`Materialize`** | 合并清单 + 快照 + runtime 绑定得到初始 `VarStore` |
| **`StoreSnapshot`** | 序列化快照；跨请求恢复 |

启用 `agent.WithVariable()` 后，RunLoop 自动：
- 注入 `var_set` 工具（extraRuntime，不参与 ValidateBindings）
- 将 `VarStore.PromptBlock()` 以 `[Variables]` 形式并入 system prompt
- 对 user message 做 `ReplaceDoubleBraceParams`（`{{key}}` 替换）
- `const_` 前缀的 key 对 Agent 侧只读

---

## 15. 与代码目录的对应关系

| 主题 | 包路径 |
|------|--------|
| 请求 / 事件 / 结果 | `pkg/runtime/request`、`pkg/runtime/event`、`pkg/runtime/outcome` |
| Spawn/Network 结构 | `pkg/runtime/exchange` |
| Agent loop | `pkg/agent` |
| 引擎核心 | `internal/engine` |
| Runner、transfer | `pkg/runner` |
| 模型与适配器 | `pkg/model`、`pkg/model/adapters/*` |
| Tool 分发 | `pkg/tool` |
| MCP | `pkg/mcp`、`pkg/mcp/cfg` |
| MCP 调试 | `loopforge/debug` |
| 变量 | `pkg/variable` |
| 技能 | `pkg/skill` |
| 测试前端 | `test-server/` |

---

## 16. 相关文档与 ADR

| 文档 | 内容 |
|------|------|
| `doc/decision/engine-layering.md` | 三层引擎架构设计决策 |
| `doc/decision/event-semantics.md` | spawn_start / spawn_end 事件语义决策 |
| `doc/decision/spawn-isolation.md` | 子 Agent 事件隔离机制决策 |
| `doc/design/data-fusion.md` | 事件类型与现网对齐 |
| `doc/design/multi-agent-engine.md` | 多 Agent、路线图 |
| `doc/design/architecture.md` | 模块与部署 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-29 | v0.9.5 | 全面重写：新增三层引擎架构（§0）、Spawn 两条路径（§5）、engine 层详述（§6）、runner 装配层（§7）；补充 SpawnSpec 全部字段（§9）；移除 pkg/network 引用；新增 ADR 交叉引用。 |

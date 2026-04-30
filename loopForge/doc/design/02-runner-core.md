# loopForge Runner 核心

> 本文档描述 `pkg/runner` 中 Runner 的**职责与执行逻辑**。Runner 是 loopForge 的顶层入口，负责装配模型适配器、注入引擎层 Spawner、管理 transfer 编排，对外暴露统一的 `Runnable` 接口。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md)——Runner 的 `Run()` 内部调用 Agent 的 `RunLoop()`，理解 Agent 的迭代逻辑有助于理解 Runner。

---

## 1. Runner 是什么

Runner 是 loopForge 面向调用方的唯一入口。它封装一个入口 Agent，在 `Run()` 中按以下流程启动一次执行：

```go
r := runner.NewRunner(entryAgent, opts...)
ch := r.Run(ctx, req)        // ← 返回事件 channel，调用方从此消费
```

Runner 隐藏了 Agent 构造之外的复杂性：

- **模型适配**：在 Agent 的 `ChatModel` 为 nil 时，自动从环境变量装配 Lark/方舟 SDK 的默认适配器
- **引擎层注入**：创建 `engine.RunState`、`engine.LoopPolicy`、`engine.ChildRegistry`，并将 `engineSpawner` 包装到 Agent 的 `Spawner` 接口上
- **transfer 编排**：当入口 Agent 注册了 handoff 目标时，自动管理多 Agent 切换和消息传递
- **预算与生命周期**：管理子树 token/cost 预算，父结束时级联取消所有子 Agent

---

## 2. Runner 结构体

`pkg/runner/runner.go`：

```go
type Runner struct {
	entryAgent   *agent.Agent      // 入口 Agent
	maxTransfers int               // 最大 transfer 次数（默认 10）
	logger       log.Logger        // 结构化日志器
	varStore     *variable.VarStore // 共享变量存储（transfer 安全）

	skillRegistry *skill.SkillRegistry  // 技能包注册表
	skillPaths    []string              // 技能扫描路径

	// spawn 与预算配置（通过 WithXxx 设置）
	spawnMaxDepth       *int     // nil = 使用请求默认值
	maxConcurrentSpawns int      // 0 = 使用全局默认值
	budgetTokens        int64    // 0 = 不限制
	budgetUSD           float64  // 0 = 不限制
}
```

### 2.1 构造

```go
func NewRunner(entryAgent *agent.Agent, opts ...RunOption) *Runner
```

- `entryAgent`：入口 Agent。若其注册了 handoff（通过 `AddHandoff`），Runner 进入 transfer 模式；否则走单 Agent 模式。
- `opts`：配置选项，按顺序应用。

### 2.2 配置选项

```go
func WithLogger(l log.Logger) RunOption                               // 设置日志器
func WithMaxTransfers(n int) RunOption                                 // 设置最大 transfer 次数
func WithVarStore(s *variable.VarStore) RunOption                      // 注入共享变量存储
func WithSpawnConfig(maxDepth, maxConcurrentSpawns int) RunOption      // 设置 spawn 深度和并行上限
func WithBudgetLimits(maxTokens int64, maxUSD float64) RunOption       // 设置子树预算上限
func WithSkillRegistry(reg *skill.SkillRegistry) RunOption             // 设置技能包注册表
func WithSkillPath(paths ...string) RunOption                          // 设置技能扫描路径
```

---

## 3. `Run()` 执行流程

`pkg/runner/runner.go`：

```go
func (r *Runner) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent
```

`Run()` 返回一个缓冲 channel（容量 128），调用方从该 channel 消费 `RuntimeEvent`，直到 channel 关闭。正常结束时最后一个事件是 `EventQueryEnd`。

### 3.1 内部流程

```
Runner.Run(ctx, req)
  │
  ├── 创建事件 channel ch（缓冲 128）
  │
  ├── 启动 goroutine：
  │     │
  │     ├── 0. 解析 spawn 配置：
  │     │      r.spawnMaxDepth → req.Options.SpawnMaxDepth（若未设）
  │     │      r.budgetTokens / r.budgetUSD → BudgetCounter
  │     │
  │     ├── 1. 构造 RunState 与 LoopPolicy：
  │     │      runState = {RunID, Depth=0, AllowSpawn=true}
  │     │      policy = NewCustomLoopPolicy(maxSteps, spawnDepth, concurrent, budget)
  │     │
  │     ├── 2. 判断执行路径：
  │     │      ├── entryAgent 有 handoff → r.runTransferLoop(...)
  │     │      └── 无 handoff → 单 Agent 执行
  │     │
  │     └── 3. 关闭 ch（goroutine 退出时 defer close(ch)）
  │
  └── 返回 ch
```

### 3.2 单 Agent 模式

当入口 Agent 没有 handoff 目标时：

```
  ├── 确定 VarStore（从 runner 继承或新建）
  ├── 构造 LoopState{VarStore, CurrentRunRef}
  ├── a = entryAgent.Clone()                // 隔离并发写入
  ├── applyDefaultLarkIfNeeded(a)           // ChatModel==nil 时自动装配
  ├── prepareAgent(a)                       // 解析技能包
  │
  ├── 注入 engineSpawner：
  │     inner = NewDefaultSpawner(a, maxD)
  │     a.Spawner = NewEngineSpawner(inner, runState)  // 包装：预算 + ChildRegistry
  │
  ├── a.RunLoop(ctx, req, ch, nil, st)      // 执行 Agent loop（见 01-agent-core.md §5）
  │
  └── runState.Terminate(completed)          // 清理：取消所有子 Agent
```

### 3.3 Transfer 模式

当入口 Agent 注册了 handoff 目标时，Runner 进入 `runTransferLoop`。

```
runTransferLoop(ctx, req, ch, runState, policy)
  │
  ├── 初始化：runStore（变量）、accumulated（累积指标）、transferChain
  │
  ├── 主循环（最多 maxTransfers + 1 次）：
  │     │
  │     ├── a = currentAgent.Clone()
  │     ├── applyDefaultLarkIfNeeded(a) + prepareAgent(a)
  │     │
  │     ├── 装配 transfer 工具链：
  │     │     a.ExtraTools = BuildTransferTools(current)   // 注册 transfer_to_* 工具
  │     │     a.ToolInterceptor = IsTransferTool           // 拦截工具调用
  │     │     a.SystemInstructions += BuildTransferPrompt  // 追加 transfer 提示
  │     │
  │     ├── 注入 engineSpawner（同上）
  │     │
  │     ├── RunLoop(ctx, req, ch, inheritedMsgs, st)
  │     │     └── 返回 *InterceptedCall（含目标 Agent 名）
  │     │
  │     ├── runState.Terminate(completed)   // 本 Agent 的子树清理
  │     │
  │     ├── 若 result == nil → 结束（回到入口 return）
  │     │
  │     ├── transferCount++ → 检查上限
  │     │
  │     ├── 累积指标：accumulated += result.Metrics
  │     │
  │     ├── 解析目标 Agent：
  │     │     target = TargetAgent(result.ToolCall.Name)   // 从工具名提取 Agent 名
  │     │     next = findHandoff(current, target)           // 从 handoffs 中查找
  │     │
  │     ├── emit(agent_transfer, phase=start)             // 通知前端 transfer
  │     │
  │     ├── 构造继承消息：
  │     │     inheritedMsgs = appendSyntheticToolResponsesAfterTransfer(...)
  │     │     (为被拦截的 tool_calls 生成占位 tool_result，满足 API 格式要求)
  │     │
  │     ├── transferChain 追加当前 Agent 名
  │     │
  │     └── current = next → 进入下一轮
  │
  └── 结束（回到入口 return）
```

---

## 4. 模型适配器

Runner 在每次执行 Agent 前，若 `Agent.ChatModel` 为 nil，会尝试从环境变量自动装配默认适配器：

### 4.1 Lark/方舟 适配器

`pkg/runner/lark_default.go`：

```go
func ApplyLarkFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string)
func applyDefaultLarkIfNeeded(a *agent.Agent)
```

环境变量查找优先级：

| 变量 | 含义 |
|------|------|
| `LARK_API_KEY` / `DOUBAO_API_KEY` / `ARK_API_KEY` | API Key |
| `LARK_BASE_URL` / `DOUBAO_BASE_URL` | 请求地址 |
| `LARK_MODEL` / `ARK_MODEL` / `DOUBAO_MODEL` | 模型名（兜底 `deepseek-v3-2-251201`） |

### 4.2 DeepSeek 适配器

`pkg/runner/deepseek_default.go`：

```go
func ApplyDeepSeekFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string)
```

环境变量：

| 变量 | 含义 |
|------|------|
| `DEEPSEEK_API_KEY` | API Key |
| `DEEPSEEK_BASE_URL` | 请求地址 |
| `DEEPSEEK_MODEL` | 模型名（兜底 `deepseek-chat`） |

---

## 5. 引擎层注入

`Runner.Run()` 在每次执行 Agent 前做以下引擎层注入：

### 5.1 Spawner 包装

`agent.DefaultSpawner` 负责子 Agent 的创建与隔离运行；`engine.NewEngineSpawner` 包装该实现，在 `Spawn()` 中注入：

- 子 Agent 注册到 `RunState.Children`（`ChildRegistry`），支持父结束时级联取消
- 子 Agent 的 token 消耗滚入 `RunState.BudgetCounter`（树级共享预算）

```go
inner := agent.NewDefaultSpawner(a, maxD)          // 纯 loop 层的 spawner
a.Spawner = engine.NewEngineSpawner(inner, runState) // 包装：注入引擎层逻辑
```

详见 [`engine-layering.md`](../decision/engine-layering.md)（ADR，待创建）。

### 5.2 RunState

```go
runState := &engine.RunState{
	RunID:         runID,
	Depth:         0,
	AllowSpawn:    true,
	Children:      engine.NewChildRegistry(),
	BudgetCounter: policy.BudgetCounter(),
}
```

`RunState` 在一次 `Run()` 中全局共享。transfer 模式下，多个 Agent 共享同一个 `RunState`，因此子 Agent 的注册和预算在各个 Agent hop 之间保持连续。

### 5.3 级联清理

每个 Agent hop 结束后（或整次 Run 结束后），调用 `runState.Terminate(reason)`：

```go
runState.Terminate(outcome.TerminationCompleted)
```

这会通过 `ChildRegistry.TerminateAll()` 取消所有属于此 Run 的活跃子 Agent。

---

## 6. 与 Agent 的分工

| 职责 | Agent | Runner |
|------|-------|--------|
| LLM 循环迭代 | **负责**（RunLoop） | — |
| 事件发射 | **负责**（emit） | — |
| 消息构建 | **负责**（buildMessages） | — |
| 工具执行 | **负责**（executeToolCalls） | — |
| 模型适配器装配 | — | **负责**（applyDefaultLarkIfNeeded） |
| RunState 管理 | — | **负责**（创建与清理） |
| engineSpawner 注入 | — | **负责** |
| transfer 编排 | — | **负责**（runTransferLoop） |
| 技能包解析与注入 | — | **负责**（prepareAgent） |
| 子树预算与级联取消 | — | **负责**（通过 RunState） |
| 对外暴露 Runnable 接口 | 直接可用 | 包装后可用 |

---

## 7. 事件流对比

### 单 Agent 模式

事件流与 [01-agent-core.md §5](01-agent-core.md#5-runloop-核心逻辑) 完全一致，`Runner.Run()` 是薄封装：

```
start → question → call_llm_start → [answer] → call_llm_end
   → (repeat tool_call_start/end if tools) → query_end
```

### Transfer 模式

每转移一次 Agent，插入一对 `agent_transfer` 事件：

```
[Agent A] start → question → call_llm_start → call_llm_end
   → (可能多轮)
   → agent_transfer(phase=start, from=A, to=B)
[Agent B] call_llm_start → call_llm_end → ... → query_end
```

- `start`/`question` 仅在第一个 Agent 发射（后续 Agent 的 `SuppressBookends=true`）
- 最终 `query_end` 携带累积的 `RunMetrics`（含所有 Agent hop 的 token 和步数）
- `Outcome.TransferChain` 包含经过的 Agent 名顺序列表

---

## 8. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/runner/runner.go` | Runner 结构体、Run、runTransferLoop、NewRunner |
| `pkg/runner/lark_default.go` | Lark/方舟默认适配器装配 |
| `pkg/runner/deepseek_default.go` | DeepSeek 默认适配器装配 |
| `pkg/runner/skills.go` | 技能注册表关联与 prepareAgent |

---

## 9. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) | Runner 调用 Agent.RunLoop；理解 Agent 循环逻辑是前置知识 |
| [engine-layering.md](../decision/engine-layering.md)（ADR，待创建） | 三层架构：runner → engine → agent |
| [`00-abstractions.md`](00-abstractions.md) §7 | Runner 结构体与选项的代码级描述 |
| [`plan-v0.9.5.md`](../plan/plan-v0.9.5.md) | 本文档为 v0.9.5 文档修订的一部分 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-29 | v0.9.5 | 初稿：聚焦 Runner 自身职责与执行逻辑，与 agent-core.md 联动。 |

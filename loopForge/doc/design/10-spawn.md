# loopForge Spawn（动态子 Agent）

> 本文档描述 loopForge 的 **spawn 机制**——父 Agent 在 LLM 推理过程中通过 `spawn_subagent` 工具创建子 Agent，在独立 goroutine 中执行隔离的子任务，结果回注到父 LLM 对话。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md)——Agent 的 `RunLoop` 与 `Spawner` 接口；[04-tools.md](04-tools.md)——`spawn_subagent` 作为注入工具。
>
> **与 Transfer 的对比**：[07-transfer.md](07-transfer.md) §1.2——spawn 是并行隔离执行，transfer 是串行上下文继承。

***

## 1. 概念

### 1.1 什么是 Spawn

Spawn 是**动态创建子 Agent 执行隔离任务**的机制。父 Agent 在 LLM 推理中判断"这个子任务可以独立完成"，调用 `spawn_subagent` 工具，引擎在独立 goroutine 中启动一个子 Agent、跑完子任务、将结果回注到父对话。

```
父 Agent LLM 推理:
  "Calculate 7*8+3" → 我可以拆："7*8" 和 "+3"
  → tool_calls: spawn_subagent(task="Calculate 7*8", ...)
                  spawn_subagent(task="Add 3 to the result", ...)

[子 Agent A] 独立 goroutine → 结果: "56"
[子 Agent B] 独立 goroutine → 结果: "59"

父 LLM 收到两个 tool_result → 总结: "The result is 59"
```

### 1.2 与 Transfer 的对比

| 维度    | Spawn                                         | Transfer                               |
| ----- | --------------------------------------------- | -------------------------------------- |
| 执行模型  | **并行/异步**——子 Agent 在后台 goroutine              | **串行**——Agent A 完成后 Agent B 接管         |
| 对话上下文 | 子 Agent **完全隔离**（仅接收 task + system\_addendum） | Agent B 继承 Agent A 完整消息历史              |
| 结果处理  | 子结果 JSON 回注到父对话 → 父 LLM 总结                    | Agent B 直接输出最终回答                       |
| 事件标记  | `agent_transfer(phase=start/end, depth>0)`    | `agent_transfer(phase=start, depth=0)` |
| 工具    | `spawn_subagent`                              | `transfer_to_*`                        |

***

## 2. 核心组件

### 2.1 SpawnSpec：子任务规格

`pkg/runtime/exchange/exchange.go`：

```go
type SpawnSpec struct {
	Task            string                       // 子任务说明（必填，作为子 Agent 的 UserMessage）
	SystemAddendum  string                       // 追加到子 Agent 系统指令的文本
	SkillIDs        []string                     // 子 Agent 绑定的技能 ID
	ToolAllowlist   []string                     // 工具白名单（glob 通配）
	LoopOverrides   LoopOverrides                // 循环参数覆盖（max_steps 等）
	ModelOverride   string                       // 模型覆盖（空 = 继承父）
	MemoryDigest    *MemoryDigest                // 父侧短期记忆摘要（可选，默认关闭）
	Lifecycle       Lifecycle                    // ephemeral（已实现）| linger（预留）
	AllowChildSpawn bool                         // 子能否再 spawn（默认 false）
	OutputCh        chan<- *event.RuntimeEvent    // 子→父事件转发通道（非阻塞）
}
```

### 2.2 SpawnResult：子任务结果

```go
type SpawnResult struct {
	ChildRunRef    RunRef                    // 子运行的引用信息（RunID / Depth / ParentRunID）
	Status         SpawnStatus               // completed | failed | rejected
	FinalText      string                    // 子 Agent 的最终输出文本
	Error          *SpawnError               // 结构化错误（被拒绝时含 Code/Message）
	Metrics        outcome.RunMetrics        // 子 Agent 的 token 消耗
	ChildToolCalls []ChildToolCall           // 子 Agent 的工具调用记录
}
```

`ChildToolCall` 聚合子 Agent 内部的每次工具调用：

```go
type ChildToolCall struct {
	Name      string `json:"name"`      // 工具名
	Arguments string `json:"arguments"`  // 参数 JSON
	Output    string `json:"output"`     // 输出 JSON
	IsError   bool   `json:"is_error"`  // 是否执行出错
}
```

### 2.3 Spawner 接口

两层 Spawner 接口（`pkg/agent` 和 `internal/engine` 中签名相同）：

```go
type Spawner interface {
	Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error)
}
```

- **DefaultSpawner**（`pkg/agent/spawn.go`）：纯 loop 层的 spawn 实现，负责子 Agent 构造、隔离运行、事件过滤
- **engineSpawner**（`internal/engine/spawn.go`）：包装 DefaultSpawner，注入引擎层逻辑（ChildRegistry 注册、预算消费）

***

## 3. DefaultSpawner：子 Agent 的创建与隔离

`pkg/agent/spawn.go`：

```go
func (s *DefaultSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error)
```

### 3.1 校验阶段

```
DefaultSpawner.Spawn(ctx, parent, spec)
  │
  ├── 1. 深度校验：
  │       parent.Depth+1 >= s.MaxDepth → Status=SpawnRejected, Code="max_depth"
  │
  ├── 2. 生命周期校验：
  │       spec.Lifecycle == "linger" → Status=SpawnRejected, Code="unsupported_lifecycle"
  │       (v0.9.x 仅支持 ephemeral；linger 预留但拒绝)
  │
  └── 3. 构造子 Agent：
          childTmpl = parent.ChildAgentBuilder(spec)  // 调用方提供的子 Agent 构造器
          childTmpl.Clone()
          applySpecToChildAgent(childTmpl, spec)       // 注入 system_addendum + MemoryDigest
          childTmpl.ChatModel = WrapNonStream(...)      // 强制非流式 LLM 调用（§6 规则）
```

### 3.2 执行与隔离阶段

```
  │
  ├── 4. 构造子 RunLoop 输入：
  │       childRef = {RunID, ParentRunID, Depth=parent.Depth+1}
  │       sub = RuntimeRequest{UserMessage=spec.Task, Options={Model, SkillIDs, MaxSteps}}
  │       st = LoopState{VarStore=New(), CurrentRunRef=&childRef}
  │
  ├── 5. 创建本地 channel ch（缓冲 256）：
  │       go func() { childTmpl.RunLoop(childCtx, sub, ch, nil, st); close(ch) }()
  │       ← 独立 goroutine，事件写入本地 ch，父端不可见
  │
  ├── 6. 本地事件循环（消费所有子事件）：
  │     for ev := range ch:
  │       ├── EventStart → spec.OutputCh(非阻塞转发 agent_transfer start)  ← v0.9.4 新增
  │       ├── EventQueryEnd → 提取 last.Outcome → spec.OutputCh(转发 agent_transfer end)
  │       ├── EventToolCallStart → 记录 pendingTC
  │       ├── EventToolCallEnd → 追加 ChildToolCall
  │       ├── EventAnswer / CallLLM* → 丢弃（隔离规则）
  │       └── childCtx.Done() → last==nil 时转发 agent_transfer(OK=false)
  │
  └── 7. 返回 SpawnResult{FinalText, Metrics, ChildToolCalls, Status}
```

### 3.3 事件隔离原则

| 子事件                      | 转发到父端                                 | 说明                          |
| ------------------------ | ------------------------------------- | --------------------------- |
| `EventStart`             | ✅ → `agent_transfer(TransferStart)`   | 通过 `SpawnSpec.OutputCh` 非阻塞 |
| `EventQueryEnd`          | ✅ → `agent_transfer(TransferEnd, OK)` | 同上                          |
| `EventAnswer`            | 🚫 丢弃                                 | ReAct 中间文本隔离                |
| `EventCallLLMStart/End`  | 🚫 丢弃                                 | 子模型调用隔离                     |
| `EventToolCallStart/End` | 🚫 仅本地记录                              | 聚合进 `ChildToolCalls`        |

### 3.4 强制非流式 LLM

子 Agent 的 ChatModel 在构造时被 `WrapNonStream` 包装——所有 LLM 调用强制 `stream=false`。这是 `spawn-runtime-rules.md` §6 的硬约束：

```go
if childTmpl.ChatModel != nil {
    childTmpl.ChatModel = model.WrapNonStream(childTmpl.ChatModel)
}
```

**原因**：子 Agent 的事件流不进入父端，父端没有流式消费的必要；强制非流式降低网络开销和 token 消耗。

***

## 4. engineSpawner：引擎层注入

`internal/engine/spawn.go`：

```go
type engineSpawner struct {
	inner    Spawner    // 被包装的 DefaultSpawner
	runState *RunState  // 当前运行状态
}

func NewEngineSpawner(inner Spawner, runState *RunState) *engineSpawner
```

engineSpawner 在 `Spawn()` 中注入引擎层逻辑：

```
engineSpawner.Spawn(ctx, parent, spec)
  │
  ├── 1. childCtx, childCancel = context.WithCancel(ctx)
  │
  ├── 2. result, err = s.inner.Spawn(childCtx, parent, spec)  ← 委托 DefaultSpawner
  │
  ├── 3. handle = NewSpawnHandle(result.ChildRunRef, childCancel)
  │     └── s.runState.Children.Register(runID, handle)       ← 注册到 ChildRegistry
  │     └── s.runState.Children.Unregister(runID)             ← 立即注销（ephemeral）
  │
  └── 4. s.runState.BudgetCounter.Consume(result.Metrics.TotalTokens, 0)
        ← 子 Agent token 消耗滚入树级预算
```

Runner 在每次执行 Agent 前注入此包装：

```go
inner := agent.NewDefaultSpawner(exec, maxD)
exec.Spawner = engine.NewEngineSpawner(inner, runState)
```

***

## 5. spawn\_subagent 工具

`pkg/agent/spawn_tool.go`：

```go
func buildSpawnSubagentVarTool(parent, parentTemplate, req, loop, eventCh) *model.ToolInfo
```

`spawn_subagent` 作为注入工具（`varTools` / extraRuntime），不参与 `ValidateBindings`。启用条件：

```go
ag := agent.New(chat,
	agent.WithSpawn(func(spec *exchange.SpawnSpec) *Agent {
		// 根据 SpawnSpec 构造子 Agent 模板
		return agent.New(chat, agent.WithName("child"))
	}),
)
```

### 5.1 同步与异步两条路径

`makeSpawnHandler` 根据 `eventCh` 参数选择路径：

**同步路径**（`eventCh == nil`）：

```
sp.Spawn(ctx, parent, spec)
  → 阻塞等待子 Agent 完成
  → 返回 SpawnResult JSON 作为 tool_result 给父 LLM
父 LLM 立即看到子结果，在同一个 step 中继续推理
```

**异步路径**（`eventCh != nil`）：

```go
loop.AddAsyncSpawn()
go func() {
	defer loop.DoneAsyncSpawn()
	res, _ := sp.Spawn(ctx, parent, spec)
	loop.CollectAsyncSpawnResult(res.FinalText)  // 存入 LoopState
}()
return sonic.MarshalString(&exchange.SpawnResult{Status: SpawnCompleted})
// ← 立即返回空结果，父 LLM 继续推理
```

父 Agent 在**无 tool\_calls 即将结束**时：

```
WaitAsyncSpawns()                    ← 等待所有后台子 Agent 完成
FlushAsyncSpawnResults()             ← 取出子结果
→ 注入为 user message: "The spawned subtask finished: {result}"
→ continue（再走一轮 LLM 总结）
```

***

## 6. 硬限制与安全边界

| 限制                      | 实现                                 | 说明                                             |
| ----------------------- | ---------------------------------- | ---------------------------------------------- |
| **MaxDepth**            | `s.MaxDepth`（默认 2）                 | `parent.Depth+1 >= MaxDepth` → `SpawnRejected` |
| **MaxConcurrentSpawns** | `LoopPolicy.MaxConcurrentSpawns()` | 异步路径限制同时未完成的子 Agent 数                          |
| **Budget**              | `RunState.BudgetCounter`           | 树级共享，子消耗后滚入总账；超限返回 `SpawnRejected`             |
| **AllowChildSpawn**     | `spec.AllowChildSpawn`             | 默认 false，逐级显式授权，不传递不继承                         |
| **非流式 LLM**             | `WrapNonStream`                    | 子 Agent 强制非流式，与父配置解耦                           |
| **Lifecycle**           | 仅 `ephemeral`                      | `linger` 预留但返回 `SpawnRejected`                 |

***

## 7. SpawnHandle 与 ChildRegistry

`internal/engine/spawn_handle.go`：

```go
type SpawnHandle struct {
	Ref    exchange.RunRef       // 子运行引用
	state  atomic.Int32          // Running | Complete | Failed | Cancelled
	cancel context.CancelFunc    // 取消函数
	done   chan struct{}         // goroutine 退出时关闭
	result atomic.Value          // *exchange.SpawnResult
}
```

关键方法：

| 方法                 | 用途                        |
| ------------------ | ------------------------- |
| `Shutdown(reason)` | 强制终止子 Agent（父结束时调用）       |
| `Done()`           | 等待子 Agent 退出              |
| `State()`          | 查询当前状态                    |
| `SetResult(res)`   | 存储最终结果（子 goroutine 退出时调用） |
| `Result(ctx)`      | 阻塞等待结果                    |

`ChildRegistry`（`child_registry.go`）维护 `runID → SpawnHandle` 映射，关键方法 `TerminateAll(reason)` 在父结束时级联取消所有子 Agent。

***

## 8. 完整数据流

```
父 Agent RunLoop
  │
  ├── Step N: LLM 调用 → tool_calls: [{name: "spawn_subagent", arguments: {task, ...}}]
  │
  ├── makeSpawnHandler(eventCh):
  │     │
  │     ├── spec = {Task, SystemAddendum, SkillIDs, ..., OutputCh=eventCh}
  │     │
  │     ├── 异步（eventCh != nil）:
  │     │     loop.AddAsyncSpawn()
  │     │     go sp.Spawn(ctx, parent, spec) → 后台 goroutine
  │     │     立即返回 fake SpawnResult → 父 LLM 继续推理
  │     │
  │     └── 同步（eventCh == nil）:
  │           result = sp.Spawn(ctx, parent, spec) → 阻塞等待
  │           → tool_result JSON 给父 LLM
  │
  └── [子 Agent goroutine]
        │
        ├── DefaultSpawner.Spawn():
        │     ├── 校验 depth / lifecycle
        │     ├── 构造子 Agent（Clone + system_addendum + 非流式）
        │     ├── go childTmpl.RunLoop(localCh)
        │     └── 本地事件循环：
        │           EventStart → OutputCh(agent_transfer start)  ← v0.9.4
        │           tool_call_start/end → 记录 ChildToolCalls
        │           QueryEnd → 提取 Outcome → OutputCh(agent_transfer end)
        │           answer/call_llm → 丢弃
        │
        └── engineSpawner.Spawn():
              ├── inner.Spawn() → DefaultSpawner 完成
              ├── ChildRegistry.Register + Unregister（ephemeral 立即清理）
              └── BudgetCounter.Consume(tokens)

父 Agent:
  │
  ├── 异步路径：无 tool_calls 时 → WaitAsyncSpawns()
  │     → FlushAsyncSpawnResults()
  │     → user message: "spawned subtask finished: {result}"
  │     → continue（LLM 总结）
  │
  └── emit(query_end, Metrics=父+子累计)
```

***

## 9. 与代码目录的对应

| 文件                                  | 职责                                                        |
| ----------------------------------- | --------------------------------------------------------- |
| `pkg/agent/spawn.go`                | DefaultSpawner：子 Agent 构造、隔离运行、事件过滤、SpawnResult 组装        |
| `pkg/agent/spawn_tool.go`           | buildSpawnSubagentVarTool、makeSpawnHandler（同步/异步两路径）      |
| `internal/engine/spawn.go`          | engineSpawner：包装注入 ChildRegistry + BudgetCounter          |
| `internal/engine/spawn_handle.go`   | SpawnHandle：子 Agent 控制句柄（Shutdown / Done / Result）        |
| `internal/engine/child_registry.go` | ChildRegistry：子 Agent 注册表（Register / TerminateAll）        |
| `internal/engine/policy.go`         | LoopPolicy：MaxDepth / MaxConcurrentSpawns / BudgetCounter |
| `pkg/runtime/exchange/exchange.go`  | SpawnSpec / SpawnResult / ChildToolCall / RunRef          |
| `pkg/agent/agent.go`                | SpawnEnabled / ChildAgentBuilder / Spawner 接口字段           |
| `pkg/agent/options.go`              | WithSpawn                                                 |
| `pkg/runner/runner.go`              | 注入 engineSpawner 到 Agent                                  |

***

## 10. 相关文档

| 文档                                      | 关系                                                |
| --------------------------------------- | ------------------------------------------------- |
| [01-agent-core.md](01-agent-core.md) §5 | RunLoop 中 `executeToolCalls` 如何调用 spawn\_subagent |
| [04-tools.md](04-tools.md) §2.3         | `spawn_subagent` 作为注入工具                           |
| [07-transfer.md](07-transfer.md) §1.2   | Spawn vs Transfer 对比                              |
| [03-events.md](03-events.md) §2.8       | `agent_transfer` 事件在 spawn 场景的载荷                  |

***

# 变更日志

| 日期         | 版本     | 变更说明                                                                                            |
| ---------- | ------ | ----------------------------------------------------------------------------------------------- |
| 2026-04-30 | v0.9.5 | 初稿：Spawn 概念、两层 Spawner、子 Agent 隔离与事件过滤、同步/异步两路径、engine 层注入、硬限制、SpawnHandle/ChildRegistry、完整数据流。 |


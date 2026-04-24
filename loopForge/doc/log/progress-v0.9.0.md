# loopForge 项目进度日志 — v0.9.0

> **版本**：v0.9.0  
> **日期**：2026-04-24（计划稿）  
> **里程碑**：**核心约束** — 子 Agent 非流式包装、父取消级联子、全树预算共享、engine 实装对接  
> **上一版本**：[v0.8.0](progress-v0.8.0.md)（Slice1 最小 spawn 实现）  
> **下一版本**：[v0.9.1](progress-v0.9.1.md)（工具与事件）  
> **设计依据**：`doc/design/spawn-runtime-rules.md` §6（非流式）、§2（级联取消）、§3.8（预算）  
> **配套实施计划**：`doc/plan/plan-v0.9.0.md`

---

## 本版本目标

1. **子 ChatModel 非流式包装**：实现 `wrapNonStream` 强制子 LLM 调用为 `stream=false`，对齐 §6；trace/日志可见 `llm_mode=non_stream`。
2. **父取消级联子**：父 Run 终态时所有子（含等待唤醒态）立即取消，不等待子 loop 当前回合跑完；实现 `RunState.Terminate(reason)`。
3. **全树预算共享**：根 Run 持有 `BudgetCounter`（tokens + USD）；子树共享同一计数器；超额返回 `SpawnError{Code: "budget_exceeded"}`。
4. **MaxConcurrentSpawns**：控制并发子数上限。
5. **internal/engine 实装对接**：完整实现 `Spawner` / `LoopPolicy` / `RunState` 与 `pkg/runner` 衔接（或薄适配层转调 `pkg/agent` 实现）。

---

## 现状与边界（相对 v0.8.0）

| 层次 | 现状（计划起点） |
|------|-----------------|
| **非流式** | Slice1 子 Agent 仍复用父流式配置；`pkg/model` 的 `ChatModel` 接口含 `Generate` 和 `Stream` 两个方法，子路径无强制非流式包装 |
| **级联取消** | `defaultSpawner` 在独立 goroutine 中运行子 `RunLoop`，父 ctx 取消时子 ctx 逐级传递；但 `RunState.Terminate` 尚未在父终态路径上被调用，`internal/engine` 的 `RunState` 尚无 `Children` map 和 `Terminate` 方法 |
| **预算** | `internal/defaults/loop.go` 定义了 `SpawnBudgetTokenSubtree = 0`（软限制禁用），无 `BudgetCounter` 结构体实现，无全树共享约束 |
| **MaxConcurrentSpawns** | `defaults.MaxConcurrentSpawnsDefault = 4` 已有定义，但 `LoopPolicy.AllowSpawn` 接口方法中尚未实现并发校验 |
| **engine 实装** | `internal/engine` 的 `Spawner` 接口定义 `Spawn(ctx, parent, spec) (*exchange.SpawnResult, error)` 与 `pkg/agent.Spawner` 重复定义；`LoopPolicy` 接口含 `AllowStep/AllowSpawn/MaxSteps/MaxSpawnDepth` 但无 `MaxConcurrentSpawns` 方法；`RunState` 仅有 `RunID/Step/LastError/Termination/ParentRunID/Depth/ActiveChildRuns` 7 个字段；任一接口均无具体实现 |

---

## 方案细节

### 阶段 A — 非流式包装

#### 当前状态分析

`pkg/model` 现有两层接口：

```go
type BaseChatModel interface {
    Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error)
    Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error)
}
type ToolCallingChatModel interface {
    BaseChatModel
    WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error)
}
```

`pkg/agent/spawn.go` 的 `defaultSpawner.Spawn` 当前通过 `childTmpl` 直接调用子 `RunLoop`，子路径无独立 ChatModel 包装。子 Agent 的 ChatModel 与父 Agent 共享同一实例或同一配置。

#### 实现路径

创建 `pkg/model/nonstream.go`，实现 `wrapNonStream`：

```go
// nonStreamModel 包装 ToolCallingChatModel，强制 Generate 使用 stream=false 的内部调用，
// 并将 Stream 方法降级为调用 Generate（非流式）。
type nonStreamModel struct {
    inner model.ToolCallingChatModel
}

func wrapNonStream(chat model.ToolCallingChatModel) model.ToolCallingChatModel {
    return &nonStreamModel{inner: chat}
}

// Generate 直接委托给 inner.Generate（已是非流式）
func (m *nonStreamModel) Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error) {
    return m.inner.Generate(ctx, input, opts...)
}

// Stream 降级：调用 Generate 后将单条消息包装为已结束的流读取器
func (m *nonStreamModel) Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error) {
    msg, err := m.inner.Generate(ctx, input, opts...)
    if err != nil {
        return nil, err
    }
    return &completedStreamReader{msg: msg}, nil
}

func (m *nonStreamModel) WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error) {
    inner, err := m.inner.WithTools(tools)
    if err != nil {
        return nil, err
    }
    return &nonStreamModel{inner: inner}, nil
}
```

**接入点**：在 `defaultSpawner.Spawn` 中，构造子 Agent 后执行 `wrapNonStream`：

```go
// 在 applySpecToChildAgent 之后：
if cm := childTmpl.ChatModel; cm != nil {
    childTmpl.ChatModel = wrapNonStream(cm).(model.ToolCallingChatModel)
}
```

**关键决策**：
- `SpawnSpec.AllowStream` 字段**保留但不读取**，引擎始终 `stream=false`
- `nonStreamModel` 的 `Stream` 降级实现应记录一条 debug 日志，便于追踪「子级尝试流式」的情况
- trace 字段：在 `pkg/log` 或 OTel span 中设置 `llm_mode=non_stream`

### 阶段 B — 子 Agent 追踪方案与即时关闭机制

阶段 B 是整个 v0.9.0 最核心的改动，分为两个正交子问题：
1. **追踪方案** — 父 Agent 如何知道有哪些活跃的子 Agent，以及如何与子交互
2. **即时关闭机制** — 不等待子 Agent 当前 loop 回合完成即可终止

---

#### B.1 问题分析：为什么当前无法即时关闭子 Agent？

在 v0.8.0 的 `defaultSpawner.Spawn`（[spawn.go](../../../pkg/agent/spawn.go#L33)）中，子 Agent 的生命周期管理有三个阻塞盲点：

```
┌─ 父 Agent 的 RunLoop ─────────────────────────────────┐
│                                                        │
│  step N:                                                │
│    consumeStream(ctx, ...)       ← 盲点 #2: stream.Recv│
│      ↓                                                  │
│    executeToolCalls(ctx, ...)    ← 盲点 #3: 工具间     │
│      ↓                                                  │
│    [spawn_subagent handler]                             │
│      ↓                                                  │
│    sp.Spawn(ctx, ...)                                   │
│      ├─ go child.RunLoop(ctx, ...)   ← 共享父 ctx      │
│      └─ for ev := range ch          ← 盲点 #1: 无 ctx  │
│          阻塞，直到 ch close                               │
│                                                        │
└────────────────────────────────────────────────────────┘
```

**盲点 #1 — `for ev := range ch`（[spawn.go:107](../../../pkg/agent/spawn.go#L107)）**

```go
// 当前代码：无 ctx 感知，必须等 ch close 才返回
for ev := range ch {
    // 处理事件、收集 outcome
}
// 子 goroutine 的 defer close(ch) 执行后才返回
```

即使子 `RunLoop` 因 ctx 取消已经快速返回，父侧 `Spawn()` 仍然**阻塞在 `for range ch` 上**，必须等子 goroutine 的 `defer close(ch)` 执行后才返回。这个子 goroutine 退出到 ch close 的间隔是微秒级的——但关键问题是：子 `RunLoop` **本身退出有多快？**

**盲点 #2 — `stream.Recv()` 循环不检查 ctx（[stream.go:40](../../../pkg/agent/stream.go#L40)）**

```go
// 当前代码：stream.Recv() 在两 chunk 之间不检查 ctx
for {
    chunk, recvErr := stream.Recv()
    // ...
}
// 如果 LLM 流在发送长回复，中间可能数秒没有新 chunk
// ctx.Done() 在这期间不会被检测到
```

**盲点 #3 — `executeToolCalls` 工具之间不检查 ctx（[tools.go:25](../../../pkg/agent/tools.go#L25)）**

```go
// 当前代码：工具调用之间不检查 ctx
for i := range toolCalls {
    // 如果第一个工具是 spawn，卡在盲点 #1
    // 第二个工具即使 ctx 已取消，仍会被执行
    content, err := tool.Invoke(ctx, infos, executor, tc)
}
```

**三个盲点叠加产生的延迟：**

| 场景 | 父 Terminate 到子退出的延迟瓶颈 |
|------|-------------------------------|
| 子正在 LLM 流接收中 | `stream.Recv()` 阻塞（等待下个 chunk / 超时），最多 `LLM_API_TIMEOUT` |
| 子正在执行无视 ctx 的工具 | 工具执行完才返回（如 `time.Sleep`、阻塞 IO），最多 `ToolTimeout` |
| 子无阻塞操作 | `for range ch` 阻塞到子 goroutine `defer close(ch)`，亚毫秒级 |
| 最佳情况 | < 1ms（子检测到 ctx Done 并返回） |
| 典型情况 | < 500ms（HTTP 请求因 ctx 取消而中断） |
| 最坏情况 | `LLM_API_TIMEOUT` 或 `ToolTimeout`（默认可能 30-60s） |

---

#### B.2 追踪方案：ChildRegistry + SpawnHandle

##### B.2.1 SpawnHandle 带生命周期状态

`s internal/engine/spawn_handle.go`：

```go
package engine

import (
    "context"
    "sync"
    "sync/atomic"

    "loopforge/pkg/runtime/exchange"
    "loopforge/pkg/runtime/outcome"
)

// ChildState 描述子 Agent 当前的生命周期阶段。
type ChildState int32

const (
    ChildStateRunning   ChildState = iota // 子 RunLoop 正在运行
    ChildStateComplete                     // 子 RunLoop 正常结束
    ChildStateFailed                       // 子 RunLoop 异常结束
    ChildStateCancelled                    // 被父级 Terminate 终止
)

// SpawnHandle 是父侧持有子 Agent 的控制句柄。
// 父可通过它取消子、查询生命周期状态、获取结果。
type SpawnHandle struct {
    Ref     exchange.RunRef
    state   atomic.Int32  // ChildState

    cancel   context.CancelFunc  // 取消子 ctx
    done     chan struct{}       // close 表示 goroutine 已退出
    result   atomic.Value       // *exchange.SpawnResult（终态后可用）

    closeOnce sync.Once
}

func NewSpawnHandle(ref exchange.RunRef, cancel context.CancelFunc) *SpawnHandle {
    h := &SpawnHandle{
        Ref:    ref,
        cancel: cancel,
        done:   make(chan struct{}),
    }
    h.state.Store(int32(ChildStateRunning))
    return h
}

// Shutdown 向子 Agent 发出取消信号，不等待子 goroutine 退出。
// 符合 §2：父终态时所有子立即取消，不等待当前回合。
func (h *SpawnHandle) Shutdown(reason outcome.TerminationReason) {
    h.closeOnce.Do(func() {
        h.state.Store(int32(ChildStateCancelled))
        h.cancel()
        close(h.done)
    })
}

// Done 返回一个 channel，子 goroutine 退出时关闭。
func (h *SpawnHandle) Done() <-chan struct{} { return h.done }

// State 返回子当前生命周期状态（线程安全）。
func (h *SpawnHandle) State() ChildState {
    return ChildState(h.state.Load())
}

// SetResult 在子 goroutine 退出时被调用，回填终态结果。
func (h *SpawnHandle) SetResult(res *exchange.SpawnResult) {
    h.result.Store(res)
    // 如果还未被 Shutdown，标记终态
    if h.State() == ChildStateRunning {
        s := ChildStateComplete
        if res.Status == exchange.SpawnFailed {
            s = ChildStateFailed
        }
        h.state.Store(int32(s))
    }
    h.closeOnce.Do(func() {
        close(h.done)
    })
}

// Result 阻塞等待子完成后返回结果。
func (h *SpawnHandle) Result(ctx context.Context) (*exchange.SpawnResult, error) {
    select {
    case <-h.done:
        if v := h.result.Load(); v != nil {
            return v.(*exchange.SpawnResult), nil
        }
        return nil, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

关键设计要点：
- **`Shutdown` vs `SetResult` 幂等**：`closeOnce` 保证 `done` channel 只关闭一次。`Shutdown` 和 `SetResult` 都可能触发关闭，通过 atomic state 确保谁先到就记录谁的状态。
- **State 原子操作**：`ChildState` 用 `atomic.Int32` 而非 mutex，避免 SPSC 场景的锁竞争。
- **`Result` 方法的 ctx 参数**：允许调用方传入外部 ctx，支持主动等待超时。如果 handle 已经被 `Shutdown`，`<-h.done` 立即返回。
- **不引入 inbox 通道**：v0.9.0 的 `SpawnHandle` 没有 `inbox chan WakeMessage`。通道铺设推迟到 v0.9.2 linger 需求。

##### B.2.2 ChildRegistry（追踪注册表）

`internal/engine/child_registry.go`：

```go
package engine

import (
    "sync"

    "loopforge/pkg/runtime/outcome"
)

// ChildRegistry 以线程安全方式追踪父 Run 的所有活跃子 Agent。
// 嵌入在 RunState 中使用。
type ChildRegistry struct {
    mu       sync.Mutex
    children map[string]*SpawnHandle  // key = child RunID
}

func NewChildRegistry() *ChildRegistry {
    return &ChildRegistry{children: make(map[string]*SpawnHandle)}
}

// Register 将新 spawn 的子句柄录入追踪表。
func (r *ChildRegistry) Register(id string, h *SpawnHandle) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.children[id] = h
}

// Unregister 在子 goroutine 退出时移出追踪表。
// 返回子句柄，供后续 metrics 归集。
func (r *ChildRegistry) Unregister(id string) *SpawnHandle {
    r.mu.Lock()
    defer r.mu.Unlock()
    h := r.children[id]
    delete(r.children, id)
    return h
}

// TerminateAll 遍历所有活跃子级，逐一调用 Shutdown。
// 返回被终止的子级数量。这是级联取消的入口。
func (r *ChildRegistry) TerminateAll(reason outcome.TerminationReason) int {
    r.mu.Lock()
    snapshot := r.children
    r.children = make(map[string]*SpawnHandle)
    count := len(snapshot)
    r.mu.Unlock()
    for _, h := range snapshot {
        h.Shutdown(reason)
    }
    return count
}

// ActiveCount 返回当前活跃子数（用于 MaxConcurrentSpawns 校验）。
func (r *ChildRegistry) ActiveCount() int {
    r.mu.Lock()
    defer r.mu.Unlock()
    return len(r.children)
}

// Snapshot 快照当前所有子级信息（用于日志/观测）。
func (r *ChildRegistry) Snapshot() []ChildInfo {
    r.mu.Lock()
    defer r.mu.Unlock()
    infos := make([]ChildInfo, 0, len(r.children))
    for id, h := range r.children {
        infos = append(infos, ChildInfo{
            RunID: id,
            Depth: h.Ref.Depth,
            State: h.State(),
        })
    }
    return infos
}

type ChildInfo struct {
    RunID string     `json:"run_id"`
    Depth int        `json:"depth"`
    State ChildState `json:"state"`
}
```

##### B.2.3 增强后的 RunState

```go
// internal/engine/runstate.go

type RunState struct {
    RunID           string
    Step            int
    LastError       error
    Termination     outcome.TerminationReason
    ParentRunID     string
    Depth           int
    ActiveChildRuns int
    AllowSpawn      bool  // 该 Run 是否被授权再次 spawn
    
    // 新增：子 Agent 追踪注册表
    Children *ChildRegistry
    
    // 新增：全树预算计数器（根 Run 持有，子树指针共享）
    BudgetCounter *BudgetCounter
}

// Terminate 是 §2 的入口：父 Run 进入终态时，级联取消所有子级。
// 调用后所有子 goroutine 收到 ctx 取消信号。
func (r *RunState) Terminate(reason outcome.TerminationReason) {
    r.Termination = reason
    if r.Children != nil {
        r.Children.TerminateAll(reason)
    }
    r.ActiveChildRuns = r.Children.ActiveCount()
}
```

> `ActiveChildRuns` 现在作为缓存字段，由 `Children.ActiveCount()` 提供真实值。

---

#### B.3 即时关闭机制：修复三个 ctx 盲点

##### B.3.1 修复盲点 #1：spawn.go 的 `for range ch` → 带 ctx 的 select

```go
// pkg/agent/spawn.go — Spawn 方法中的事件收集循环

// 派生独立子 ctx（不是直接复用父 ctx）
childCtx, childCancel := context.WithCancel(ctx)
defer childCancel()

// ... 构造 handle、启动 goroutine ...

// 修复前：
// for ev := range ch { ... }

// 修复后：带 ctx 感知的 select
var last *outcome.RuntimeOutcome

eventLoop:
for {
    select {
    case ev, ok := <-ch:
        if !ok {
            // ch closed → 子 goroutine 已退出
            break eventLoop
        }
        if qe := ev.QueryEnd(); qe != nil && qe.Outcome != nil {
            last = qe.Outcome
        }
    case <-childCtx.Done():
        // 父 ctx 取消（级联）或子自身超时
        // 不等待 ch close，直接退出
        break eventLoop
    }
}

// 构造返回结果（可能是 cancelled，可能是正常完成）
if last == nil {
    status := exchange.SpawnFailed
    code := "cancelled"
    if childCtx.Err() != nil {
        code = "parent_cancelled"
    }
    return &exchange.SpawnResult{
        ChildRunRef: childRef,
        Status:      status,
        Error:       &exchange.SpawnError{Code: code, Message: "child run terminated early"},
    }, nil
}
// ... 正常构造结果 ...
```

**关键变化**：
- 使用 `childCtx` 而非 `ctx`，让级联取消可以独立于父原始 ctx
- `select { case ev, ok := <-ch: ... case <-childCtx.Done(): ... }` 打破死阻塞
- 当盲点 #2/#3 延迟解决后，childCtx 被取消 → 子 RunLoop 退出 → ch close → select 立即感知

##### B.3.2 修复盲点 #2：stream.go 的 Recv 循环插 ctx 检查

```go
// pkg/agent/stream.go — consumeStream

for {
    // 每轮 Recv 前检查 ctx
    select {
    case <-ctx.Done():
        return streamResult{Err: ctx.Err()}
    default:
    }

    chunk, recvErr := stream.Recv()
    if recvErr == io.EOF {
        break
    }
    if recvErr != nil {
        return streamResult{Err: recvErr}
    }
    // ... 处理 chunk ...
}
```

> **关于中断流式 HTTP 请求的说明**：
> 如果底层 LLM 客户端在 HTTP 调用中传入了 ctx，那么 `stream.Recv()` 正在等待 TCP 数据时 ctx 取消会立即中断连接（Go 的 `net/http` 在 ctx 取消时关闭 TCP 连接）。这个机制下，即使在 `Recv()` 调用内部 ctx 被取消，延迟也在 `SO_TIMEOUT` 级别（通常 200ms），而非真正的即时。
> 
> 如果底层客户端没有传入 ctx，则 `select { case <-ctx.Done() }` 在 `Recv()` 返回后才会命中。此时延迟取决于 chunk 到达间隔。确认下游 LLM 客户端是否正确传递 ctx 是测试要点。

##### B.3.3 修复盲点 #3：executeToolCalls 的工具间插 ctx 检查

```go
// pkg/agent/tools.go — executeToolCalls

for i := range toolCalls {
    // 每条工具调用前检查 ctx
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    tc := toolCalls[i]
    emit(step, &event.ToolCallStartPayload{...})
    
    content, err := tool.Invoke(ctx, infos, executor, tc)
    // ...
}
```

> 工具执行本身（`tool.Invoke`）通过 ctx 参数传递取消信号。`spawn_subagent` 工具的执行路径是：`tool.Invoke` → `tc.Handle(ctx, ...)` → `makeSpawnHandler` → `sp.Spawn(ctx, ...)`。由于 Spawn 内部使用了 `childCtx`（派生自传入的 ctx），级联取消会逐级传递。
>
> 对于 MCP 等外部工具，ctx 取消通过 HTTP 请求中断。对于无视 ctx 的同步工具（如 `time.Sleep`），将在工具超时后返回。这类工具的超时参数应在 `ToolInfo` 层配置。

##### B.3.4 完整级联取消时序图

```ascii
时间轴 →
═══════════════════════════════════════════════════════════════════

Runner 层                   父 RunState             子 SpawnHandle         子 RunLoop (goroutine)
  │                           │                        │                      │
  │  完成/取消/错误            │                        │                      │
  │ ─────────────────────────→│                        │                      │
  │                           │                        │                      │
  │                     Terminate(reason)              │                      │
  │                           │                        │                      │
  │                     Children.TerminateAll()        │                      │
  │                           │                        │                      │
  │                           │  ─────────────────────→│                      │
  │                           │                        │                      │
  │                           │                  Shutdown()                   │
  │                           │                        │                      │
  │                           │                childCancel()                  │
  │                           │                        │  ──────────────────→│
  │                           │                        │                      │
  │                           │                        │               [ctx cancelled]
  │                           │                        │                      │
  │                           │                        │   ┌──────────────────┤
  │                           │                        │   │ 检查当前阻塞点   │
  │                           │                        │   │                  │
  │                           │                        │   ├─ 在 consumeStream │
  │                           │                        │   │  stream.Recv()    │
  │                           │                        │   │  → HTTP 中断      │
  │                           │                        │   │  → 返回 ctx 错误  │
  │                           │                        │   │                  │
  │                           │                        │   ├─ 在 tool.Invoke  │
  │                           │                        │   │  工具 Handle 获取  │
  │                           │                        │   │  ctx.Done         │
  │                           │                        │   │  → 快速失败       │
  │                           │                        │   │                  │
  │                           │                        │   └─ 在上述之间      │
  │                           │                        │     select {ctx.Done}│
  │                           │                        │     → 立即命中       │
  │                           │                        │                      │
  │                           │                        │   RunLoop 返回       │
  │                           │                        │   defer 链执行       │
  │                           │                        │                      │
  │                           │                        │   close(ch)          │
  │                           │                        │  ←──────────────────│
  │                           │                        │                      │
  │                     Unregister(child)              │                      │
  │                           │  ←─────────────────────│                      │
  │                           │                        │                      │
  │  父侧 Spawn() 返回        │                        │                      │
  │  ←────────────────────────│                        │                      │
  │                           │                        │                      │
```

---

#### B.4 兜底与安全：Runaway Child Detection

即使修复了三个盲点，仍存在子 Agent 不响应 ctx 取消的最坏情况：

| 场景 | 原因 | 应对 |
|------|------|------|
| LLM HTTP 客户端未传 ctx | `stream.Recv()` 不感知 ctx | 确认所有 LLM provider 实现传入 ctx；单元测试 mock 验证 |
| 同步工具完全阻塞 | 如 `time.Sleep(30s)`、阻塞 IO | watchdog 超时后强制回收 |
| goroutine 死循环 | 子 RunLoop 的 for step 无意料之外的退出路径 | 父 ctx 取消 → `select {case <-ctx.Done()}` 在 for step 开头命中 |

**Watchdog 兜底**：

```go
// 在 Terminate 之后启动 watchdog，防止子 goroutine 泄漏
func (r *RunState) TerminateWithGuard(reason outcome.TerminationReason, guardTimeout time.Duration) {
    r.Terminate(reason)
    
    // snapshot all handles for watchdog
    handles := r.Children.children  // internal access for simplicity
    
    for _, h := range handles {
        go func(h *SpawnHandle) {
            select {
            case <-h.Done():
                // 正常退出
            case <-time.After(guardTimeout):
                // 子未响应 ctx 取消，日志告警
                log.Default().Warn("child run did not respond to cancellation within guard timeout",
                    "child_run_id", h.Ref.RunID,
                    "parent_run_id", h.Ref.ParentRunID,
                    "state", h.State(),
                    "guard_timeout", guardTimeout,
                )
            }
        }(h)
    }
}

const DefaultGuardTimeout = 5 * time.Second
```

> **设计决策**：watchdog 只记录告警日志而不强制终止（Go 无法安全地终止一个 goroutine）。避免泄漏的最终防线是子 RunLoop 自身的 ctx 感知正确性。watchdog 超时意味着代码有 bug，需要修复盲点而非强杀。

---

#### B.5 子 Agent 的可观测性（Tracing）

##### B.5.1 结构化日志字段

| 事件 | slog 级别 | 字段 |
|------|-----------|------|
| spawn 被调用 | Debug | `spawn.child_run_id`, `spawn.parent_run_id`, `spawn.depth`, `spawn.lifecycle` |
| spawn 被策略拒绝 | Info | 原因 + `spawn.reject_reason`（depth/budget/unsupported_lifecycle） |
| 子 RunLoop 启动 | Info | `spawn.child_run_id`, `spawn.model`, `spawn.llm_mode=non_stream` |
| 子 RunLoop 结束 | Info | `spawn.child_run_id`, `spawn.status`, `spawn.duration_ms`, `spawn.tokens`, `spawn.cost_usd` |
| 级联取消 | Warn | `spawn.child_run_id`, `spawn.cancel_reason`, `spawn.state_at_cancel` |
| watchdog 超时 | Error | `spawn.child_run_id`, `spawn.guard_timeout` |
| ChildRegistry 快照 | Debug | `spawn.active_count`, `spawn.children` |

##### B.5.2 OTel Span 结构

```go
// spawn span 属性（用于 tracing 关联父子 run）
type SpawnSpanAttrs struct {
    ChildRunID    string `otel:"spawn.child_run_id"`
    ParentRunID   string `otel:"spawn.parent_run_id"`
    Depth         int    `otel:"spawn.depth"`
    Lifecycle     string `otel:"spawn.lifecycle"`
    LLMMode       string `otel:"spawn.llm_mode"`       // "non_stream"
    ToolFilterCnt int    `otel:"spawn.tool_filter_cnt"` // 0（v0.9.1 才填充）
    BudgetTokens  int64  `otel:"spawn.budget_tokens"`   // 全树预算上限
}
```

父子 span 关系通过 `parent_run_id` → `run_id` 关联，无需直接的 span context 传递。在 OTel 中，子 RunLoop 的 span 创建时可显式指定父 span 的 `SpanContext`，形成正确的 trace tree。

##### B.5.3 RunState 观测接口

```go
// RunState 暴露给 Runner/Engine 的观测方法
func (r *RunState) SpawnInfo() SpawnInfo {
    snapshot := r.Children.Snapshot()
    return SpawnInfo{
        ActiveCount: len(snapshot),
        Children:    snapshot,
        BudgetUsed:  r.BudgetCounter.TokensUsed,
        BudgetMax:   r.BudgetCounter.TokensMax,
        CostUsed:    r.BudgetCounter.CostUSD,
        CostMax:     r.BudgetCounter.CostUSDMax,
    }
}

type SpawnInfo struct {
    ActiveCount int                  `json:"active_count"`
    Children    []ChildInfo          `json:"children"`
    BudgetUsed  int64                `json:"budget_tokens_used"`
    BudgetMax   int64                `json:"budget_tokens_max"`
    CostUsed    float64              `json:"cost_usd_used"`
    CostMax     float64              `json:"cost_usd_max"`
}
```

---

#### B.6 关键决策与边界原则

| # | 决策 | 理由 |
|---|------|------|
| 1 | `SpawnHandle` 使用 `closeOnce` 保证 `done` 只关闭一次 | `Shutdown` 和 `SetResult` 都可能触发关闭（先到先得），必须幂等 |
| 2 | ChildRegistry 使用快照而非逐 Child 加锁 | 防止 `TerminateAll` 遍历时持有锁时间过长（子 goroutine 并发调用 Unregister 可能死锁） |
| 3 | v0.9.0 不引入 `SpawnHandle.inbox` 通道 | inbox 通道只在 linger 模式（v0.9.2）需要。v0.9.0 的 `Shutdown` = `cancel()` |
| 4 | `for ev := range ch` → `select { ev, ok := <-ch; <-ctx.Done() }` | 必须给阻塞的 range 循环增加 ctx 感知路径，否则级联取消无法突破 spawn 返回 |
| 5 | Watchdog 只日志告警不强制终止 | Go 无法安全终止 goroutine。watchdog 超时 = 代码 bug 的红旗 |
| 6 | `ActiveChildRuns` 作为缓存字段，真值由 `ChildRegistry.ActiveCount()` 提供 | 避免两处状态不一致。ActiveChildRuns 可在策略校验中批量更新 |
| 7 | 子 ctx 派生自父 ctx（`context.WithCancel(ctx)`），而非共享 | 保证级联取消不影响父原始 ctx 的其他监听方 |
| 8 | Tracing 通过 `parent_run_id` 字段关联而非 OTel 的 span context 传递 | 子 goroutine 在 Spawn 中创建，天然可继承 span context。字段关联作为 fallback 兜底 |

### 阶段 C — 全树预算共享 + MaxConcurrentSpawns

#### BudgetCounter

```go
// pkg/runtime/exchange/exchange.go 或独立文件 pkg/runtime/budget/budget.go

type BudgetCounter struct {
    mu         sync.Mutex
    TokensUsed int64
    CostUSD    float64
    TokensMax  int64
    CostUSDMax float64
}

func NewBudgetCounter(tokensMax int64, costUSDMax float64) *BudgetCounter {
    return &BudgetCounter{
        TokensMax:  tokensMax,
        CostUSDMax: costUSDMax,
    }
}

// Allow 检查是否还能消耗指定数量的 tokens/cost。
// 若超额，返回 false 且不累加。
func (b *BudgetCounter) Allow(tokens int64, costUSD float64) bool {
    b.mu.Lock()
    defer b.mu.Unlock()
    newTokens := b.TokensUsed + tokens
    newCost := b.CostUSD + costUSD
    if b.TokensMax > 0 && newTokens > b.TokensMax {
        return false
    }
    if b.CostUSDMax > 0 && newCost > b.CostUSDMax {
        return false
    }
    return true
}

// Consume 累加已消耗的 tokens/cost。调用方须先 Allow 检查。
func (b *BudgetCounter) Consume(tokens int64, costUSD float64) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.TokensUsed += tokens
    b.CostUSD += costUSD
}
```

**默认值**（`internal/defaults/loop.go`）：

```go
const (
    BudgetTokensTreeDefault   = int64(100_000)   // 全树 100K tokens 上限
    BudgetUSDTreeDefault      = 0.5              // 全树 $0.50 上限
    MaxConcurrentSpawnsDefault = 4               // 默认 4 个并发子
)
```

> 注：`SpawnBudgetTokenSubtree = 0` 的旧常量保留但弃用，新增全树上限于 `BudgetTokensTreeDefault`。

**接入 LoopPolicy**：

```go
type LoopPolicy interface {
    AllowStep(state *RunState) bool
    AllowSpawn(parent *RunState, depth int) bool
    MaxSteps() int
    MaxSpawnDepth() int
    MaxConcurrentSpawns() int                     // 新增
    BudgetCounter() *BudgetCounter                // 新增
}
```

`AllowStep` 实现：

```go
func (p *defaultLoopPolicy) AllowStep(state *RunState) bool {
    if state.Step >= p.MaxSteps() {
        return false
    }
    bc := p.BudgetCounter()
    if bc != nil && !bc.Allow(estimatedTokensPerStep, 0) {
        return false
    }
    return true
}
```

`AllowSpawn` 实现：

```go
func (p *defaultLoopPolicy) AllowSpawn(parent *RunState, depth int) bool {
    if depth > p.MaxSpawnDepth() {
        return false
    }
    if parent.ActiveChildRuns >= p.MaxConcurrentSpawns() {
        return false
    }
    bc := p.BudgetCounter()
    if bc != nil && !bc.Allow(estimatedTokensPerSpawn, 0) {
        return false
    }
    return true
}
```

**关键决策**：
- 子 Run 完成后，`SpawnResult.Metrics` 中的 tokens/cost **逐级归入**父 BudgetCounter
- `estimatedTokensPerStep` 和 `estimatedTokensPerSpawn` 在 MVP 阶段使用保守估算值（如 1000 / 5000），后续改为精确累加
- 超额返回 `SpawnError{Code: "budget_exceeded"}`，附带剩余额度信息

### 阶段 D — internal/engine 实装对接

#### Spawner 完整实装

```go
// internal/engine/spawn.go

type defaultSpawner struct {
    agentFactory func(spec *exchange.SpawnSpec) pkg_agent.Runnable
    policy       LoopPolicy
    runState     *RunState
}

func (s *defaultSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error) {
    // 1. 策略校验
    if !s.policy.AllowSpawn(s.runState, parent.Depth+1) {
        return spawnRejected("budget_exceeded", "policy rejected spawn"), nil
    }

    // 2. 深度校验
    if parent.Depth+1 >= s.policy.MaxSpawnDepth() {
        return spawnRejected("max_depth", "exceeds max spawn depth"), nil
    }

    // 3. 生命周期校验：v0.9.0 仅支持 ephemeral
    if spec.Lifecycle != "" && spec.Lifecycle != exchange.LifecycleEphemeral {
        return spawnRejected("unsupported_lifecycle", "only ephemeral supported in v0.9.0"), nil
    }

    // 4. 构造子 Agent
    child := s.agentFactory(spec)
    child = applyChildOverrides(child, spec)

    // 5. 强制非流式
    child = wrapChildChatModel(child)

    // 6. 分配 RunID、维护 RunState
    childRef := allocChildRef(parent)
    childCtx, cancel := context.WithCancel(ctx)
    handle := &SpawnHandle{Ref: childRef, cancel: cancel, closed: make(chan struct{})}
    s.runState.Mu.Lock()
    s.runState.Children[childRef.RunID] = handle
    s.runState.ActiveChildRuns++
    s.runState.Mu.Unlock()

    // 7. 子 RunLoop 在独立 goroutine
    go func() {
        defer func() {
            s.runState.Mu.Lock()
            delete(s.runState.Children, childRef.RunID)
            s.runState.ActiveChildRuns--
            s.runState.Mu.Unlock()
            handle.Close()
        }()
        outcome := runChildLoop(childCtx, child, spec)
        // 归入全树预算
        if bc := s.policy.BudgetCounter(); bc != nil {
            bc.Consume(outcome.Metrics.TotalTokens, outcome.Metrics.TotalCostUSD)
        }
        // 回填 SpawnResult
        handle.result = outcomeToSpawnResult(childRef, outcome)
    }()

    // 8. 等待结果（v0.9.0 同步等待；v0.9.2 异步 handle）
    select {
    case <-childCtx.Done():
        return nil, childCtx.Err()
    case <-handle.Done():
        return handle.result, nil
    }
}
```

#### Runner 注入

在 `pkg/runner/runner.go` 中：

```go
type Runner struct {
    entryAgent    *agent.Agent
    spawner       engine.Spawner     // 新增
    loopPolicy    engine.LoopPolicy  // 新增
    // ...
}

func NewRunner(entryAgent *agent.Agent, opts ...RunnerOption) *Runner {
    r := &Runner{
        entryAgent:   entryAgent,
        maxTransfers: 10,
        // ...
    }
    for _, opt := range opts {
        opt(r)
    }
    if r.loopPolicy == nil {
        r.loopPolicy = engine.NewDefaultLoopPolicy()
    }
    return r
}
```

---

## 数据结构改造一览

| 结构 | v0.8.0 现状 | v0.9.0 改造 | 关联规则 |
|------|------------|-------------|----------|
| `RunState` | `RunID/Step/LastError/Termination/ParentRunID/Depth/ActiveChildRuns` | `Children` 改为 `*ChildRegistry`；新增 `AllowSpawn bool`、`BudgetCounter *BudgetCounter`；`Terminate` 委托 ChildRegistry.TerminateAll | §2 / §5 |
| `SpawnHandle` | 无 | 新增：`Ref/state(atomic.Int32)/cancel/done(chan)/result(atomic.Value)/closeOnce`；状态机：running→complete/failed/cancelled | §2 |
| `ChildRegistry` | 无 | 新增：`mu + map[string]*SpawnHandle`；`Register/Unregister/TerminateAll/ActiveCount/Snapshot` | §2 |
| `Spawner` (engine) | 接口定义 `Spawn(ctx, parent, spec) (*SpawnResult, error)` | 完整实装 `defaultSpawner`，整合策略校验/非流式/预算/tracking | §4 / §5 |
| `LoopPolicy` (engine) | `AllowStep/AllowSpawn/MaxSteps/MaxSpawnDepth` | 新增 `MaxConcurrentSpawns() int`、`BudgetCounter() *BudgetCounter`；默认实现 `defaultLoopPolicy` | §3.8 |
| `ChatModel` | `Generate/Stream` 两方法，子路径无独立包装 | 新增 `nonStreamModel` 包装器，`wrapNonStream` 函数 | §6 |
| `BudgetCounter` | 无 | 新增结构体：`Allow/Consume` 线程安全方法 | §3.8 |
| `defaults/loop.go` | `SpawnBudgetTokenSubtree = 0` | 新增 `BudgetTokensTreeDefault/BudgetUSDTreeDefault`；`SpawnBudgetTokenSubtree` 保留弃用 | §3.8 |
| `SpawnSpec` | 已含 `ToolAllowlist/MemoryDigest/Lifecycle/AllowChildSpawn` | 无需新增字段。`AllowStream` 保留但引擎忽略 | §6 |
| `SpawnResult` | `ChildRunRef/Status/FinalText/Error/Metrics` | 无需新增字段。v0.9.0 不实现 `ToolInvocations`（v0.9.1） | — |
| `RunMetrics` | `Model/Tokens/CostUSD/Steps` | 无需新增字段。预算由外部 BudgetCounter 管理 | §3.8 |

---

## 文件/包影响清单

| 路径/包 | 新增/修改 | 说明 |
|---------|----------|------|
| `pkg/model/nonstream.go` | 新增 | `nonStreamModel` + `wrapNonStream` |
| `internal/engine/runstate.go` | 修改 | `Children` → `*ChildRegistry`；新增 `AllowSpawn`、`BudgetCounter`；`Terminate` 委托注册表；新增 `SpawnInfo()` 观测方法 |
| `internal/engine/spawn_handle.go` | 新增 | `SpawnHandle`（state atomic.Int32 + cancel/done/result + Shutdown/SetResult/State） |
| `internal/engine/child_registry.go` | 新增 | `ChildRegistry`（Register/Unregister/TerminateAll/ActiveCount/Snapshot） |
| `internal/engine/spawn.go` | 修改 | `defaultSpawner` 完整实装，整合策略校验/非流式/预算/ChildRegistry 注册；`for range ch` → ctx-aware select |
| `internal/engine/policy.go` | 新增 | `defaultLoopPolicy` 实现（含 `MaxConcurrentSpawns`、`BudgetCounter`） |
| `internal/engine/engine.go` | 修改 | 追加 `LoopPolicy` 接口方法 |
| `internal/defaults/loop.go` | 修改 | 新增预算默认值、`DefaultGuardTimeout`；更新注释 |
| `pkg/runtime/exchange/exchange.go` 或新包 `pkg/runtime/budget` | 新增/修改 | `BudgetCounter` 结构体 |
| `pkg/agent/spawn.go` | 修改 | `defaultSpawner.Spawn` 接入 `wrapNonStream`、派生 `childCtx`、修复盲点 #1 |
| `pkg/agent/stream.go` | 修改 | 修复盲点 #2：`stream.Recv()` 循环中插 `select {case <-ctx.Done()}` |
| `pkg/agent/tools.go` | 修改 | 修复盲点 #3：`executeToolCalls` 工具间插 `select {case <-ctx.Done()}` |
| `pkg/agent/loop.go` | 修改 | for step 循环开头插 ctx 检查（兜底） |
| `pkg/runner/runner.go` | 修改 | 注入 `Spawner/LoopPolicy`；父终态触发 `RunState.Terminate` |
| `pkg/log` 或 OTel 层 | 修改 | 新增 `llm_mode=non_stream`、spawn 生命周期事件、级联取消日志 |

---

## 测试矩阵

### 单元测试

| 测试项 | 说明 | 预期 |
|--------|------|------|
| `wrapNonStream` 强制非流式 | 子 ChatModel 调用 `Generate` 正常；调用 `Stream` 返回降级流 | 不抛出异常；`Stream` 降级后只产出一条消息 |
| `AllowStream=true` 仍非流式 | 构造 `SpawnSpec{AllowStream: true}`，传入 spawn 路径 | 引擎忽略该字段，子 ChatModel 仍为 `nonStreamModel` |
| `SpawnHandle.Shutdown` 原子性 | 并发调用 `Shutdown` 和 `SetResult` | `closeOnce` 保证 done 只关闭一次；先到者决定 state |
| `SpawnHandle.State` 状态机 | `Shutdown` → `Cancelled`；`SetResult(completed)` → `Complete`；`SetResult(failed)` → `Failed` | 状态转换正确；终态后 `State()` 不改变 |
| `ChildRegistry.TerminateAll` 并发安全 | 子 goroutine 并发调用 `Unregister` 的同时父调用 `TerminateAll` | 快照机制防止死锁；所有 handle 都被 Shutdown |
| `ChildRegistry.ActiveCount` | 注册 3 个、注销 1 个 | `ActiveCount() == 2` |
| `ChildRegistry.Snapshot` | 有 2 个活跃子（不同 depth/state） | 快照包含 2 条记录，字段正确 |
| `RunState.Terminate` 级联取消 | 父 `Terminate` 后所有子 handle `Done()` 立即返回 | 子 goroutine 退出；`ActiveChildRuns = 0` |
| 父取消时 in-flight 工具调用 | 子正在执行 function-call，父调用 `Terminate` | function-call 返回 ctx 取消错误 |
| `BudgetCounter.Allow` 超额 | tokens > TokensMax 时 `Allow` 返回 false | 返回 false，不累加 |
| `BudgetCounter.Consume` 线程安全 | 多个 goroutine 同时累加 | 最终值等于各 goroutine 累加和 |
| `LoopPolicy.AllowSpawn` 并发超限 | `ActiveChildRuns >= MaxConcurrentSpawns` | 返回 false |
| `LoopPolicy.AllowStep` 预算超限 | BudgetCounter 余额不足 | 返回 false |

### 集成测试（mock 模型）

| 测试项 | 说明 |
|--------|------|
| 子 Run 非流式 | 抓取子 Agent 的 LLM 调用，验证使用 `nonStreamModel` |
| 父取消时子立即退出 | 父 ctx cancel，子 goroutine 在 100ms 内退出 |
| 盲点 #1 修复验证 | spawn 中 `for range ch` 被 ctx-aware select 替代；ctx 取消后 spawn 返回取消结果而非阻塞 |
| 盲点 #2 修复验证 | consumeStream 在 stream.Recv() 之间感知 ctx.Done() 并返回 |
| 盲点 #3 修复验证 | executeToolCalls 在工具调用之间感知 ctx.Done() 并返回 |
| 预算超额拒绝 | 设置极低 TokenMax，子 spawn 返回 `SpawnRejected` + `Code="budget_exceeded"` |
| 并发子数超限拒绝 | `MaxConcurrentSpawns=1`，同时请求 2 个子 spawn，第 2 个被拒绝 |
| ChildRegistry 生命周期 | spawn 注册 → 子完成 → Unregister → ActiveCount 归零 |
| RunState.SpawnInfo 观测 | 活跃子数、预算消耗字段正确返回 |

---

## 风险与决议

| 项 | 决议（v0.9.0） | 落点 |
|---|----------------|------|
| **非流式实现范围** | `nonStreamModel` 在 `pkg/model` 包实现；仅包装 `ToolCallingChatModel` 接口。`Stream` 降级为单条消息读取器，不阻塞父主路径。 | `pkg/model/nonstream.go` |
| **级联取消替换范围** | 替换三个盲点（spawn for-range / stream.Recv 循环 / executeToolCalls 工具间）。`SpawnHandle` 仅实现 `Shutdown=cancel()`，不含 inbox。 | `pkg/agent/spawn.go`、`pkg/agent/stream.go`、`pkg/agent/tools.go`、`internal/engine/spawn_handle.go` |
| **ChildRegistry 快照而非逐 handle 锁定** | 防止 `TerminateAll` 遍历时与子 goroutine 并发 `Unregister` 导致死锁。快照后释放锁再依次 `Shutdown`。 | `internal/engine/child_registry.go` |
| **`SpawnHandle` 的 `closeOnce` 幂等性** | `Shutdown` 和 `SetResult` 都可能触发 done channel 关闭；通过 `closeOnce` 保证只关闭一次，atomic state 记录先到者的状态。 | `internal/engine/spawn_handle.go` |
| **预算值设定** | MVP 使用默认值：100K tokens / $0.50。生产部署时按模型定价调整。`SpawnBudgetTokenSubtree = 0` 旧常量保留弃用注释。 | `internal/defaults/loop.go` |
| **engine 与 agent 的对接策略** | `internal/engine` 的 `Spawner` 完整实装，`pkg/agent` 的 `defaultSpawner` 通过薄适配层（`engineAdapter`）转调 `internal/engine.Spawner`，避免两套实现分裂。 | `internal/engine/spawn.go`、`pkg/agent/spawn.go` |
| **`LoopPolicy` 接口扩展** | 新增 `MaxConcurrentSpawns() int` 和 `BudgetCounter() *BudgetCounter` 方法；v0.8.0 的 `LoopPolicy` 接口不再兼容，需更新所有调用方。 | `internal/engine/engine.go` |
| **`AllowStream` 字段** | 保留在 `SpawnSpec` 中，但引擎不读取；JSON Schema 中不暴露；文档注明「v0.8.0 决议保留，v0.9.0 引擎忽略」。 | 已有字段，新增注释 |
| **Runaway Child 兜底** | Watchdog 只日志告警不强制终止。Go 无法安全终止 goroutine。watchdog 超时 = 代码 bug 的红旗。 | `internal/engine/runstate.go`、`TerminateWithGuard` |
| **ActiveChildRuns 缓存** | `ActiveChildRuns` 作为缓存字段，真值由 `ChildRegistry.ActiveCount()` 提供。策略校验中批量更新缓存。 | `internal/engine/runstate.go` |

---

## 交付清单

- 子 LLM trace/日志可见 `llm_mode=non_stream`
- 父取消时子立即退出测试覆盖（三个盲点修复验证）
- `BudgetCounter` 全树共享，超额拒绝
- `MaxConcurrentSpawns` 并发子数限制
- `internal/engine` 实装完成（Spawner / LoopPolicy / RunState / ChildRegistry / SpawnHandle）
- `pkg/agent.defaultSpawner` 通过适配层转调 `internal/engine.Spawner`
- `SpawnHandle` 生命周期状态机（running → complete / failed / cancelled）
- `ChildRegistry` 快照与观测接口（SpawnInfo / Snapshot / ActiveCount）
- 结构化日志：spawn 生命周期事件、级联取消、watchdog 超时
- `TerminateWithGuard` 兜底机制（5s 默认超时）
- `SpawnInfo` 观测接口（活跃子数 + 预算消耗）

---

## 实现顺序依赖图

```
阶段A (非流式)
    │
    ▼
阶段D (engine 实装)
    │
    ├──▶ 阶段B (级联取消 + 追踪)
    │         ├── SpawnHandle（状态机 + closeOnce 幂等）
    │         ├── ChildRegistry（注册表 + 快照）
    │         ├── RunState.Terminate → 委托注册表
    │         ├── 盲点 #1 修复 (spawn for-range → ctx select)
    │         ├── 盲点 #2 修复 (stream.Recv 循环插 ctx 检查)
    │         ├── 盲点 #3 修复 (executeToolCalls 工具间插 ctx 检查)
    │         └── Runaway Watchdog（5s 超时兜底）
    │
    └──▶ 阶段C (预算 + 并发)
              ├── BudgetCounter
              └── LoopPolicy.AllowSpawn 增强
```

> 阶段 A 无外部依赖。阶段 B 的核心（SpawnHandle + ChildRegistry）是阶段 D 的必要组件。阶段 C 也依赖 ChildRegistry 的 ActiveCount。建议顺序：A → B (SpawnHandle + ChildRegistry) → D 骨架 → B（盲点修复+watchdog）+ C → D 完整集成 + 观测层 → 测试。

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：v0.9.0 版本计划（核心约束子版本，从原 v0.9.0 PRD 拆分） |
| 2026-04-24 | 补充方案细节：子 Agent 追踪方案（ChildRegistry + SpawnHandle 状态机）和即时关闭机制（三个 ctx 盲点分析 + 修复 + Runaway Watchdog兜底 + 可观测性设计） |

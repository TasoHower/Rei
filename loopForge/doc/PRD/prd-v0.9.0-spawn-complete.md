# loopForge 产品需求文档 — v0.9.x Spawn 功能补全

> **版本**：v0.9.0 / v0.9.1 / v0.9.2  
> **日期**：2026-04-24  
> **里程碑**：Spawn 功能补全（完成 v0.8.0 遗留功能与后续迭代 TODO）  
> **状态**：计划稿  
> **关联文档**：[progress-v0.8.0.md](../log/progress-v0.8.0.md)、[plan-v0.8.0.md](../plan/plan-v0.8.0.md)、[spawn-runtime-rules.md](../design/spawn-runtime-rules.md)

---

## 1. 概述

### 1.1 产品定位

**v0.9.x 系列**是 loopForge v0.8.0 Spawn 功能的补全版本，完成 Slice1 未实现的核心功能，使 Spawn 达到完整可用状态。基于 `doc/design/spawn-runtime-rules.md` 的完整规则集，拆分为三个子版本递进交付：

| 版本 | 聚焦 |
|------|------|
| **v0.9.0** | 核心约束 — 非流式、父取消级联、预算、engine 对接 |
| **v0.9.1** | 工具与事件 — 工具过滤、ReAct 聚合、spawn 事件、transfer 互斥 |
| **v0.9.2** | 生命周期最终 — Linger 唤醒、脱敏、遗留待决完成 |

### 1.2 核心价值

1. **完整约束** — 实现 spawn-runtime-rules.md 的全部 7 条规则
2. **成本可控** — 全树共享预算，父取消级联子
3. **灵活生命周期** — 支持 ephemeral（单次）和 linger（可唤醒）两种模式
4. **事件完整** — spawn 起止事件可观测
5. **安全互斥** — 有活跃子 Agent 时禁止 transfer

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.8.0** | Slice1 最小实现；v0.9.x 完成其后续迭代 TODO |
| **v0.9.0** | 核心约束（非流式、级联取消、预算） |
| **v0.9.1** | 工具过滤、ReAct 聚合、事件、transfer 互斥 |
| **v0.9.2** | Linger 唤醒、最终补全 |
| **v1.0+** | 性能优化与稳定化 |

---

## 2. 需求范围

### 2.1 版本拆分总览

| # | 需求项 | v0.9.0 | v0.9.1 | v0.9.2 |
|---|--------|--------|--------|--------|
| 1 | 子 ChatModel 非流式包装 | P0 | - | - |
| 2 | 全树预算共享（BudgetCounter） | P0 | - | - |
| 3 | 父取消级联子（RunState.Terminate） | P0 | - | - |
| 4 | MaxConcurrentSpawns | P0 | - | - |
| 5 | internal/engine 实装对接 | P0 | - | - |
| 6 | ToolAllowlist/ToolBlocklist glob 通配 | - | P0 | - |
| 7 | applyChildToolFilter 白先黑后 | - | P0 | - |
| 8 | SpawnResult.ToolInvocations 聚合 | - | P0 | - |
| 9 | EmitChildToolInvocations 外发 | - | P0 | - |
| 10 | spawn 事件（EventSpawnStart/End/Wake） | - | P0 | - |
| 11 | agent_transfer 与 spawn 互斥 | - | P0 | - |
| 12 | LifecycleLinger 完整实现 | - | - | P0 |
| 13 | SpawnHandle/ChildMailbox 完整实现 | - | - | P0 |
| 14 | WakeMessage 唤醒机制 | - | - | P0 |
| 15 | ToolInvocations 脱敏与上限 | - | - | P1 |
| 16 | 事件命名对表 | - | - | P1 |
| 17 | 遗留待决完成 | - | - | P1 |

### 2.2 不在本版本范围

1. **嵌套 spawn > 2 层**（v0.8.0 决议仅支持 root + 一层子 Agent）
2. **子 Agent 流式输出**（v0.8.0 决议 `AllowStream` 字段保留但不开放）
3. **自动摘要器**（`MemoryDigest` 默认关闭）

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：子 Agent 调用 LLM 时强制非流式以降低网络开销（v0.9.0）  
**验收标准**：
- 子 Agent 的 LLM 调用强制 `stream=false`
- trace/日志可见 `llm_mode=non_stream`
- `SpawnSpec.AllowStream` 字段被引擎忽略

**US-2**：预算全树共享，成本可控（v0.9.0）  
**验收标准**：
- 根 Run 持有 `BudgetCounter`（tokens + USD）
- 所有子树共享同一计数器
- 超额时拒绝 `Code = "budget_exceeded"`

**US-3**：父结束时所有子立即退出（v0.9.0）  
**验收标准**：
- 父 Run 终态时所有子（含等待唤醒态）立即取消
- 不等待子 loop 当前回合跑完
- 子级 in-flight function-call 进入 recorder 的 Error 字段

**US-4**：能够通过工具白/黑名单控制子 Agent 可见工具（v0.9.1）  
**验收标准**：
- `tool_white_list` 和 `tool_block_list` 支持 glob 通配（如 `mcp:foo/*`）
- 白先黑后过滤逻辑正确
- `spawn_subagent` 工具不受双名单影响

**US-5**：子 Agent 的工具调用结果可聚合外发（v0.9.1）  
**验收标准**：
- 子 ReAct 中间 function-call/tool_result 不进入父侧事件流
- `SpawnResult.ToolInvocations` 按发生顺序聚合
- 父 Agent 通过 `EmitChildToolInvocations` 外发工具调用报文

**US-6**：spawn 事件可观测（v0.9.1）  
**验收标准**：
- `EventSpawnStart` / `EventSpawnEnd` / `EventSpawnWake` 事件可发出
- 子级 RunID、depth、父级 RunID 关联

**US-7**：有活跃子 Agent 时禁止 transfer（v0.9.1）  
**验收标准**：
- root agent system prompt 显式写入约束
- `agent_transfer` 工具 Handle 硬校验
- 命中返回 `Code = "transfer_blocked_active_children"`

**US-8**：子 Agent 可 linger 并支持再次唤醒（v0.9.2）  
**验收标准**：
- `Lifecycle = linger` 子 Agent 执行完单次任务后进入等待态
- 父 Agent 可通过 `SpawnHandle.Wake` 再次唤醒子 Agent
- 空闲超时自动回收

---

## 4. 功能需求

### 4.1 子 ChatModel 非流式包装（v0.9.0）

```go
// wrapNonStream 强制子 ChatModel 为非流式
func wrapNonStream(chat model.ChatModel) model.ChatModel {
    // 实现强制 stream=false
}
```

### 4.2 全树预算共享（v0.9.0）

```go
type BudgetCounter struct {
    TokensUsed int64
    CostUSD    float64
    TokensMax  int64
    CostUSDMax float64
}
```

### 4.3 工具过滤与 ReAct 聚合（v0.9.1）

```
白先黑后：
1. 若 allow 非空 → 仅保留 name ∈ allow 的工具
2. 若 block 非空 → 移除 name ∈ block 的工具
3. spawn_subagent 工具不受双名单影响
```

```go
type ToolInvocation struct {
    Name         string
    Arguments    json.RawMessage
    ResultDigest string
    StartedAt    time.Time
    EndedAt      time.Time
    Error        string
    MCPSource    string
    Attrs        map[string]string
}
```

### 4.4 spawn 事件（v0.9.1）

```go
const (
    EventSpawnStart EventType = "spawn_start"
    EventSpawnEnd   EventType = "spawn_end"
    EventSpawnWake  EventType = "spawn_wake"
)
```

### 4.5 agent_transfer 与 spawn 互斥（v0.9.1）

```
互斥逻辑：
1. root agent system prompt 显式写入「有活跃子 Agent 时禁止 transfer」
2. agent_transfer 工具 Handle 执行前校验 RunState.ActiveChildRuns == 0
3. 命中返回 Code = "transfer_blocked_active_children"
4. 等子 Agent 全部回收后方可 transfer
```

### 4.6 linger 生命周期与唤醒（v0.9.2）

```go
type Lifecycle string

const (
    LifecycleEphemeral Lifecycle = "ephemeral"
    LifecycleLinger    Lifecycle = "linger"
)

type WakeMessage struct {
    Seq        uint64
    Task       string
    Addendum   string
    Digest     *MemoryDigest
    Deadline   time.Time
    Control    WakeControl
}
```

```go
type SpawnHandle struct {
    Ref       RunRef
    Lifecycle Lifecycle
    inbox     chan WakeMessage
    results   chan SpawnResult
    events    chan *RuntimeEvent
    cancel    context.CancelFunc
    closeOnce sync.Once
}

func (h *SpawnHandle) Wake(ctx context.Context, msg WakeMessage) error
func (h *SpawnHandle) Result(ctx context.Context) (SpawnResult, error)
func (h *SpawnHandle) Events() <-chan *RuntimeEvent
func (h *SpawnHandle) Close()
```

---

## 5. 技术需求

### 5.1 LoopPolicy 接口

```go
type LoopPolicy interface {
    AllowStep(state *RunState) bool
    AllowSpawn(state *RunState, depth int) bool
    MaxSteps() int
    MaxSpawnDepth() int
    MaxConcurrentSpawns() int
}
```

### 5.2 RunState

```go
type RunState struct {
    RunID           string
    Step            int
    LastError       error
    Termination     outcome.TerminationReason
    ParentRunID     string
    Depth           int
    ActiveChildRuns int
    Children        map[string]*SpawnHandle
    Mailbox         *ChildMailbox
    AllowSpawn      bool
    BudgetCounter   *BudgetCounter
}
```

### 5.3 观测性

- **slog**：spawn_start / spawn_end / spawn_wake 事件；预算消耗；linger 唤醒与超时
- **OTel**：spawn span（parent_run_id、depth、allow_child_spawn、tool_filter_size）；子级 llm_mode=non_stream

---

## 6. 验收标准

| 版本 | 验收项 |
|------|--------|
| **v0.9.0** | 非流式、级联取消、预算共享、engine 对接 |
| **v0.9.1** | 工具过滤、ReAct 聚合、事件、transfer 互斥 |
| **v0.9.2** | Linger 唤醒、脱敏、事件命名对表 |

详见各版本 acceptance 与 progress 文档。

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **linger 复杂度** | linger 模式下 goroutine 泄漏风险 | 严格 idle timeout + Close() 调用 |
| **预算精度** | 预算全树共享的线程安全 | 加锁保护 BudgetCounter |
| **事件命名对表** | 与 data-fusion.md 命名差异 | 提前 review 并对齐 |

---

## 8. 参考文档

- [progress-v0.8.0.md](../log/progress-v0.8.0.md) — v0.8.0 进度
- [plan-v0.8.0.md](../plan/plan-v0.8.0.md) — v0.8.0 实施计划
- [spawn-runtime-rules.md](../design/spawn-runtime-rules.md) — spawn 运行时规则
- [multi-agent-engine.md](../design/multi-agent-engine.md#38-spawn) — spawn 总述

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-24 | v0.9.x | 初始版本 |
| 2026-04-24 | v0.9.x | 拆分为 v0.9.0（核心约束）/ v0.9.1（工具与事件）/ v0.9.2（生命周期最终）三个子版本 |

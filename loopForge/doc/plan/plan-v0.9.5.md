# loopForge 实施计划 — v0.9.5（决策与设计文档修订）

> **对应版本进度**：`doc/log/progress-v0.9.5.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的任务拆分与执行顺序；**先于编码**成文。  
> **主目标**：全面修订 `doc/decision/` + `doc/design/` 下的技术文档，对齐代码事实，补充 v0.8.0~v0.9.4 期间形成但未记录的关键决策。

---

## 1. 背景与问题

### 1.1 `doc/decision/` 现状

`doc/decision/` 目录当前只有一份文档：[sdk-selection.md](file:///Users/admin/Documents/hower/rei/loopForge/doc/decision/sdk-selection.md)，创建于 v0.1.0 初期。该文档已严重滞后于代码事实：

| 问题 | 文档描述 | 代码事实 |
|------|---------|---------|
| `pkg/network` | 声称 loopForge 使用 agent-sdk-go 的 `pkg/network`（静态多 Agent 策略） | `pkg/network` **不存在**；多 Agent 通过 `agent_transfer` / runner orchestrator 实现 |
| loop 内核 | 声称 loop 内核是 agent-sdk-go `pkg/runner` | loop 内核是**自实现**的 [pkg/agent/loop.go](file:///Users/admin/Documents/hower/rei/loopForge/pkg/agent/loop.go)；agent-sdk-go 仅作为模型适配层使用 |
| 引擎架构 | 没有关于 `internal/engine` 的描述 | `internal/engine/`（12 个文件）是实际的核心引擎层，管理 RunState、Spawner、ChildRegistry、预算等 |
| 模型适配 | 未提及 `pkg/model/adapters/` | 代码已有三层适配：agentsdk（Lark）、deepseek、以及接口抽象 `pkg/model/interface` |

v0.8.0~v0.9.4 期间形成了一批重要的架构决策，全部未以 ADR 形式落盘：

| 决策 | 形成版本 | 当前仅记录在 |
|------|---------|-------------|
| **事件设计**：用 `agent_transfer` 表达 spawn 生命周期，而非新增 `spawn_*` 专用事件 | v0.9.4 | `data-fusion.md` §3.1（一句话提及） |
| **spawn 隔离**：用 `SpawnSpec.OutputCh`（非阻塞转发）而非直接共享父 eventCh | v0.9.4 | `plan-v0.9.4.md` §8（实施细节） |
| **引擎分层**：`pkg/agent`（loop）↔ `internal/engine`（状态管理）↔ `pkg/runner`（入口） | v0.8.0~v0.9.2 | 分散在多个 progress 日志中 |
| **子非流式**：spawn 子 Agent 强制非流式 LLM 调用，与父配置解耦 | v0.8.0 | `spawn-runtime-rules.md` §6 |
| **工具双名单**：白先黑后的 glob 通配过滤策略 | v0.8.0 | `spawn-runtime-rules.md` §7 |
| **生命周期**：仅 ephemeral，linger 预留但拒绝 | v0.8.0~v0.9.2 | `spawn.go` 中的 `SpawnRejected` 分支 |
| **DeepSeek 接线**：新增适配器而非替换 Lark 适配器 | v0.9.3 | `runner/deepseek_default.go` |

### 1.2 `doc/design/` 现状

`doc/design/` 下 6 份文档定位偏"愿景与设计蓝图"，与当前代码实现存在不同程度偏差：

| 文档 | 问题 | 严重程度 |
|------|------|----------|
| **architecture.md** | §2.2 分层表大量引用不存在的模块：`pkg/network`、`internal/network/`、`internal/observability/`、`internal/devserver/`、`web/devui/`；§7 将 test-server 混同为 DevServer | 高 |
| **abstractions.md** | §0 架构图缺少 `internal/engine`；§8 `SpawnSpec` 摘录缺少 `OutputCh`/`Lifecycle`/`MemoryDigest`/`AllowChildSpawn`；提及不存在的 `RunModeNetwork` 和 `NetworkStrategy` | 中 |
| **multi-agent-engine.md** | 全文大量引用 `pkg/network`、`NetworkRunner`、`internal/observability/`、`web/devui/` 等不存在模块；§8 Dev UI 设计蓝图未实现；§4.1 描述不存在的静态 Network 策略 | 高 |
| **data-fusion.md** | 基本准确，缺少对 `event-semantics.md` ADR 的交叉引用 | 低 |
| **mcp-tool-unification.md** | §1.4 `loopforge/debug` import 路径与当前代码不一致（实际路径为 `loopforge/pkg/mcp`）；Ark schema 清洗参考已过时 | 中 |
| **spawn-runtime-rules.md** | 基本准确，与代码一致 | 低 |

### 1.3 约束

- **只补文档不补代码**：不修改 `pkg/`、`internal/`、`test-server/` 下的任何 Go/JS 代码
- **保持事实优先**：每个文档必须与当前代码行为一致；愿景标注为"规划"
- **不改变设计文档的愿景定位**：`doc/design/` 文档是架构蓝图，只添加实现状态标记，不删愿景内容
- **`doc/decision/` 下的 ADR 是严格的事实记录**：需与代码逐行对齐

---

## 2. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.5.md` | 版本目标、范围、交付清单 |
| `doc/design/spawn-runtime-rules.md` | spawn 运行时规则（需交叉引用） |
| `doc/design/data-fusion.md` | 事件类型语义（需交叉引用） |
| `doc/design/multi-agent-engine.md` | 引擎架构设计（需标记实现状态） |
| `doc/design/architecture.md` | 系统架构（需修正事实错误） |
| `doc/design/abstractions.md` | 关键抽象（需修正事实错误） |
| `doc/design/mcp-tool-unification.md` | MCP 工具统一（需修正引用错误） |
| `doc/log/progress-v0.8.0.md` | spawn 初始实现的技术决策集 |
| `doc/log/progress-v0.9.4.md` | 最近版本，含事件转发决策 |

---

## 3. 执行流程

⚠ **重要**：以下每个文档为一个独立作业项。每完成一项，我会停下来等待你的确认，再进入下一项。顺序如下：

```
[ADR 1] event-semantics.md   →  你确认   →  我实施
    → 你确认实施结果
[ADR 2] spawn-isolation.md   →  你确认   →  我实施
    → 你确认实施结果
[ADR 3] engine-layering.md   →  你确认   →  我实施
    → 你确认实施结果
    → 你下一次明确说"继续下一个文档修改"
[sdk-selection.md 修订]      →  你确认   →  我实施
    → 你确认实施结果
[architecture.md 修订]       →  你确认   →  我实施
    → ...
```

---

## 4. 文档清单（逐个实施）

---

### 作业 #1：新增 `doc/decision/event-semantics.md`

**类型**：ADR（架构决策记录）  
**前置条件**：无（本次第一个实施）

**内容要求**：

1. **问题背景**：v0.9.4 前 spawn 子 Agent 对前端完全不可见——子 Agent 所有事件在本地事件循环中消费，父端只能看到 `spawn_subagent` 工具调用，看不到子 Agent 的启动与结束。
2. **候选方案**：
   - 方案 A：新增 `EventSpawnStart`/`EventSpawnEnd` 专用事件类型 + Payload
   - 方案 B：复用 `agent_transfer` + `Phase`（start/end）+ `ChildRunID`/`Depth`
3. **决策理由**：方案 B 更优——`AgentTransferPayload` 已有完备字段，避免事件类型膨胀，与 `data-fusion.md` 设计原则一致
4. **后果**：spawn 事件和 transfer 事件共用同一事件类型，前端需通过 `phase` + `depth` 区分
5. **关联文档**：`data-fusion.md` §3.1、`plan-v0.9.4.md` §3.2、`spawn.go`、`spawn_tool.go`

**必须包含的代码片段**：

```go
// AgentTransferPayload 中用于 spawn 生命周期的字段（pkg/runtime/event/event.go:L150-L168）
type AgentTransferPayload struct {
    Phase      TransferPhase `json:"phase"`       // "start" | "end"
    FromAgent  string        `json:"from_agent"`  // 父 Agent 角色名
    ToAgent    string        `json:"to_agent"`    // 子 Agent 名称
    ChildRunID string        `json:"child_run_id,omitempty"` // 子运行 ID
    Depth      int           `json:"depth,omitempty"`         // 子树深度
    OK         bool          `json:"ok,omitempty"`            // 仅 end 时有效
}
```

```go
// DefaultSpawner 事件循环中转发 start/end 的核心逻辑（pkg/agent/spawn.go）
// 插入点 ①：EventStart → agent_transfer(TransferStart)
if se := ev.Start(); se != nil && spec.OutputCh != nil {
    select {
    case spec.OutputCh <- &event.RuntimeEvent{
        Type:  event.EventAgentTransfer,
        RunID: parent.RunID,
        Step:  0,
        Payload: &event.AgentTransferPayload{
            Phase:      event.TransferStart,
            FromAgent:  parent.AgentRole,
            ToAgent:    childTmpl.Name,
            ChildRunID: childRef.RunID,
            Depth:      childRef.Depth,
        },
    }:
    default:  // 非阻塞发送；父端满时不阻塞子
    }
}

// 插入点 ②：EventQueryEnd → agent_transfer(TransferEnd)
if qe := ev.QueryEnd(); qe != nil {
    // ... 提取 last 后 ...
    if spec.OutputCh != nil {
        childOK := last != nil && last.Termination == outcome.TerminationCompleted
        select {
        case spec.OutputCh <- &event.RuntimeEvent{
            Type:  event.EventAgentTransfer,
            RunID: parent.RunID,
            Step:  0,
            Payload: &event.AgentTransferPayload{
                Phase:      event.TransferEnd,
                FromAgent:  parent.AgentRole,
                ToAgent:    childTmpl.Name,
                ChildRunID: childRef.RunID,
                Depth:      childRef.Depth,
                OK:         childOK,
            },
        }:
        default:
        }
    }
    continue
}
```

**必须包含的逻辑图**（ASCII art）：

```
子 Agent RunLoop
  │
  ├─ EventStart ──→ DefaultSpawner 本地消费 ──→ [OutputCh] ──→ 父 eventCh ──→ test-server SSE
  │                                                    │          agent_transfer(phase=start)
  ├─ EventAnswer ──→ 本地消费（隔离，不转发）
  ├─ EventCallLLMStart/End ──→ 本地消费（隔离）
  ├─ EventToolCallStart/End ──→ 本地消费 + 记录到 ChildToolCalls
  │                                                    │
  └─ EventQueryEnd ──→ 本地消费（提取 Outcome）──→ [OutputCh] ──→ 父 eventCh ──→ test-server SSE
                                                               agent_transfer(phase=end, ok=true/false)
```

---

### 作业 #2：新增 `doc/decision/spawn-isolation.md`

**类型**：ADR（架构决策记录）  
**前置条件**：作业 #1 已完成并确认

**内容要求**：

1. **问题背景**：子 Agent ReAct 中间步骤（`answer`、`call_llm_*`、`tool_call_*`）应不对父端暴露；但启动和结束事件应可观测。同时要防止父端慢消费阻塞子 Agent goroutine。
2. **候选方案**：
   - 方案 A：直接将子 eventCh 注入父 eventCh（违反隔离原则）
   - 方案 B：子 Agent 流式写入父 eventCh（实现复杂，父端可能被子中间步骤淹没）
   - 方案 C（选定）：`DefaultSpawner` 内建本地 channel → 本地事件循环 → 仅 `start`/`query_end` 通过 `SpawnSpec.OutputCh` 转发
3. **核心约束**：`spawn-runtime-rules.md` §7（中间步骤不可见）
4. **决策理由**：方案 C 最轻量——利用已有的 `SpawnSpec.OutputCh` 通道，不新增复杂异步机制，非阻塞发送确保子 goroutine 不被父端慢消费阻塞
5. **后果**：父端 channel 满时 start/end 事件可能丢弃（可观测性增强的权衡）；未来需要更多子事件可扩展 OutputCh 发送逻辑
6. **关联文档**：`spawn.go`、`spawn_tool.go`、`plan-v0.9.4.md` §8.1、`spawn-runtime-rules.md` §7

**必须包含的代码片段**：

```go
// DefaultSpawner.Spawn() 的完整事件循环结构（pkg/agent/spawn.go:L134-L211）
// 本地 channel 消费所有子事件，仅 start/query_end 通过 OutputCh 转发

ch := make(chan *event.RuntimeEvent, 256)  // 本地 channel，父端不可见

go func() {
    defer close(ch)
    childTmpl.RunLoop(childCtx, sub, ch, nil, st)
}()

eventLoop:
for {
    select {
    case ev, ok := <-ch:
        if !ok { break eventLoop }

        // ✅ 隔离规则：仅 EventStart 和 EventQueryEnd 通过 OutputCh 转发
        if se := ev.Start(); se != nil && spec.OutputCh != nil {
            // 非阻塞发送 agent_transfer(TransferStart)
            select { case spec.OutputCh <- ... : default: }
        }

        if qe := ev.QueryEnd(); qe != nil {
            // 提取 Outcome，然后非阻塞发送 agent_transfer(TransferEnd)
            select { case spec.OutputCh <- ... : default: }
            continue  // 不继续处理本事件的其他判断
        }

        // 🚫 隔离规则：以下事件仅在本地消费，不向父端转发
        if cs := ev.ToolCallStart(); cs != nil { /* 本地记录 */ }
        if ce := ev.ToolCallEnd(); ce != nil   { /* 本地追加 ChildToolCall */ }

    case <-childCtx.Done():
        break eventLoop
    }
}
```

```go
// spawn_tool.go 中打通 OutputCh 的一行变更（spawn_tool.go:L120-L127）
spec := &exchange.SpawnSpec{
    Task:            a.Task,
    SystemAddendum:  a.SystemAddendum,
    SkillIDs:        a.SkillIDs,
    ModelOverride:   a.Model,
    AllowChildSpawn: a.AllowChildSpawn,
    OutputCh:        eventCh,  // ← 唯一变更：子→父事件转发通道
}
```

**必须包含的逻辑图**：

```
                    父 Agent eventCh (cap 256)
                          ↑
                          │ agent_transfer(start/end) 仅两条
                    ┌─────┴─────┐
                    │ OutputCh  │  (SpawnSpec 字段，非阻塞发送)
                    └─────┬─────┘
                          │
┌─────────────────────────┴────────────────────────────┐
│  DefaultSpawner.Spawn() 本地事件循环                    │
│                                                       │
│  ┌─────────────────────────────────────────────────────┤
│  │  子 Agent RunLoop (独立 goroutine)                   │
│  │                                                     │
│  │  EventStart      ──→ 转发 start ?──→ OutputCh       │
│  │  EventAnswer     ──→ 丢弃 (本地不保留)                │
│  │  CallLLMStart    ──→ 丢弃                             │
│  │  ToolCallStart   ──→ 记录 pendingTC                  │
│  │  ToolCallEnd     ──→ 追加 ChildToolCalls              │
│  │  QueryEnd        ──→ 提取 Outcome → 转发 end → OutputCh │
│  │  childCtx.Done   ──→ 转发 end(OK=false) → OutputCh   │
│  └─────────────────────────────────────────────────────┤
│                                                        │
│  最终返回 SpawnResult (FinalText + Metrics + ToolCalls) │
└────────────────────────────────────────────────────────┘
```

---

### 作业 #3：新增 `doc/decision/engine-layering.md`

**类型**：ADR（架构决策记录）  
**前置条件**：作业 #2 已完成并确认

**内容要求**：

1. **问题背景**：从 v0.1.0 单 Agent loop 演进到 v0.9.x 支持 spawn + multi-agent transfer，需要清晰的分层来管理不同职责
2. **候选方案**：
   - 方案 A：所有逻辑集中在 `pkg/agent`（违反单一职责，`internal/engine` 的全局状态管理需要隔离）
   - 方案 B（选定）：三层架构 `pkg/runner` → `internal/engine` → `pkg/agent`
3. **决策理由**：单向依赖链——入口层可以换、引擎层管理全局状态、Agent 层纯净无状态，便于测试和替换
4. **后果**：`pkg/agent` 不直接引用 `internal/engine`；新能力按分层添加
5. **关联文档**：`internal/engine/`、`pkg/runner/runner.go`、`pkg/agent/loop.go`

**必须包含的代码片段**：

```go
// pkg/agent — 纯净的 Agent 定义与 RunLoop（不关心外部状态）
type Agent struct {
    Name               string
    ChatModel          model.ToolCallingChatModel
    ToolInfos          []*model.ToolInfo
    SpawnEnabled       bool
    ChildAgentBuilder  func(*exchange.SpawnSpec) *Agent
    Spawner            Spawner   // 接口，可被 engineSpawner 包装
}

func (a *Agent) RunLoop(ctx context.Context, req *request.RuntimeRequest,
    ch chan<- *event.RuntimeEvent, inheritedMsgs []*model.Message,
    state *LoopState) *InterceptedCall {
    // 纯 loop：拼消息 → emit → call_llm → tool → 再循环
}
```

```go
// internal/engine — 全局状态管理与生命周期
type RunState struct {
    RunID           string
    Step            int
    Depth           int
    ActiveChildRuns int
    Children        *ChildRegistry
    BudgetCounter   *budget.BudgetCounter
}

type engineSpawner struct {
    inner    agent.Spawner       // 包装 pkg/agent DefaultSpawner
    runState *RunState
}

func (s *engineSpawner) Spawn(ctx context.Context, parent exchange.RunRef, spec *exchange.SpawnSpec) (*exchange.SpawnResult, error) {
    // 引擎层注入：子注册到 ChildRegistry、子 token 滚入树级预算
    result, err := s.inner.Spawn(childCtx, parent, spec)
    if result != nil {
        s.runState.Children.Register(result.ChildRunRef.RunID, handle)
        if s.runState.BudgetCounter != nil {
            s.runState.BudgetCounter.Consume(result.Metrics.TotalTokens, 0)
        }
    }
    return result, nil
}
```

```go
// pkg/runner — 入口装配层
func NewRunner(entry *agent.Agent, opts ...RunOption) *Runner {
    r := &Runner{entry: entry}
    for _, o := range opts { o(r) }
    return r
}

func (r *Runner) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent {
    runState := engine.NewRunState(...)
    // 注入 engine 层 Spawner 包装
    inner := agent.NewDefaultSpawner(exec, maxD)
    exec.Spawner = engine.NewEngineSpawner(inner, runState)
    return exec.RunLoopWithEngine(ctx, req, runState)
}
```

**必须包含的逻辑图**：

```
                       依赖方向（自上而下）
                    ┌──────────────────────────┐
                    │   pkg/runner              │
                    │   Runner.Run()             │
                    │   NewRunner() → Runnable   │
                    │   夹接 agent-sdk-go 适配   │
                    │   装配 engine 层 Spawner   │
                    └─────────────┬──────────────┘
                                  │ 引用
                    ┌─────────────▼──────────────┐
                    │   internal/engine           │
                    │   RunState / ChildRegistry  │
                    │   engineSpawner             │
                    │   BudgetCounter             │
                    │   SpawnHandle               │
                    └─────────────┬──────────────┘
                                  │ 引用 interface
                    ┌─────────────▼──────────────┐
                    │   pkg/agent                 │
                    │   Agent / RunLoop           │
                    │   DefaultSpawner            │
                    │   Spawner interface          │
                    │   纯净 loop，不关心外部状态  │
                    └─────────────────────────────┘
```

---

### 作业 #4：修订 `doc/decision/sdk-selection.md`

**类型**：修订现有文档  
**前置条件**：作业 #3 已完成并确认

**修改要点**：

1. **§1 术语对照表**：
   - 移除 `pkg/network` 引用
   - agent-sdk-go 角色：从 "loop 内核" 改为 "模型适配层"
   - 新增 `internal/engine`（引擎核心）、`pkg/agent`（Agent loop 自实现）、`pkg/runner`（入口装配层）的准确描述
2. **§2 选型理由**：修正 agent-sdk-go 的使用范围（仅模型适配）；补充自实现 loop 的原因
3. **§3 与 Eino 对比**：编排单元从 NetworkRunner 改为 engine.Runner + agent_transfer
4. **§6 最终决策**：架构栈更新为 `pkg/agent` + `internal/engine` + `pkg/runner`
5. **变更记录**：末行记录本次修订

---

### 作业 #5：修订 `doc/design/architecture.md`

**类型**：修订现有设计文档  
**前置条件**：作业 #4 已完成并确认

**修改要点**：

1. **§2.2 分层表**：对不存在的模块添加"（规划）"标记——`pkg/network`、`internal/observability/`、`internal/devserver/`、`web/devui/`
2. **§3.2 静态 Network**：修正 `pkg/network` 为 `agent_transfer / handoff`
3. **§7 可观测性**：明确 test-server 是当前实际调试手段；DevServer 标注为"（规划）"
4. **尾部**：新增"实现状态"字段说明各模块的落地情况

---

### 作业 #6：修订 `doc/design/abstractions.md`

**类型**：修订现有设计文档  
**前置条件**：作业 #5 已完成并确认

**修改要点**：

1. **§0 架构图**：补充 `internal/engine/` 层
2. **§8 `SpawnSpec` 摘录**：补充 `OutputCh`、`Lifecycle`、`MemoryDigest`、`AllowChildSpawn` 字段
3. 移除对不存在的 `RunModeNetwork` 和 `NetworkStrategy` 的引用
4. **尾部**：新增交叉引用指针指向 `engine-layering.md` ADR

---

### 作业 #7：修订 `doc/design/multi-agent-engine.md`

**类型**：修订现有设计文档  
**前置条件**：作业 #6 已完成并确认

**修改要点**：

1. **文件头部**：添加"版本对应"实现状态块
2. **§1.2**：将 `pkg/network` 修正为 handoff/agent_transfer
3. **§2.1 架构图**：标记 `pkg/network` 为"（规划）"
4. **§4.1 静态 Network**：替换为当前实际的 agent_transfer 描述；原 pkg/network 段落标记为规划
5. **§8 Dev UI**：添加"（规划 — 当前调试使用 test-server）"标记

---

### 作业 #8：修订 `doc/design/data-fusion.md`

**类型**：修订现有设计文档  
**前置条件**：作业 #7 已完成并确认

**修改要点**：

1. **§3.1 事件映射表**：补充 `agent_transfer` 用于 spawn 生命周期的说明
2. **§6 相关文档**：新增指向 `event-semantics.md` ADR 的交叉引用

---

### 作业 #9：修订 `doc/design/mcp-tool-unification.md`

**类型**：修订现有设计文档  
**前置条件**：作业 #8 已完成并确认

**修改要点**：

1. **§1.4**：修正 `loopforge/debug` import 路径说明为 `loopforge/pkg/mcp`
2. **§6 相关文档**：补充当前交叉引用

---

### 作业 #10：修订 `doc/design/spawn-runtime-rules.md`

**类型**：修订现有设计文档  
**前置条件**：作业 #9 已完成并确认

**修改要点**：

1. 尾部"与多文档交叉引用"表：补充指向 `spawn-isolation.md`、`engine-layering.md` ADR 的指针

---

### 作业 #11：新增 `doc/decision/README.md` + 更新 `doc/PRD/README.md`

**类型**：目录索引维护  
**前置条件**：作业 #10 已完成并确认

**修改要点**：

1. 新增 `doc/decision/README.md`（遵循 PRD/README.md 体例）
2. 更新 `doc/PRD/README.md` 决策文档引用表

---

### 作业 #12：最终验证

**类型**：验证  
**前置条件**：作业 #11 已完成并确认

**验证项**：

1. `git diff` 确认仅修改 `doc/` 下的文件
2. 每个 ADR 确认包含：代码片段 + 逻辑图 + 五要素
3. 每个 design 文档确认"（规划）"标记正确
4. 交叉引用全部有效

---

## 5. 主要文件/包影响

| # | 路径/包 | 类型 | 章节 |
|---|---------|------|------|
| 1 | `doc/decision/event-semantics.md` | **新增** | §4 作业 #1 |
| 2 | `doc/decision/spawn-isolation.md` | **新增** | §4 作业 #2 |
| 3 | `doc/decision/engine-layering.md` | **新增** | §4 作业 #3 |
| 4 | `doc/decision/sdk-selection.md` | **修订** | §4 作业 #4 |
| 5 | `doc/design/architecture.md` | **修订** | §4 作业 #5 |
| 6 | `doc/design/abstractions.md` | **修订** | §4 作业 #6 |
| 7 | `doc/design/multi-agent-engine.md` | **修订** | §4 作业 #7 |
| 8 | `doc/design/data-fusion.md` | **修订** | §4 作业 #8 |
| 9 | `doc/design/mcp-tool-unification.md` | **修订** | §4 作业 #9 |
| 10 | `doc/design/spawn-runtime-rules.md` | **修订** | §4 作业 #10 |
| 11 | `doc/decision/README.md` | **新增** | §4 作业 #11 |
| 12 | `doc/PRD/README.md` | **修订** | §4 作业 #11 |

---

## 6. 风险与边界

1. **不修改代码**：纯文档修订，无编译风险
2. **愿景 vs 实现**：设计文档中的"（规划）"标记不删除原有内容
3. **代码事实准确性**：代码片段来自 `pkg/agent/spawn.go`、`spawn_tool.go`、`event/event.go`、`internal/engine/spawn.go`、`pkg/runner/runner.go` 等文件，已通过 `go build ./...` 确认编译通过
4. **ADR 五要素 + 代码片段 + 逻辑图**：每份 ADR 必须同时包含这三部分

---

## 7. 验收标准

1. 新增 3 份 ADR 文档，每份包含：五要素 + 代码片段 + ASCII 逻辑图
2. `doc/decision/sdk-selection.md` 术语表和架构描述与当前代码完全一致
3. `doc/design/` 下 6 份文件全部修订：不存在的模块标记"（规划）"，事实错误修正
4. `doc/decision/README.md` 和 `doc/PRD/README.md` 同步更新
5. `git diff` 确认仅修改 `doc/` 下的文档

---

## 8. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-29 | 初稿：v0.9.5 文档修订计划，覆盖 `doc/decision/` + `doc/design/`；逐个文档确认制。 |

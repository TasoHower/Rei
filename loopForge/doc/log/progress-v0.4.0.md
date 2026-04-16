# loopForge 项目进度日志 — v0.4.0

> **版本**：v0.4.0  
> **日期**：2026-04-16  
> **里程碑**：Agent 架构统一 — Transfer 能力下沉至 RunnerAgent、删除 AgentConfig + Registry、事件报文修正、并发隔离  
> **上一版本**：v0.3.0（Agent Transfer / Handoff）

---

## 本版本目标

**解决 v0.3.0 遗留的三大架构问题**：

1. **Agent 定义冗余**：`transfer.AgentConfig` 与 `agent.RunnerAgent` 字段高度重叠（9/10 字段一致），`runnerFromConfig()` 是纯字段复制。Transfer 是引擎基础能力，应直接在 `RunnerAgent` 上支持。
2. **事件报文混乱**：Transfer 拦截时发射了 `ToolCallStart` 但无 `ToolCallEnd`，形成悬挂报文对。Transfer 是控制流信号，不应产生 `ToolCall` 系列报文。
3. **并发隔离缺失**：多 segment 共享同一 Agent Graph 时，Orchestrator 若直接修改共享 `*RunnerAgent` 实例将产生数据竞争。

---

## 设计决策

| 决策 | 说明 |
|------|------|
| **保留 `pkg/transfer`** | 编排层（Orchestrator）与单 Agent 运行循环（pkg/agent）职责分离，保持代码结构清晰 |
| **去掉 Registry + AgentConfig** | Agent 通过 `AddHandoff` 直接引用目标 Agent，无需字符串查找和注册表间接层 |
| **不新增 FinishTransfer** | `FinishReason` 保持 `tool_calls`（LLM 行为事实描述），Transfer 由 `AgentTransfer` 事件通知即可 |
| **Per-Run Clone** | Orchestrator 每次 run 克隆 agent template，注入 ExtraTools 和 ToolInterceptor 在副本上 |

---

## 问题分析

### 问题 1：Agent 定义冗余

```
agent.RunnerAgent（pkg/agent/runner.go）      transfer.AgentConfig（pkg/transfer/registry.go）
──────────────────────────────────────         ────────────────────────────────────────────
Name               string                      Name               string
ModelName          string                      ModelName          string
SystemInstructions string                      SystemInstructions string
ChatModel          ToolCallingChatModel         ChatModel          ToolCallingChatModel
ToolInfos          []*ToolInfo                  ToolInfos          []*ToolInfo
Executor           ToolExecutor                 Executor           ToolExecutor
MaxSteps           int                          MaxSteps           int
CallOptions        []CallOption                 CallOptions        []CallOption
ExtraTools         []*ToolInfo                  （无）
ToolInterceptor    ToolInterceptor              （无）
（无）                                           Description        string          ← 仅此独有
（无）                                           TransferTargets    []string         ← 仅此独有
```

`runnerFromConfig()` 就是把 `AgentConfig` 的 9 个字段逐一复制到 `RunnerAgent`。

**解法**：`RunnerAgent` 新增 `Description` 字段和 `handoffs []*RunnerAgent`（通过 `AddHandoff` 注册），删除 `AgentConfig` 和 `Registry`。

### 问题 2：事件报文不一致

当前拦截路径（`runner_loop.go:163-186`）发了 `ToolCallStart` 但无 `ToolCallEnd`，与后续 `AgentTransfer` 事件语义重叠。

**解法**：拦截到 transfer 工具时直接返回 `InterceptedCall`，不发射任何 `ToolCall` 事件。`AgentTransfer` 是 Transfer 的唯一信号。

### 问题 3：并发隔离

v0.3.0 因 `runnerFromConfig` 每次新建 `RunnerAgent` 而偶然安全。去掉 `AgentConfig` 后，Orchestrator 直接持有 `*RunnerAgent` 引用，若在其上注入 `ExtraTools`/`ToolInterceptor` 将产生数据竞争。

**解法**：`clone()` per-run 克隆 template agent，在副本上注入运行时字段。`RunLoop` 内部状态全为栈变量，天然隔离。

---

## 新 API 用法

```go
// 构造 Agent
triage := agent.NewRunnerAgent(chat,
    agent.WithName("triage"),
    agent.WithDescription("分诊 Agent"),
    agent.WithSystemInstructions("你是分诊助手..."),
)
expert := agent.NewRunnerAgent(chat,
    agent.WithName("math_expert"),
    agent.WithDescription("数学专家"),
    agent.WithToolInfos(mathTools),
)
writer := agent.NewRunnerAgent(chat,
    agent.WithName("writer"),
    agent.WithDescription("创作者"),
)

// 直接引用注册 handoff 目标（支持循环引用）
triage.AddHandoff(expert, writer)
expert.AddHandoff(triage)

// 编排（入口就是 Agent 本身，无需 Registry）
orch := transfer.NewOrchestrator(triage, transfer.WithMaxTransfers(5))
ch := orch.Run(ctx, req)
```

---

## 交付清单

### Step 1：RunnerAgent 扩展

**文件**：`pkg/agent/runner.go`、`pkg/agent/runner_options.go`

- [ ] **新增 `Description string` 字段**：human-readable，用于 transfer 工具描述
- [ ] **新增 `handoffs []*RunnerAgent`**（未导出）+ `AddHandoff(targets ...*RunnerAgent)` 方法 + `Handoffs() []*RunnerAgent` 访问器
- [ ] **新增 `clone()` 方法**：值拷贝 struct + slice 字段使用新底层数组（`ToolInfos`、`ExtraTools`、`CallOptions`、`handoffs`）；`handoffs` 中的指针不深拷贝（它们是不可变 template）
- [ ] **新增 `WithDescription` RunnerOption**

```go
func (a *RunnerAgent) clone() *RunnerAgent {
    c := *a
    if a.ToolInfos != nil {
        c.ToolInfos = make([]*model.ToolInfo, len(a.ToolInfos))
        copy(c.ToolInfos, a.ToolInfos)
    }
    if a.ExtraTools != nil {
        c.ExtraTools = make([]*model.ToolInfo, len(a.ExtraTools))
        copy(c.ExtraTools, a.ExtraTools)
    }
    if a.CallOptions != nil {
        c.CallOptions = make([]model.CallOption, len(a.CallOptions))
        copy(c.CallOptions, a.CallOptions)
    }
    if a.handoffs != nil {
        c.handoffs = make([]*RunnerAgent, len(a.handoffs))
        copy(c.handoffs, a.handoffs)
    }
    return &c
}
```

### Step 2：事件报文修正

**文件**：`pkg/agent/runner_loop.go`

- [ ] **移除拦截路径的 `ToolCallStart` 发射**（L168-172）：拦截到 transfer 工具时直接返回 `InterceptedCall`
- [ ] **`FinishReason` 保持不变**：`CallLLMEnd.FinishReason` 继续使用 `tool_calls`

### Step 3：pkg/transfer 重构

**删除**：`registry.go`（AgentConfig + Registry）、`registry_test.go`

**保留并修改**：

- [ ] **`tool.go`**：`BuildTools(current string, registry *Registry)` → `BuildTools(current *agent.RunnerAgent)`，从 `current.Handoffs()` 遍历生成 transfer 工具
- [ ] **`orchestrator.go`**：
  - `Orchestrator.Registry` + `EntryAgent string` → `Orchestrator.EntryAgent *agent.RunnerAgent`
  - `NewOrchestrator(registry, "triage")` → `NewOrchestrator(triage)`
  - `runnerFromConfig` → `current.clone()` + 注入 `ExtraTools`/`ToolInterceptor`
  - 新增 `findHandoff(a, name)` 按 Name 查找目标 Agent
  - 删除重复 `runIDFrom`
- [ ] **`doc.go`**：更新包文档

Orchestrator 核心循环：

```go
func (o *Orchestrator) orchestrate(ctx, req, ch) {
    current := o.EntryAgent
    for {
        runner := current.clone()
        runner.ExtraTools = BuildTools(current)
        runner.ToolInterceptor = IsTransferTool

        result := runner.RunLoop(ctx, req, ch, inheritedMsgs, st)
        if result == nil { return }

        targetName := TargetAgent(result.ToolCall.Name)
        next := findHandoff(current, targetName)
        if next == nil {
            emitError("invalid_transfer", "target not found: "+targetName)
            return
        }
        // ... emit AgentTransfer, build inheritedMsgs ...
        current = next
    }
}
```

### Step 4：更新消费方

- [ ] **`cmd/agentdemo/main.go`**：`runTransferDemo` 重写 — 删除 `transfer.NewRegistry()`、`registry.Register(transfer.AgentConfig{...})`、`registry.Validate()`；改为 `agent.NewRunnerAgent(...)` + `AddHandoff` + `transfer.NewOrchestrator(triage)`
- [ ] **`test-server/main.go`**：`buildTransferAgent` 同样重写
- [ ] **`pkg/agent/doc.go`**：更新包文档，说明 Agent 通过 `AddHandoff` 注册 transfer 目标
- [ ] **`pkg/model/interface/chatmodel.go`**：接口文档补充并发安全约束

### Step 5：测试

- [ ] **`pkg/transfer/transfer_test.go`**：适配新 API（`NewRunnerAgent` + `AddHandoff` + `NewOrchestrator(entry)`）；新增事件配对断言（Transfer 无 `ToolCallStart`/`ToolCallEnd`）
- [ ] **`pkg/transfer/helpers_test.go`**：mock 保留，适配新构造方式
- [ ] **删除 `pkg/transfer/registry_test.go`**
- [ ] **新增 `pkg/agent/clone_test.go`**：验证 clone 后修改副本 slice 不影响原始 template
- [ ] **新增并发隔离测试**（`pkg/transfer/transfer_test.go`）：同一 Orchestrator 并发 Run 多个 segment，`go test -race` 验证无数据竞争
- [ ] **回归**：`go build ./...` + `go test ./... -race`

---

## 文件变更总览

| 路径 | 操作 | 说明 |
|------|------|------|
| `pkg/agent/runner.go` | 修改 | 新增 `Description`、`handoffs`、`AddHandoff()`、`Handoffs()`、`clone()` |
| `pkg/agent/runner_options.go` | 修改 | 新增 `WithDescription` |
| `pkg/agent/runner_loop.go` | 修改 | 移除拦截路径的 `ToolCallStart` 发射 |
| `pkg/agent/doc.go` | 修改 | 更新包文档 |
| `pkg/agent/clone_test.go` | **新建** | clone 隔离测试 |
| `pkg/transfer/tool.go` | 修改 | `BuildTools` 改为接受 `*agent.RunnerAgent`，从 Handoffs 读取 |
| `pkg/transfer/orchestrator.go` | 修改 | 删除 Registry，`EntryAgent` 改为 `*agent.RunnerAgent`，使用 clone |
| `pkg/transfer/doc.go` | 修改 | 更新包文档 |
| `pkg/transfer/registry.go` | **删除** | AgentConfig + Registry 不再需要 |
| `pkg/transfer/registry_test.go` | **删除** | 随 Registry 删除 |
| `pkg/transfer/transfer_test.go` | 修改 | 适配新 API + 新增事件/并发测试 |
| `pkg/transfer/helpers_test.go` | 修改 | 适配新构造方式 |
| `pkg/model/interface/chatmodel.go` | 修改 | 并发安全约束文档 |
| `cmd/agentdemo/main.go` | 修改 | 适配新 API |
| `test-server/main.go` | 修改 | 适配新 API |

---

## 重构前后对比

### 包结构

```
Before (v0.3.0)                              After (v0.4.0)
─────────────                                ─────────────
pkg/agent/                                   pkg/agent/
├── runner.go      (RunnerAgent)             ├── runner.go      (+ Description, handoffs, clone)
├── runner_loop.go                           ├── runner_loop.go  (- ToolCallStart on intercept)
├── runner_options.go                        ├── runner_options.go (+ WithDescription)
└── ...                                      ├── clone_test.go   (新建)
                                             └── ...
pkg/transfer/
├── registry.go    (AgentConfig + Registry)  （删除）
├── registry_test.go                         （删除）
├── orchestrator.go (Registry + name 查找)    ├── orchestrator.go (*RunnerAgent + Handoffs + clone)
├── tool.go        (Registry 依赖)           ├── tool.go         (*RunnerAgent 直接引用)
└── ...                                      └── ...
```

### API 变化

```go
// Before (v0.3.0) — 15 行，间接引用
registry := transfer.NewRegistry()
registry.Register(transfer.AgentConfig{
    Name:        "triage",
    Description: "分诊",
    ChatModel:   chat,
    TransferTargets: []string{"expert", "writer"},
    // ... 9 个字段逐一填写 ...
})
registry.Register(transfer.AgentConfig{Name: "expert", ChatModel: chat, ...})
registry.Register(transfer.AgentConfig{Name: "writer", ChatModel: chat, ...})
registry.Validate()
orch := transfer.NewOrchestrator(registry, "triage", transfer.WithMaxTransfers(5))

// After (v0.4.0) — 5 行，直接引用
triage := agent.NewRunnerAgent(chat, agent.WithName("triage"), agent.WithDescription("分诊"), ...)
expert := agent.NewRunnerAgent(chat, agent.WithName("expert"), ...)
writer := agent.NewRunnerAgent(chat, agent.WithName("writer"), ...)
triage.AddHandoff(expert, writer)
orch := transfer.NewOrchestrator(triage, transfer.WithMaxTransfers(5))
```

### 事件流修正

```
Before (v0.3.0，有问题)                        After (v0.4.0，修正)
─────────────────────                          ─────────────────────
EventCallLLMEnd{finish: "tool_calls"}          EventCallLLMEnd{finish: "tool_calls"}
EventToolCallStart{name: "transfer_to_math"}   （无 — transfer 不产生 ToolCall 事件）
EventAgentTransfer{from, to, reason}           EventAgentTransfer{from, to, reason}
```

### 并发隔离

```
                   EntryAgent (template, 只读)
                        │
           ┌────────────┼────────────┐
           ▼                         ▼
    Segment A                 Segment B
    current.clone()           current.clone()
    runner.ExtraTools = ...   runner.ExtraTools = ...
    runner.RunLoop(...)       runner.RunLoop(...)
    栈变量互不干扰 ✓            栈变量互不干扰 ✓
```

---

## Transfer vs Spawn vs Network（语义对照）

| 维度 | Transfer（v0.4.0） | Spawn（后续） | Network（后续） |
|------|-------------------|-------------|----------------|
| 控制流 | 平级移交，A 退出 B 接管 | 父子递归 | 编排图 |
| 事件 | `AgentTransfer`（唯一信号） | `SpawnStart`/`SpawnEnd` | `NetworkSlot*` |
| ToolCall 报文 | **不产生** | **产生**完整 Start/End 对 | N/A |
| FinishReason | `tool_calls`（LLM 行为描述） | `tool_calls` | N/A |

---

## 不包含（后续版本）

- **Spawn（父子递归子任务）**：v0.5.0
- **静态 Network**：v0.5.0+
- **RunnerAgent 更名**：评估后决定
- **MCP client / Skills 注入**：独立里程碑

---

## 提交记录（开发日志）

| 日期 | 说明 |
|------|------|
| 2026-04-16 | 版本规划；进度日志初始化。 |
| 2026-04-16 | 方案迭代：去掉 FinishTransfer（FinishReason 是 LLM 行为描述，不应混入引擎控制流）；保留 pkg/transfer 包（编排层与 Agent 运行循环职责分离）；去掉 Registry（Agent 通过 AddHandoff 直接引用目标，无需字符串查找）。 |
| 2026-04-16 | **代码交付完成**。全部 5 步执行完毕，`go build ./...` + `go test ./... -race` 通过。 |
| 2026-04-16 | 补充：Orchestrator 为有 handoff 目标的 agent 自动注入 transfer 引导 prompt（`BuildTransferPrompt`）。 |

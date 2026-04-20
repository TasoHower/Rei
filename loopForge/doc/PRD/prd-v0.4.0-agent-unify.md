# loopForge 产品需求文档 — v0.4.0 Agent 架构统一

> **版本**：v0.4.0  
> **日期**：2026-04-16  
> **里程碑**：Agent 架构统一 — Transfer 能力下沉至 RunnerAgent、删除 AgentConfig + Registry、事件报文修正、并发隔离  
> **状态**：已完成  
> **关联文档**：[progress-v0.4.0.md](../log/progress-v0.4.0.md)、[progress-v0.3.0.md](../log/progress-v0.3.0.md)

---

## 1. 概述

### 1.1 产品定位

**v0.4.0** 是 loopForge 的**架构简化版本**，解决 v0.3.0 遗留的三大架构问题：Agent 定义冗余、事件报文混乱、并发隔离缺失。通过简化 Agent 定义、修正事件报文、引入 per-run 克隆，提升代码质量和运行时安全性。

### 1.2 核心价值

1. **简化设计** — 去掉冗余的 `AgentConfig` 和 `Registry`，直接在 `RunnerAgent` 上支持 Transfer
2. **事件清晰** — Transfer 不产生 `ToolCall` 事件，仅发射 `AgentTransfer` 事件
3. **并发安全** — per-run 克隆 agent template，避免数据竞争
4. **职责分离** — 编排层（Orchestrator）与单 Agent 运行循环（pkg/agent）职责分离

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.3.0** | Agent Transfer / Handoff；v0.4.0 在此基础上简化架构 |
| **v0.4.1** | 包拆分 + 命名统一 + 结构化日志；v0.4.0 的代码在 v0.4.1 中进一步重构 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.4.0）

#### P0（必须实现）

1. **简化 Agent 定义**
   - `RunnerAgent` 新增 `Description` 字段和 `handoffs []*RunnerAgent`
   - 通过 `AddHandoff` 注册目标 Agent
   - 删除 `AgentConfig` 和 `Registry`

2. **修正事件报文**
   - Transfer 拦截时不发射 `ToolCall` 事件
   - `AgentTransfer` 是 Transfer 的唯一信号
   - `FinishReason` 保持 `tool_calls`（LLM 行为事实描述）

3. **并发隔离**
   - Orchestrator 每次 run 克隆 agent template
   - 在副本上注入 `ExtraTools` 和 `ToolInterceptor`
   - `RunLoop` 内部状态全为栈变量，天然隔离

#### P1（可选增强）

- 文档完善（包文档、示例代码）
- 性能优化（减少不必要的克隆）

### 2.2 不在本版本范围

1. **Transfer 功能增强** — 不支持多阶段 Transfer、条件 Transfer
2. **Spawn 能力** — 父子递归子任务留待后续版本
3. **持久化** — Agent 状态不持久化

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够直接在 `RunnerAgent` 上注册 Transfer 目标  
**验收标准**：
- 调用 `agent.AddHandoff(targetAgent)` 注册目标
- 无需通过 `Registry` 间接注册
- 支持多个 Transfer 目标

**US-2**：能够看到清晰的 Transfer 事件流  
**验收标准**：
- Transfer 时仅发射 `AgentTransfer` 事件
- 不发射 `ToolCallStart` / `ToolCallEnd` 事件
- 事件包含 `FromAgent` / `ToAgent` / `Reason` 字段

**US-3**：能够安全地并发运行多个 Agent  
**验收标准**：
- 多 goroutine 运行同一 agent template 无数据竞争
- 每 run 克隆副本，互不干扰
- 测试通过 race detector

**US-4**：能够使用简洁的 API 创建 Agent  
**验收标准**：
- 直接创建 `RunnerAgent`，无需 `AgentConfig`
- 通过 `With*` Option 配置字段
- 代码简洁易读

### 3.2 作为 Agent，我希望...

**US-5**：能够在 Transfer 时正确交接给下游 Agent  
**验收标准**：
- Transfer 时传递对话历史给目标 Agent
- 目标 Agent 使用自身的工具集和系统指令
- 事件流对消费方透明连续

**US-6**：能够在 Transfer 后正确退出  
**验收标准**：
- Transfer 后当前 Agent 退出
- 不发射多余的 `ToolCallEnd` 事件
- `FinishReason` 正确描述 LLM 行为

---

## 4. 功能需求

### 4.1 Agent 定义简化

#### 4.1.1 RunnerAgent 结构

```go
type RunnerAgent struct {
    Name               string
    Description        string  // 新增：Agent 描述
    ModelName          string
    SystemInstructions string
    ChatModel          ToolCallingChatModel
    ToolInfos          []*ToolInfo
    Executor           ToolExecutor
    MaxSteps           int
    CallOptions        []CallOption
    ExtraTools         []*ToolInfo
    ToolInterceptor    ToolInterceptor
    handoffs           []*RunnerAgent  // 新增：Transfer 目标（通过 AddHandoff 注册）
}
```

#### 4.1.2 AddHandoff 方法

```go
// AddHandoff registers a target agent for transfer.
func (a *RunnerAgent) AddHandoff(target *RunnerAgent) {
    a.handoffs = append(a.handoffs, target)
}
```

#### 4.1.3 删除的类型

```go
// 删除 AgentConfig（冗余）
// type AgentConfig struct { ... }

// 删除 Registry（不再需要）
// type Registry struct { ... }
```

### 4.2 事件报文修正

#### 4.2.1 Transfer 拦截逻辑

```go
// 拦截到 transfer 工具时：
if isTransferTool(toolCall) {
    // 不发射 ToolCallStart/ToolCallEnd 事件
    // 直接返回 InterceptedCall
    return &InterceptedCall{
        TargetAgent: targetAgent(toolCall),
        Reason:      extractReason(toolCall),
    }
}
```

#### 4.2.2 AgentTransfer 事件

```go
// 发射 AgentTransfer 事件（Transfer 的唯一信号）
emit(&RuntimeEvent{
    Type: EventAgentTransfer,
    Payload: &AgentTransferPayload{
        FromAgent: current.Name,
        ToAgent:   target.Name,
        Reason:    reason,
    },
})
```

#### 4.2.3 FinishReason

```go
// FinishReason 保持 tool_calls（LLM 行为事实描述）
outcome.FinishReason = "tool_calls"
```

### 4.3 并发隔离

#### 4.3.1 Per-Run Clone

```go
// Orchestrator 每次 run 克隆 agent template
func (o *Orchestrator) run(ctx context.Context, req *Request) (<-chan *event.RuntimeEvent, error) {
    // 克隆 template agent
    agentClone := o.entryAgent.Clone()
    
    // 在副本上注入运行时字段
    agentClone.ExtraTools = buildTransferTools(agentClone, o.registry)
    agentClone.ToolInterceptor = o.buildInterceptor()
    
    // 运行克隆的 agent
    return agentClone.Run(ctx, req)
}
```

#### 4.3.2 Clone 方法

```go
// Clone creates a shallow copy of the agent.
// LoopState internal state is all stack variables, naturally isolated.
func (a *RunnerAgent) Clone() *RunnerAgent {
    return &RunnerAgent{
        Name:               a.Name,
        Description:        a.Description,
        ModelName:          a.ModelName,
        SystemInstructions: a.SystemInstructions,
        ChatModel:          a.ChatModel,
        ToolInfos:          append([]*ToolInfo(nil), a.ToolInfos...),
        Executor:           a.Executor,
        MaxSteps:           a.MaxSteps,
        CallOptions:        append([]CallOption(nil), a.CallOptions...),
        ExtraTools:         append([]*ToolInfo(nil), a.ExtraTools...),
        ToolInterceptor:    a.ToolInterceptor,
        handoffs:           append([]*RunnerAgent(nil), a.handoffs...),
    }
}
```

### 4.4 API 用法

#### 4.4.1 创建 Agent

```go
// 创建 Agent
triage := agent.NewRunnerAgent(chat,
    agent.WithName("triage"),
    agent.WithDescription("分诊 Agent"),
    agent.WithSystemInstructions("你是分诊助手..."),
    agent.WithTools(tools),
)

expert := agent.NewRunnerAgent(chat,
    agent.WithName("math_expert"),
    agent.WithDescription("数学专家"),
    agent.WithSystemInstructions("你是数学专家..."),
    agent.WithTools(expertTools),
)
```

#### 4.4.2 注册 Transfer 目标

```go
// 注册 Transfer 目标
triage.AddHandoff(expert)
```

#### 4.4.3 运行 Agent

```go
// 直接运行单个 Agent
req := &Request{
    Messages: []model.Message{userMessage},
}
eventChan := triage.Run(ctx, req)

// 消费事件流
for event := range eventChan {
    handleEvent(event)
}
```

#### 4.4.4 使用 Orchestrator

```go
// 使用 Orchestrator 运行多 Agent
registry := transfer.NewRegistry()
registry.Register(&transfer.AgentConfig{
    Name:              "triage",
    Description:       "分诊 Agent",
    ChatModel:         chat,
    ToolInfos:         tools,
    SystemInstructions: "...",
    TransferTargets:    []string{"math_expert"},
})
registry.Register(&transfer.AgentConfig{
    Name:              "math_expert",
    Description:       "数学专家",
    ChatModel:         chat,
    ToolInfos:         expertTools,
    SystemInstructions: "...",
})

orchestrator := transfer.NewOrchestrator(triage, registry,
    transfer.WithMaxTransfers(5),
)

eventChan := orchestrator.Run(ctx, req)
```

---

## 5. 技术需求

### 5.1 设计决策

#### 5.1.1 保留 `pkg/transfer`

**理由**：
- 编排层（Orchestrator）与单 Agent 运行循环（pkg/agent）职责分离
- 保持代码结构清晰
- 便于未来扩展（如支持更复杂的编排策略）

#### 5.1.2 去掉 Registry + AgentConfig

**理由**：
- Agent 通过 `AddHandoff` 直接引用目标 Agent
- 无需字符串查找和注册表间接层
- 简化设计，减少冗余

#### 5.1.3 不新增 FinishTransfer

**理由**：
- `FinishReason` 保持 `tool_calls`（LLM 行为事实描述）
- Transfer 由 `AgentTransfer` 事件通知即可
- 避免过度设计

#### 5.1.4 Per-Run Clone

**理由**：
- 多 segment 共享同一 Agent Graph 时，避免数据竞争
- 在副本上注入运行时字段，template 保持不变
- `RunLoop` 内部状态全为栈变量，天然隔离

### 5.2 错误处理

#### 5.2.1 Transfer 目标不存在

```go
if target == nil {
    return fmt.Errorf("transfer target %q not found", targetName)
}
```

#### 5.2.2 超过最大 Transfer 次数

```go
if state.TransferCount >= o.maxTransfers {
    return fmt.Errorf("max transfers (%d) exceeded", o.maxTransfers)
}
```

### 5.3 观测性

#### 5.3.1 日志（slog）

```go
// Agent Transfer
slog.Info("Agent transfer",
    "from", current.Name,
    "to", target.Name,
    "reason", reason,
)

// Max transfers exceeded
slog.Error("Max transfers exceeded",
    "agent", current.Name,
    "count", state.TransferCount,
)
```

#### 5.3.2 事件

- `AgentTransfer` — Transfer 事件（含 `FromAgent` / `ToAgent` / `Reason`）
- `RuntimeOutcome.TransferChain` — 记录经过的 Agent 名称

---

## 6. 验收标准

### 6.1 功能验收

- [ ] 可直接在 `RunnerAgent` 上注册 Transfer 目标
- [ ] 无需 `AgentConfig` 和 `Registry`
- [ ] Transfer 时仅发射 `AgentTransfer` 事件
- [ ] `FinishReason` 正确描述 LLM 行为
- [ ] 多 goroutine 运行无数据竞争

### 6.2 性能验收

- [ ] Clone 操作时间 < 10μs
- [ ] 内存占用增加 < 1KB/clone
- [ ] Transfer 延迟 < 1ms

### 6.3 并发验收

- [ ] 通过 race detector（`go test -race`）
- [ ] 多 segment 并发运行无冲突
- [ ] Transfer 链正确传递

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **Clone 性能开销** | 每 run 克隆可能增加性能开销 | 浅克隆、性能测试、必要时优化 |
| **API 破坏性变更** | 删除 `AgentConfig` / `Registry` 可能破坏现有代码 | 迁移指南、示例代码更新 |
| **Transfer 循环** | 可能形成 A→B→A 的循环 Transfer | `MaxTransfers` 限制、文档说明 |
| **状态同步** | Clone 后状态不同步，可能导致行为不一致 | 文档明确边界、代码审查 |

---

## 8. 参考文档

- [progress-v0.4.0.md](../log/progress-v0.4.0.md) — 版本进度日志
- [progress-v0.3.0.md](../log/progress-v0.3.0.md) — 前置版本
- [OpenAI Swarm](https://github.com/openai/swarm) — Agent handoff 语义参考

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-16 | v0.4.0 | 初始版本 |

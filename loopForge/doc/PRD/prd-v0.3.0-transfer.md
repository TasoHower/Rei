# loopForge 产品需求文档 — v0.3.0 Agent Transfer

> **版本**：v0.3.0  
> **日期**：2026-04-15  
> **里程碑**：Agent Transfer（Handoff） — 多 Agent 注册 + 平级移交，loopForge 从 React Agent SDK 升级为 Multi-Agent SDK  
> **状态**：已完成  
> **关联文档**：[progress-v0.3.0.md](../log/progress-v0.3.0.md)、[progress-v0.2.0.md](../log/progress-v0.2.0.md)

---

## 1. 概述

### 1.1 产品定位

**v0.3.0** 是 loopForge 的**里程碑版本**，实现 **Agent Transfer（交接/移交）** 能力：多个具名 Agent 注册到 `Registry`，运行时由模型通过 `transfer_to_{name}` 工具将控制权**平级移交**给目标 Agent。通过此版本，loopForge 从 React Agent SDK 升级为 **Multi-Agent SDK**。

### 1.2 核心价值

1. **多 Agent 协作** — 支持多个 Agent 平级移交，各司其职
2. **透明连续** — Transfer 时继承对话历史，事件流对消费方透明连续
3. **类型安全** — Transfer 工具自动生成，参数和返回值类型安全
4. **可观测** — `AgentTransfer` 事件、`TransferChain` 完整记录移交链

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.2.0** | 流式事件驱动；v0.3.0 在此基础上增加 Transfer 能力 |
| **v0.4.0** | Agent 架构统一；v0.3.0 的 `AgentConfig` / `Registry` 在 v0.4.0 中简化 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.3.0）

#### P0（必须实现）

1. **`pkg/transfer` 包**
   - Transfer 能力独立为 `pkg/transfer/`，与 `pkg/agent/` 解耦
   - 编排层（Orchestrator）与单 Agent 运行循环职责分离

2. **`transfer.Registry`**
   - 具名 Agent 配置注册表
   - `AgentConfig` 含 `Name`、`Description`、`ChatModel`、`ToolInfos`、`SystemInstructions`、`TransferTargets` 等字段
   - 提供 `Register` / `Get` / `Names` / `TransferTargetsFor` / `Validate` 方法

3. **Transfer 工具生成**
   - `transfer.BuildTools(current, registry)` 根据当前 Agent 的 `TransferTargets` 自动生成 `transfer_to_{name}` 工具
   - JSON Schema 参数 `{ "reason": "string" }`
   - 工具描述包含目标 Agent 的 `Description`

4. **`transfer.Orchestrator` 编排器**
   - 实现 `agent.Agent` 接口
   - 持有 `Registry` + `EntryAgent` + `MaxTransfers`
   - 内部循环：构造 RunnerAgent → 执行 → 若触发 transfer → 构造目标 RunnerAgent → 继续
   - `MaxTransfers` 防止循环移交

5. **RunnerAgent 重构**
   - 包重组：原 `runner_agent.go` 拆分为 `runner.go`、`runner_loop.go`、`runner_stream.go`、`runner_tools.go`、`runner_helpers.go`
   - 通用拦截机制：新增 `ToolInterceptor` + `ExtraTools`
   - 对话历史传递：Transfer 时传递 `msgs` 给目标 Agent

6. **事件层扩展**
   - `AgentTransferPayload` 新增 `FromAgent` / `ToAgent` / `Reason` 字段
   - `RuntimeOutcome.TransferChain` 按顺序记录经过的 Agent 名称

7. **Demo & Test**
   - CLI Demo：三 Agent 场景（`triage` / `math_expert` / `writer`）
   - test-server Transfer 模式：Web UI 增加 Single / Transfer 模式切换
   - 测试：Registry 单测、Orchestrator 集成测试

#### P1（可选增强）

- Transfer 工具描述优化（含示例）
- Transfer 链可视化（test-server）

### 2.2 不在本版本范围

1. **Spawn 能力** — 父子递归子任务留待后续版本
2. **动态注册** — 不支持运行时动态注册 Agent
3. **条件 Transfer** — 不支持基于条件的 Transfer

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够注册多个 Agent 到 Registry  
**验收标准**：
- 创建 `AgentConfig` 并调用 `registry.Register(config)`
- 支持多个 Agent 注册
- 重复名称校验失败

**US-2**：能够配置 Transfer 目标  
**验收标准**：
- 在 `AgentConfig` 中设置 `TransferTargets`
- 支持多个 Transfer 目标
- 目标必须存在于 Registry 中

**US-3**：能够自动生成 Transfer 工具  
**验收标准**：
- 调用 `transfer.BuildTools(current, registry)`
- 自动生成 `transfer_to_{name}` 工具
- 工具描述包含目标 Agent 的 `Description`

**US-4**：能够运行多 Agent Transfer 场景  
**验收标准**：
- 创建 `Orchestrator` 并调用 `Run()`
- Transfer 时正确切换到目标 Agent
- 对话历史正确传递

**US-5**：能够看到 Transfer 事件流  
**验收标准**：
- 事件流包含 `AgentTransfer` 事件
- 事件包含 `FromAgent` / `ToAgent` / `Reason` 字段
- `TransferChain` 记录完整链路

### 3.2 作为 Agent，我希望...

**US-6**：能够使用 `transfer_to_{name}` 工具移交控制权  
**验收标准**：
- LLM 调用 `transfer_to_{name}` 工具
- 工具参数包含 `reason`
- 拦截工具调用并切换到目标 Agent

**US-7**：能够继承上游 Agent 的对话历史  
**验收标准**：
- Transfer 时传递 `msgs` 给目标 Agent
- 替换 system 消息为目标 Agent 的 `SystemInstructions`
- 附加合成 tool result 闭合悬挂的 tool call

---

## 4. 功能需求

### 4.1 Registry

#### 4.1.1 AgentConfig

```go
type AgentConfig struct {
    Name               string
    Description        string
    ChatModel          ToolCallingChatModel
    ToolInfos          []*ToolInfo
    SystemInstructions string
    TransferTargets    []string  // 可移交的目标 Agent 名称列表
}
```

#### 4.1.2 Registry 方法

```go
type Registry struct {
    agents map[string]*AgentConfig
}

func NewRegistry() *Registry

// Register registers an agent config.
func (r *Registry) Register(config *AgentConfig)

// Get gets an agent config by name.
func (r *Registry) Get(name string) (*AgentConfig, error)

// Names returns all registered agent names.
func (r *Registry) Names() []string

// TransferTargetsFor returns the transfer targets for an agent.
func (r *Registry) TransferTargetsFor(name string) ([]string, error)

// Validate validates the registry (no duplicates, targets exist).
func (r *Registry) Validate() error
```

### 4.2 Transfer 工具

#### 4.2.1 BuildTools

```go
// BuildTools builds transfer tools for the current agent based on its TransferTargets.
func BuildTools(current *RunnerAgent, registry *Registry) []*ToolInfo {
    targets, _ := registry.TransferTargetsFor(current.Name)
    
    var tools []*ToolInfo
    for _, targetName := range targets {
        target, _ := registry.Get(targetName)
        
        tool := &ToolInfo{
            Name:        "transfer_to_" + targetName,
            Description: fmt.Sprintf("Transfer control to %s: %s", targetName, target.Description),
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "reason": map[string]any{
                        "type":        "string",
                        "description": "Reason for transfer",
                    },
                },
                "required": []string{"reason"},
            },
        }
        tools = append(tools, tool)
    }
    
    return tools
}
```

### 4.3 Orchestrator

#### 4.3.1 结构

```go
type Orchestrator struct {
    registry      *Registry
    entryAgent    *RunnerAgent
    maxTransfers  int
}
```

#### 4.3.2 Run 方法

```go
func (o *Orchestrator) Run(ctx context.Context, req *Request) (<-chan *event.RuntimeEvent, error) {
    eventChan := make(chan *event.RuntimeEvent, 64)
    
    go func() {
        defer close(eventChan)
        
        current := o.entryAgent
        msgs := req.Messages
        transferCount := 0
        
        for {
            // 检查最大 Transfer 次数
            if transferCount >= o.maxTransfers {
                emitError("max transfers exceeded")
                return
            }
            
            // 构造 RunnerAgent
            agent := buildRunnerAgent(current, o.registry)
            agent.ExtraTools = BuildTools(current, o.registry)
            agent.ToolInterceptor = o.buildInterceptor()
            
            // 运行 Agent
            outcome, err := agent.Run(ctx, &Request{Messages: msgs})
            if err != nil {
                emitError(err)
                return
            }
            
            // 消费事件流
            for event := range outcome.EventChan {
                eventChan <- event
                
                // 拦截 Transfer
                if event.Type == EventToolCall && isTransferTool(event.ToolCall()) {
                    targetName := TargetAgent(event.ToolCall())
                    reason := ExtractReason(event.ToolCall())
                    
                    // 发射 AgentTransfer 事件
                    emit(&AgentTransferPayload{
                        FromAgent: current.Name,
                        ToAgent:   targetName,
                        Reason:    reason,
                    })
                    
                    // 切换到目标 Agent
                    current, _ = o.registry.Get(targetName)
                    msgs = buildTransferMessages(outcome, current)
                    transferCount++
                    break
                }
            }
            
            // 无 Transfer，结束
            if outcome.FinishReason != "tool_calls" {
                return
            }
        }
    }()
    
    return eventChan, nil
}
```

### 4.4 对话历史传递

#### 4.4.1 buildTransferMessages

```go
// buildTransferMessages builds messages for the target agent.
func buildTransferMessages(outcome *RuntimeOutcome, target *AgentConfig) []Message {
    // 复制消息
    msgs := append([]Message(nil), outcome.Messages...)
    
    // 替换 system 消息
    msgs = replaceSystemMessage(msgs, target.SystemInstructions)
    
    // 附加合成 tool result
    syntheticResult := &ToolCall{
        Name:      "transfer_to_" + target.Name,
        Arguments: `{"reason": "Transfer from previous agent"}`,
        Result:    "Transfer successful",
    }
    msgs = append(msgs, Message{
        Role:    "tool",
        Content: syntheticResult.Result,
    })
    
    return msgs
}
```

---

## 5. 技术需求

### 5.1 架构示意图

```
┌─────────────────────────────────────────────────────────────┐
│                     transfer.Orchestrator                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Registry   │  │  EntryAgent  │  │ MaxTransfers │      │
│  └──────┬───────┘  └──────────────┘  └──────────────┘      │
│         │                                                    │
│         ▼                                                    │
│  ┌─────────────────────────────────────────────────────┐   │
│  │              Transfer Loop                            │   │
│  │  1. build RunnerAgent from AgentConfig                │   │
│  │  2. inject ExtraTools (transfer_to_*)                 │   │
│  │  3. inject ToolInterceptor                            │   │
│  │  4. run agent                                         │   │
│  │  5. if transfer → switch to target agent              │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
         │
         │ implements agent.Agent interface
         ▼
┌─────────────────────────────────────────────────────────────┐
│                      pkg/agent.Agent                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  Runnable    │  │  Run()       │  │  EventChan   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 错误处理

#### 5.2.1 Registry 错误

```go
// ErrAgentNotFound returned when getting a non-existent agent.
var ErrAgentNotFound = errors.New("agent not found")

// ErrDuplicateAgent returned when registering duplicate names.
var ErrDuplicateAgent = errors.New("duplicate agent name")

// ErrInvalidTransferTarget returned when transfer target doesn't exist.
var ErrInvalidTransferTarget = errors.New("invalid transfer target")
```

### 5.3 观测性

#### 5.3.1 日志

```go
// Agent registered
slog.Info("Agent registered", "name", config.Name)

// Transfer
slog.Info("Agent transfer",
    "from", current.Name,
    "to", target.Name,
    "reason", reason,
)

// Max transfers exceeded
slog.Error("Max transfers exceeded",
    "count", transferCount,
    "max", o.maxTransfers,
)
```

#### 5.3.2 事件

- `AgentTransfer` — Transfer 事件（含 `FromAgent` / `ToAgent` / `Reason`）
- `RuntimeOutcome.TransferChain` — 记录经过的 Agent 名称

---

## 6. 验收标准

### 6.1 功能验收

- [ ] 可注册多个 Agent 到 Registry
- [ ] 可配置 Transfer 目标
- [ ] 自动生成 Transfer 工具
- [ ] 可运行多 Agent Transfer 场景
- [ ] Transfer 时正确切换 Agent
- [ ] 对话历史正确传递
- [ ] TransferChain 记录完整链路

### 6.2 性能验收

- [ ] Transfer 延迟 < 10ms
- [ ] Registry 查找时间 < 1μs
- [ ] 内存占用 < 1KB/Agent

### 6.3 测试验收

- [ ] Registry 单测通过（8 用例）
- [ ] Orchestrator 集成测试通过（3 用例）
- [ ] Demo 运行正常

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **Transfer 循环** | 可能形成 A→B→A 的循环 Transfer | `MaxTransfers` 限制、文档说明 |
| **状态同步** | Transfer 时状态可能不同步 | 对话历史传递、合成 tool result |
| **性能开销** | 多 Agent 切换可能增加延迟 | 性能测试、必要时优化 |
| **API 复杂度** | Registry + Orchestrator 增加学习成本 | 文档清晰、示例代码 |

---

## 8. 参考文档

- [progress-v0.3.0.md](../log/progress-v0.3.0.md) — 版本进度日志
- [progress-v0.2.0.md](../log/progress-v0.2.0.md) — 前置版本
- [OpenAI Swarm](https://github.com/openai/swarm) — Agent handoff 语义参考

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-15 | v0.3.0 | 初始版本 |

# loopForge 项目进度日志 — v0.3.0

> **版本**：v0.3.0  
> **日期**：2026-04-15  
> **里程碑**：Agent Transfer（Handoff） — 多 Agent 注册 + 平级移交，loopForge 从 React Agent SDK 升级为 Multi-Agent SDK  
> **上一版本**：v0.2.0（流式事件驱动）

---

## 本版本目标

实现 **Agent Transfer（交接/移交）** 能力：多个具名 Agent 注册到 `Registry`，运行时由模型通过 `transfer_to_{name}` 工具将控制权**平级移交**给目标 Agent。目标 Agent 继承对话历史、使用自身工具集与系统指令继续执行，事件流对消费方透明连续。通过 `Orchestrator` 顶层编排器对外提供与单 Agent 一致的 `Agent` 接口。

**设计对齐**：OpenAI Swarm / Agents SDK 的 handoff 语义。Transfer 是**平级移交**（A 退出、B 接管）；与 Spawn（父子递归子任务）正交，Spawn 留后续版本。

---

## 交付清单

### 核心架构变更

- [x] **`pkg/transfer` 包**：Transfer 能力独立为 `pkg/transfer/`，与 `pkg/agent/` 解耦
- [x] **`transfer.Registry`**：具名 Agent 配置注册表（`pkg/transfer/registry.go`）；`AgentConfig` 含 `Name`、`Description`、`ChatModel`、`ToolInfos`、`SystemInstructions`、`TransferTargets` 等字段；提供 `Register` / `Get` / `Names` / `TransferTargetsFor` / `Validate` 方法
- [x] **Transfer 工具生成**：`transfer.BuildTools(current, registry)` 根据当前 Agent 的 `TransferTargets`（或全部其他 Agent）自动生成 `transfer_to_{name}` 工具描述（`pkg/transfer/tool.go`）；JSON Schema 参数 `{ "reason": "string" }`；工具描述包含目标 Agent 的 `Description`
- [x] **`transfer.Orchestrator` 编排器**：实现 `agent.Agent` 接口（`pkg/transfer/orchestrator.go`）；持有 `Registry` + `EntryAgent` + `MaxTransfers`；内部循环：构造 RunnerAgent → 执行 → 若触发 transfer → 构造目标 RunnerAgent → 继续；`MaxTransfers` 防止循环移交

### RunnerAgent 重构

- [x] **包重组**：原 `runner_agent.go`（403 行）拆分为 `runner.go`（类型定义）、`runner_loop.go`（RunLoop 主骨架）、`runner_stream.go`（流式消费）、`runner_tools.go`（工具执行）、`runner_helpers.go`（设置与消息构建）
- [x] **通用拦截机制**：新增 `ToolInterceptor` + `ExtraTools` 字段，RunnerAgent 不再直接依赖 transfer 概念。Orchestrator 通过这两个钩子注入 transfer 工具并拦截调用
- [x] **对话历史传递**：Transfer 时将当前 `msgs`（替换 system 为目标 Agent 的 `SystemInstructions`）传递给新 Agent，附加合成 tool result 闭合悬挂的 tool call

### 事件层

- [x] **`AgentTransferPayload` 扩展**：新增 `FromAgent` / `ToAgent` / `Reason` 字段，消费方可追踪完整移交链
- [x] **`RuntimeOutcome.TransferChain`**：按顺序记录经过的 Agent 名称

### Demo & test-server

- [x] **CLI Demo**：三 Agent 场景 — `triage` / `math_expert` / `writer`；`DEMO_MODE=transfer` 启用
- [x] **test-server Transfer 模式**：Web UI 增加 Single / Transfer 模式切换；transfer 事件渲染为 `from → to` 横幅 + reason；DONE 事件展示 Transfer Chain 链路条

### 测试

- [x] **`pkg/transfer/registry_test.go`**：注册、查找、重复名称校验、TransferTargets 校验（8 个用例）
- [x] **`pkg/transfer/transfer_test.go`**：mock ChatModel → 验证 transfer 事件流、Metrics 聚合、TransferChain（3 个用例）
- [x] **`pkg/agent/runner_agent_tool_test.go`**：tool loop 调用 + standalone 回归（2 个用例）

---

## 变更文件

| 路径 | 变更 |
|------|------|
| `pkg/agent/runner.go` | 新建：`RunnerAgent` 类型定义 + `Run()` + `ToolInterceptor` / `InterceptedCall` / `LoopState` 类型 |
| `pkg/agent/runner_loop.go` | 新建：`RunLoop()` 主骨架（从原 `runner_agent.go` 拆分） |
| `pkg/agent/runner_stream.go` | 新建：`consumeStream()` 流式响应消费 |
| `pkg/agent/runner_tools.go` | 新建：`executeToolCalls()` 工具调用执行 |
| `pkg/agent/runner_helpers.go` | 新建：`bindModel()` / `buildMessages()` / `replaceSystemMessage()` 等辅助函数 |
| `pkg/agent/runner_options.go` | 修改：去掉 `WithRegistry`，新增 `WithExtraTools` / `WithToolInterceptor` |
| `pkg/agent/runner_agent.go` | 删除：拆分为上述 5 个文件 |
| `pkg/transfer/doc.go` | 新建：包文档 |
| `pkg/transfer/registry.go` | 新建：`AgentConfig` + `Registry`（从 `pkg/agent` 迁移） |
| `pkg/transfer/orchestrator.go` | 新建：`Orchestrator` 实现 `agent.Agent`，管理 transfer 循环 + 合成 tool result 闭合 |
| `pkg/transfer/orchestrator_option.go` | 新建：`WithMaxTransfers` |
| `pkg/transfer/tool.go` | 新建：`BuildTools` / `IsTransferTool` / `TargetAgent` / `ExtractReason` |
| `pkg/transfer/registry_test.go` | 新建：Registry 单测（8 用例） |
| `pkg/transfer/transfer_test.go` | 新建：Orchestrator transfer 集成测试（3 用例） |
| `pkg/runtime/event/event.go` | 修改：`AgentTransferPayload` 增加 `FromAgent` / `ToAgent` / `Reason` |
| `pkg/runtime/outcome/outcome.go` | 修改：`RuntimeOutcome` 增加 `TransferChain` |
| `cmd/agentdemo/main.go` | 修改：transfer demo 改用 `pkg/transfer` 包 |
| `test-server/main.go` | 修改：新增 Transfer 模式，构建三 Agent Orchestrator |
| `test-server/static/index.html` | 修改：模式切换器、transfer 横幅、chain 可视化 |

---

## 设计要点

### 架构示意图

#### 组件关系

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Orchestrator                                │
│                     (implements Agent)                               │
│                                                                     │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐        │
│  │ AgentConfig   │     │ AgentConfig   │     │ AgentConfig   │       │
│  │ "triage"      │     │ "math_expert" │     │ "writer"      │       │
│  │ ┌───────────┐ │     │ ┌───────────┐ │     │ ┌───────────┐ │       │
│  │ │ ChatModel │ │     │ │ ChatModel │ │     │ │ ChatModel │ │       │
│  │ │ Tools     │ │     │ │ Tools     │ │     │ │ Tools     │ │       │
│  │ │ System    │ │     │ │ System    │ │     │ │ System    │ │       │
│  │ └───────────┘ │     │ └───────────┘ │     │ └───────────┘ │       │
│  └──────┬───────┘     └──────┬───────┘     └──────────────┘        │
│         │                    │                                       │
│  ┌──────┴────────────────────┴──────────────────────────────┐       │
│  │                    AgentRegistry                          │       │
│  │          Register / Get / TransferTargetsFor              │       │
│  └───────────────────────────────────────────────────────────┘       │
└─────────────────────────────────────────────────────────────────────┘
```

#### Transfer 完整生命周期

```
                          ┌─────────────┐
                          │   Client     │
                          │  (caller)    │
                          └──────┬──────┘
                                 │ req = RuntimeRequest{UserMessage: "计算 2+3"}
                                 ▼
                    ┌────────────────────────┐
                    │  Orchestrator.Run(req)  │
                    │  entryAgent = "triage"  │
                    └────────────┬───────────┘
                                 │
        ╔════════════════════════╧════════════════════════════╗
        ║  Transfer Loop (for { ... })                        ║
        ║                                                     ║
        ║  ┌─ iteration 1 ──────────────────────────────────┐ ║
        ║  │  currentAgent = "triage"                        │ ║
        ║  │                                                 │ ║
        ║  │  ┌───────────────────────────────────────────┐  │ ║
        ║  │  │  RunnerAgent("triage").runLoop()           │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  1. allTools = [user_tools]                │  │ ║
        ║  │  │             + [transfer_to_math_expert,    │  │ ║
        ║  │  │                transfer_to_writer]         │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  2. LLM.Stream(msgs, allTools)            │  │ ║
        ║  │  │     → toolCalls: [{transfer_to_math_expert,│  │ ║
        ║  │  │        args: {"reason": "需要数学计算"}}]   │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  3. isTransferTool("transfer_to_...") ✓   │  │ ║
        ║  │  │     → return transferSignal {              │  │ ║
        ║  │  │         TargetAgent: "math_expert"         │  │ ║
        ║  │  │         Reason: "需要数学计算"              │  │ ║
        ║  │  │         Msgs: [sys, user, assistant]       │  │ ║
        ║  │  │         Metrics: {tokens, steps}           │  │ ║
        ║  │  │       }                                    │  │ ║
        ║  │  └───────────────────────────────────────────┘  │ ║
        ║  │                                                 │ ║
        ║  │  ← emit EventAgentTransfer{                     │ ║
        ║  │       Phase: start,                             │ ║
        ║  │       From: "triage", To: "math_expert"         │ ║
        ║  │     }                                           │ ║
        ║  │  ← accumulated += sig.Metrics                   │ ║
        ║  │  ← transferChain = ["triage"]                   │ ║
        ║  │  ← inheritedMsgs = sig.Msgs                     │ ║
        ║  │  ← currentAgent = "math_expert"                 │ ║
        ║  └─────────────────────────────────────────────────┘ ║
        ║                                                     ║
        ║  ┌─ iteration 2 ──────────────────────────────────┐ ║
        ║  │  currentAgent = "math_expert"                   │ ║
        ║  │                                                 │ ║
        ║  │  ┌───────────────────────────────────────────┐  │ ║
        ║  │  │  RunnerAgent("math_expert").runLoop()      │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  1. msgs = replaceSystemMessage(           │  │ ║
        ║  │  │            inheritedMsgs,                  │  │ ║
        ║  │  │            "你是数学专家...")               │  │ ║
        ║  │  │     [new_sys, user, assistant_prev]        │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  2. LLM.Stream(msgs, math_tools)          │  │ ║
        ║  │  │     → toolCalls: [{calculate, "2+3"}]      │  │ ║
        ║  │  │     → tool.Invoke → "5"                    │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  3. LLM.Stream(msgs + tool_result)        │  │ ║
        ║  │  │     → "2 + 3 = 5"                         │  │ ║
        ║  │  │     → no tool calls → normal completion    │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  4. emit EventQueryEnd {                   │  │ ║
        ║  │  │       FinalText: "2 + 3 = 5"              │  │ ║
        ║  │  │       Termination: completed               │  │ ║
        ║  │  │       Metrics: accumulated + local         │  │ ║
        ║  │  │       TransferChain: ["triage","math_..."] │  │ ║
        ║  │  │     }                                      │  │ ║
        ║  │  │                                           │  │ ║
        ║  │  │  return nil (no transfer)                  │  │ ║
        ║  │  └───────────────────────────────────────────┘  │ ║
        ║  │                                                 │ ║
        ║  │  sig == nil → return (正常结束)                  │ ║
        ║  └─────────────────────────────────────────────────┘ ║
        ╚═══════════════════════════════════════════════════════╝
                                 │
                                 ▼
                    ┌────────────────────────┐
                    │  channel closed         │
                    │  Client 收到完整事件流   │
                    └────────────────────────┘
```

#### 事件流时序（Client 视角）

```
时间 ─────────────────────────────────────────────────────────────────▶

 triage Agent                          math_expert Agent
 ─────────────────────────             ─────────────────────────────
 ┊                                     ┊
 ├─ EventStart                         ┊
 ├─ EventQuestion{msg}                 ┊
 ├─ EventCallLLMStart{model}           ┊
 ├─ EventCallLLMEnd{tool_calls}        ┊
 ├─ EventToolCallStart{transfer_to_*}  ┊
 ┊                                     ┊
 ├─ EventAgentTransfer ──────────────▶ ┊  ← Phase: start
 ┊   {from: triage, to: math_expert}   ┊
 ┊                                     ┊
 ┊                                     ├─ EventCallLLMStart{model}
 ┊                                     ├─ EventCallLLMEnd{tool_calls}
 ┊                                     ├─ EventToolCallStart{calculate}
 ┊                                     ├─ EventToolCallEnd{result: "5"}
 ┊                                     ├─ EventCallLLMStart{model}
 ┊                                     ├─ EventAnswer{delta: "2+3=5"}
 ┊                                     ├─ EventCallLLMEnd{stop}
 ┊                                     ├─ EventQueryEnd{
 ┊                                     ┊    FinalText, Metrics,
 ┊                                     ┊    TransferChain: [triage, math_expert]
 ┊                                     ┊  }
 ┊                                     ┊
 ────── channel closed ────────────────────────────────────────────
```

#### 关键数据结构关系

```
AgentRegistry                    RunnerAgent
 ┌─────────────────┐              ┌────────────────────────┐
 │ agents: map      │◄─────────── │ Registry *AgentRegistry │
 │   "triage" → cfg │              │ Name    "triage"        │
 │   "expert" → cfg │              │ ToolInfos [...]         │
 │ order: [...]     │              └─────────┬──────────────┘
 └────────┬────────┘                        │
          │                                  │ runLoop() returns
          │ TransferTargetsFor("triage")     │
          │ → ["expert"]                     ▼
          │                        transferSignal
          │                         ┌──────────────────┐
          │                         │ TargetAgent: "expert"│
          ▼                         │ Reason: "..."     │
 buildTransferTools()               │ Msgs: []*Message  │
  → []*ToolInfo{                    │ Metrics: RunMetrics│
      Name: "transfer_to_expert"    └────────┬─────────┘
      Params: {reason: string}               │
    }                                        ▼
                                   Orchestrator.orchestrate()
                                    → 切换 currentAgent
                                    → 传递 inheritedMsgs
                                    → 累加 accumulated metrics
```

### Transfer 控制流（简版）

```
Orchestrator.Run(req)
  ├── 构造入口 RunnerAgent（从 Registry 取 AgentConfig）
  ├── agent.runLoop(ctx, req, ch)
  │     ├── LLM 推理
  │     ├── 识别 transfer_to_math_expert tool call
  │     ├── 返回 transferSignal{TargetAgent: "math_expert", Reason: "...", Msgs: [...]}
  │     └── return（退出当前 loop）
  ├── Orchestrator 收到 signal → 发送 EventAgentTransfer{Phase: start}
  ├── 构造目标 RunnerAgent（替换 system、工具、配置）
  ├── 目标 agent.runLoop(ctx, req, ch)  ← 使用同一 channel
  │     ├── LLM 推理（带完整对话历史 + 新 system）
  │     ├── 正常工具调用 / 生成回答
  │     └── 发送 EventQueryEnd
  └── return（循环结束）
```

### Transfer vs Spawn 语义区分

| 维度 | Transfer（v0.3.0） | Spawn（后续） |
|------|-------------------|-------------|
| 控制流 | 平级移交，A 退出 B 接管 | 父子递归，子 Run 结束结果回注父 |
| 对话历史 | B 继承 A 的完整历史 | 子 Run 独立上下文，仅携带 task |
| 事件 | `agent_transfer` | `spawn_start` / `spawn_end` |
| Run ID | 同一 RunID | 子 Run 有独立 RunID |
| 典型场景 | 分诊 → 专家；客服 → 技术支持 | 拆分子任务并行处理 |

---

## 未包含（后续版本）

- **Spawn（父子递归子任务）**：v0.4.0
- **静态 Network（parallel / sequential / competitive）**：v0.4.0+
- **TransferContextSummary 模式**：需额外 LLM 调用做摘要
- **MCP client**：独立里程碑
- **Skills 注入**：独立里程碑
- **Tool whitelist / blacklist**：per-agent 工具权限收敛

---

## 路线图 TODO

### P0 — 核心能力（下一里程碑）

- [ ] **Spawn（动态子 Agent）**：父子 Run 递归调用，`SpawnSpec` → `SpawnResult` 全流程，`SpawnStart` / `SpawnEnd` 事件，MaxDepth 控制
- [ ] **Tool whitelist / blacklist**：per-agent / per-spawn 的工具权限收敛，与 `ToolRegistry` 集成
- [ ] **Skill registry**：`SKILL.md` 指令注入 + MCP 能力发现统一注册，支持动态启停
- [ ] **MCP client**：`internal/mcp` 实现 stdio / streamable-http transport，`tools/list` 发现 → `ToolRegistry` 自动注册

### P1 — 增强与可观测

- [ ] **Memory（short / long）**：短期 `[]Message` 滑动窗口 + 摘要；长期通过 MCP 持久化
- [ ] **Reflection / retry**：工具调用失败后模型自反思并重试
- [ ] **Cost tracing**：usage 采集 + Run 树聚合 + `TotalCostUSD`
- [ ] **Span 可视化**：OTel span 埋点，输出到 Jaeger / Dev UI

---

## 提交记录（开发日志）

| 日期 | 说明 |
|------|------|
| 2026-04-15 | 版本规划；进度日志初始化。 |
| 2026-04-16 | `AgentRegistry` + `AgentConfig`（注册/查找/校验）；`buildTransferTools` 自动生成 `transfer_to_{name}` 工具；`AgentTransferPayload` 扩展 `FromAgent`/`ToAgent`/`Reason`；`RuntimeOutcome.TransferChain`；`RunnerAgent.runLoop` 重构支持 `transferSignal` + `loopState`（累积 metrics/chain、SuppressBookends）；`Orchestrator` 实现 `Agent` 接口（transfer 循环 + `MaxTransfers` 防环）；`WithRegistry`/`WithMaxTransfers` options；三 Agent transfer demo（triage/math_expert/writer）；13 个测试全部通过（registry 8 + transfer 4 + 原有 1）。 |
| 2026-04-16 | **代码审核通过**。详见下方审核记录。 |
| 2026-04-16 | **包重组**：transfer 代码从 `pkg/agent/` 迁移到 `pkg/transfer/`；`runner_agent.go`（403 行）拆分为 5 个文件；RunnerAgent 通过 `ToolInterceptor` + `ExtraTools` 与 transfer 解耦；13 个测试全部通过。 |
| 2026-04-16 | **test-server Transfer 模式**：后端新增 `buildTransferAgent()`（triage / math_expert / writer），前端增加 Single/Transfer 模式切换、transfer 横幅可视化、TransferChain 链路展示。 |
| 2026-04-16 | **Bug fix**：transfer 后对话历史缺少 tool result 导致目标 Agent 收到悬挂 tool call、LLM 返回空响应。Orchestrator 追加合成 tool result 闭合 tool call 循环，修复后端到端 transfer 流程正常运行。 |

---

## 代码审核记录（2026-04-16）

### 审核范围

| 文件 | 行数 | 类型 |
|------|------|------|
| `pkg/agent/transfer_tool.go` | 62 | 新建 |
| `pkg/agent/runner_agent.go` | 403 | 修改 |
| `pkg/agent/orchestrator.go` | 150 | 新建 |
| `pkg/agent/orchestrator_option.go` | 15 | 新建 |
| `pkg/agent/registry.go` | 119 | 新建 |
| `pkg/agent/runner_options.go` | 78 | 修改 |
| `pkg/runtime/event/event.go` | 172 | 修改 |
| `pkg/runtime/outcome/outcome.go` | 32 | 修改 |
| `pkg/agent/transfer_test.go` | 239 | 新建 |
| `pkg/agent/registry_test.go` | 119 | 新建 |

### 审核结论：通过

### 设计亮点

1. **幻影工具（Phantom Tool）模式**：`transfer_to_{name}` 工具只有 Schema 没有 Handle，在 `runLoop` 的工具循环中被 `isTransferTool()` 提前拦截为 `transferSignal` 返回值，不走 `tool.Invoke` 执行路径。控制流信号与业务工具完全解耦。

2. **接口透明性**：`Orchestrator` 实现 `Agent` 接口（`var _ Agent = (*Orchestrator)(nil)`），多 Agent 协作对调用方完全透明。无论底层经过了几次 transfer，调用方始终只看到一个 `<-chan *RuntimeEvent`。

3. **Metrics 分层累加无重复**：`Orchestrator` 在每次 transfer 后将 `sig.Metrics`（仅含当前 Agent 本地消耗）累加到 `accumulated`；最终 Agent 的 `buildMetrics()` 将自身本地 metrics 与 `state.AccumulatedMetrics` 合并输出。两层各自只算自己的部分，无双重计算。

4. **对话历史传递语义正确**：`replaceSystemMessage()` 只替换第一条 system 消息为目标 Agent 的 `SystemInstructions`，保留完整的 user/assistant/tool 对话历史，目标 Agent 能看到之前的完整交互上下文。

5. **事件流融合**：`loopState.SuppressBookends` 让后续 Agent 跳过 `EventStart` / `EventQuestion` 的重复发送，Client 看到的事件流是一条连续的时间线。

6. **防环机制**：`MaxTransfers` + `transferCount` 计数器，测试中覆盖了 ping-pong 无限循环场景（`TestOrchestrator_MaxTransfers_Exceeded`），确认 3 次 transfer 后触发 `max_transfers` 错误终止。

7. **细粒度移交控制**：`AgentConfig.TransferTargets` 允许每个 Agent 声明允许移交的目标列表；为空时默认可移交到注册表中所有其他 Agent。`Validate()` 方法在注册后检查所有 target 引用是否有效。

### 注意事项（非阻塞，建议后续处理）

1. **`TransferEnd` 已定义未使用**：`event.go` 中定义了 `TransferEnd TransferPhase = "end"`，但 Orchestrator 仅发送 `TransferStart`。`AgentTransferPayload` 中的 `ChildRunID`、`Depth`、`OK` 字段也未在 transfer 路径中赋值（这些字段更适合 Spawn 场景）。建议在后续版本中明确：要么在目标 Agent 完成后补发 `TransferEnd`，要么将这些字段标记为 Spawn 专用。

2. **混合工具调用的静默丢弃**：如果模型在同一轮返回了 transfer 工具和普通工具（`toolCalls` 包含 `transfer_to_X` + `calculate`），当前逻辑在遍历到 transfer 工具后直接返回 `transferSignal`，后续的普通工具调用被静默跳过。行为正确（transfer 优先级最高），但建议添加注释说明或在事件流中记录被跳过的工具调用。

3. **`tool.ValidateBindings` 的校验范围**：`runLoop` 第 125 行对 `a.ToolInfos`（原始工具）做校验，而实际传给模型的是 `allTools`（含 transfer 工具）。这是正确的——transfer 工具无 Handle 不应参与校验——但校验放在 `allTools` 构建之后读起来有些跳跃，可考虑加注释或调整顺序。

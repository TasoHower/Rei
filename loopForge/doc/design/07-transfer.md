# loopForge Transfer（Agent Handoff）

> 本文档描述 loopForge 中 Agent 之间的 **transfer（handoff）机制**。Transfer 允许一个 Agent 在 LLM 推理过程中将任务转交给另一个更专业的 Agent，实现多 Agent 协作。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md)——Agent 的 `RunLoop` 与 `ToolInterceptor` 机制；[02-runner-core.md](02-runner-core.md) §3.3——Runner 的 `runTransferLoop` 主循环。

---

## 1. 概念

### 1.1 什么是 Transfer

Transfer 是 Agent 之间的**handoff 任务交接**。与 spawn（创建子 Agent 并行执行）不同，transfer 是**串行**的：Agent A 将整个对话上下文移交给 Agent B，Agent B 接管后续所有推理。

```
请求 → [Agent A] → 判断"我需要数学专家" → transfer → [Agent B] → 最终回答
```

### 1.2 与 Spawn 的区别

| 维度 | Transfer | Spawn |
|------|---------|-------|
| 执行方式 | **串行**——Agent A 完成后 Agent B 接管 | **并行/异步**——子 Agent 在后台 goroutine 中执行 |
| 对话上下文 | Agent B 继承 Agent A 的**完整消息历史** | 子 Agent 默认**完全隔离**（仅可选 MemoryDigest） |
| 触发方式 | LLM 调用 `transfer_to_xxx` 工具 | LLM 调用 `spawn_subagent` 工具 |
| 结果处理 | Agent B 直接输出最终回答给用户 | 子结果回注到父对话，父 LLM 再总结 |
| 事件标记 | `agent_transfer(phase=start)` | `spawn_start / spawn_end` |

---

## 2. 核心组件

### 2.1 AddHandoff：注册可转移目标

`pkg/agent/agent.go`：

```go
func (a *Agent) AddHandoff(targets ...*Agent)   // 注册 transfer 目标列表
func (a *Agent) Handoffs() []*Agent              // 读取已注册的目标
```

调用方通过 `AddHandoff` 声明当前 Agent 可以将任务转交给哪些 Agent：

```go
triage := agent.New(chat,
    agent.WithName("triage"),
    agent.WithSystemInstructions("Route to the appropriate specialist."),
)

mathExpert := agent.New(chat,
    agent.WithName("math_expert"),
    agent.WithDescription("Performs arithmetic via tools."),
)

writer := agent.New(chat,
    agent.WithName("writer"),
    agent.WithDescription("Creates creative text and stories."),
)

triage.AddHandoff(mathExpert, writer)  // triage 可以转交给这两个专家
```

### 2.2 BuildTransferTools：生成 transfer_to_* 工具

`pkg/agent/transfer.go`：

```go
const TransferToolPrefix = "transfer_to_"

func BuildTransferTools(current *Agent) []*model.ToolInfo
```

Runner 在每次 Agent hop 前调用此函数，根据当前 Agent 的 `Handoffs()` 列表自动生成转移工具：

```
Handoffs = [math_expert, writer]
        ↓
BuildTransferTools 生成：
  - ToolInfo{Name: "transfer_to_math_expert", Description: "...", Parameters: {reason: string}}
  - ToolInfo{Name: "transfer_to_writer",     Description: "...", Parameters: {reason: string}}
```

这些工具**不带 Handle**（`Handle = nil`），不参与 `tool.Invoke` 的常规分派。它们通过 `WithExtraTools` 注入，不参与 `ValidateBindings`。

工具参数仅有一个 `reason` 字段，让 LLM 解释为何需要转移：

```json
{"reason": "This problem requires step-by-step arithmetic calculation"}
```

### 2.3 ToolInterceptor：拦截转移调用

Runner 在构造 Agent 时同时设置 `ToolInterceptor`：

```go
a.ToolInterceptor = agent.IsTransferTool  // return strings.HasPrefix(tc.Name, "transfer_to_")
```

在 `RunLoop` 的迭代中，当 LLM 返回 `tool_calls` 后，`runLoopInterceptIfNeeded` 在**正式执行工具之前**检查拦截器：

```go
// loop.go L244-L247
if ic := a.runLoopInterceptIfNeeded(msgs, sr, vstore, modelName, ...); ic != nil {
    pendingQueryEnd = nil     // 暂停本 Agent 的 query_end
    return ic                 // 返回 *InterceptedCall 给 Runner
}
```

若 `ToolInterceptor` 匹配了某个 `transfer_to_*` 调用，`RunLoop` 立即返回 `*InterceptedCall`，**不会向下走到 `executeToolCalls`**。

### 2.4 InterceptedCall：传递转移信号

`pkg/agent/agent.go`：

```go
type InterceptedCall struct {
	ToolCall    model.ToolCallPart          // 被拦截的 transfer_to_* 调用
	Msgs        []*model.Message            // Agent A 的完整对话历史
	Metrics     outcome.RunMetrics          // Agent A 的指标
	VarSnapshot *variable.StoreSnapshot     // Agent A 的变量快照
}
```

这是 Agent A 和 Runner 之间的**内部契约**。Agent A 通过返回 `*InterceptedCall` 告诉 Runner："我需要移交，这是目标、上下文和当前状态。"

### 2.5 BuildTransferPrompt：注入转移指令

Runner 在每次 Agent hop 前同时调用此函数，将转移协议追加到 system prompt：

```go
if p := agent.BuildTransferPrompt(current); p != "" {
    a.SystemInstructions += p
}
```

注入的提示包含：

- **When to Transfer**：5 种适合转移的场景（领域专长、能力缺失、并行化、质量提升、不确定性）
- **When NOT to Transfer**：3 种不应转移的场景（能自行完成、增加延迟、简单对话）
- **Transfer Rules**：选择最佳 Agent、提供清晰原因、保留约束、不暴露路由逻辑
- **After Transfer**：整合结果、验证输出、可迭代转移

---

## 3. runTransferLoop：Runner 的转移主循环

`pkg/runner/runner.go`：

```go
func (r *Runner) runTransferLoop(ctx, req, ch, runState, policy)
```

`Runner.Run()` 在检测到入口 Agent 有 `Handoffs()` 时，走此路径而非单 Agent 模式。

### 3.1 循环流程

```
runTransferLoop：
  │
  ├── 初始化：
  │     current = entryAgent
  │     inheritedMsgs = nil（首轮无继承）
  │     accumulated = 空指标
  │     transferChain = []
  │     transferCount = 0
  │     firstAgent = true
  │
  └── 主循环（for {}）：
        │
        ├── 1. a = current.Clone() + applyDefaultLarkIfNeeded + prepareAgent
        │
        ├── 2. 装配 transfer 工具链：
        │       a.ExtraTools = BuildTransferTools(current)
        │       a.ToolInterceptor = IsTransferTool
        │       a.SystemInstructions += BuildTransferPrompt(current)
        │
        ├── 3. 构造 LoopState：
        │       AccumulatedMetrics = accumulated     (前几个 Agent 的累计指标)
        │       TransferChain = transferChain        (已走过的 Agent 名)
        │       SuppressBookends = !firstAgent       (非首个 Agent 跳过 start/question)
        │       VarStore = runStore                  (共享变量)
        │       CurrentRunRef = {RunID, Depth=0}
        │
        ├── 4. 注入 engineSpawner（预算 + ChildRegistry）
        │
        ├── 5. result := a.RunLoop(ctx, req, ch, inheritedMsgs, st)
        │       └── 返回 *InterceptedCall 或 nil
        │
        ├── 6. runState.Terminate(completed)         // 清理本 Agent 的子树
        │
        ├── 7. 若 result == nil → 结束循环（return，Agent B 正常完成了）
        │
        ├── 8. transferCount++ → 检查是否超 maxTransfers
        │
        ├── 9. 累积指标：accumulated += result.Metrics
        │
        ├── 10. 解析目标 Agent：
        │        targetName = TargetAgent(result.ToolCall.Name)  // 从 "transfer_to_X" 提取 "X"
        │        next = findHandoff(current, targetName)         // 从 Handoffs 列表查找
        │        └── 找不到 → emitError("invalid_transfer")
        │
        ├── 11. emit(agent_transfer, phase=start, from=A, to=B, reason)
        │
        ├── 12. 构造继承消息：
        │        inheritedMsgs = appendSyntheticToolResponsesAfterTransfer(...)
        │        (为被拦截的 tool_calls 生成占位 tool_result，满足 API 格式要求)
        │
        ├── 13. transferChain 追加 current.Name
        │
        └── 14. current = next → firstAgent = false → 回到步骤 1
```

### 3.2 appendSyntheticToolResponsesAfterTransfer

Transfer 时 `RunLoop` 在工具执行前被拦截，所以**没有真实的 tool_result 消息**。但 OpenAI 兼容 API 要求：若 assistant 消息含 `tool_calls`，后续必须有等量的 `role=tool` 消息。

Runner 通过此函数补丁这个缺口：

```
LLM 输出: tool_calls = [{id: "c1", name: "transfer_to_math_expert"}]
  → 拦截：不执行 tool.Invoke
  → 生成: role=tool message {tool_call_id: "c1", content: "Transfer accepted. Handled by math_expert."}
  → Agent B 收到完整的、API 合法的消息历史
```

对于**并行 tool_calls**（一条 assistant 消息含多个 tool_call），被转移的那个生成 handoff 消息，其余的生成占位 `{"cancelled":true,"reason":"superseded by transfer"}`。

### 3.3 防循环保护

```go
transferCount++
if transferCount > r.maxTransfers {  // 默认 10
    emitError("max_transfers", "exceeded maximum transfer count")
    return
}
```

每完成一次 handoff 递增计数器，超过上限立即终止，防止 Agent 之间的无限循环转移。

---

## 4. 事件流

Transfer 模式下的事件流：

```
[Agent A] start → question
    → call_llm_start → call_llm_end(tool_calls)
    → tool_call_start(name=transfer_to_B)
    → agent_transfer(phase=start, from=A, to=B, reason)
    → tool_call_end

[Agent B] call_llm_start → answer(Δ) × N → call_llm_end(stop)
    → query_end(completed, TransferChain=[A, B])
```

关键差异：

| 差异 | 说明 |
|------|------|
| `start`/`question` 仅一次 | 第二个 Agent 起 `SuppressBookends=true`，跳过书签事件 |
| `agent_transfer` 插入 | 每次转移时 Runner emit 一条 transfer 事件 |
| `TransferChain` 累积 | 最终 Outcome 中列出所有经过的 Agent 名 |
| `Metrics` 累积 | 最终指标 = 所有 Agent hop 的 token/步数之和 |

---

## 5. 完整端到端示例

### 5.1 代码

```go
// 构造 triage Agent（入口，可以 transfer）
triage := agent.New(chat,
    agent.WithName("triage"),
    agent.WithSystemInstructions("Route to the appropriate specialist. "+
        "If the user asks about math, transfer to math_expert. "+
        "If about writing, transfer to writer."),
)

// 构造 math_expert（转移目标 #1）
mathExpert := agent.New(chat,
    agent.WithName("math_expert"),
    agent.WithDescription("Performs arithmetic using built-in tools."),
    agent.WithSystemInstructions("You are the arithmetic specialist. "+
        "Call tools for every numeric step; never compute mentally."),
    agent.WithToolInfos(mathTools),
)

// 构造 writer（转移目标 #2）
writer := agent.New(chat,
    agent.WithName("writer"),
    agent.WithDescription("Creates creative text and stories."),
    agent.WithSystemInstructions("You are a creative writer. "+
        "Write engaging, well-structured content."),
)

// 注册 transfer 目标
triage.AddHandoff(mathExpert, writer)

// 启动 Runner
r := runner.NewRunner(triage,
    runner.WithMaxTransfers(10),
    runner.WithVarStore(variable.New()),
)

ch := r.Run(ctx, &request.RuntimeRequest{
    SessionID:   "sess-1",
    UserMessage: "Calculate 7 * 8 + 3",
})
```

### 5.2 执行过程

```
1. 用户: "Calculate 7 * 8 + 3"

2. Runner.Run() → entryAgent.Handoffs() = [math_expert, writer] → 进入 transfer 模式

3. [triage Agent]
    system prompt 含: "Route to the appropriate specialist..."
    ExtraTools 含: [transfer_to_math_expert, transfer_to_writer]
    ToolInterceptor = IsTransferTool
    LLM 推理 → 判断需要数学能力 → tool_calls: [{name: "transfer_to_math_expert",
        arguments: {"reason":"This problem requires step-by-step arithmetic"}}]

4. RunLoop 拦截：ToolInterceptor 返回 true → return *InterceptedCall{...}

5. Runner:
    TargetAgent("transfer_to_math_expert") → "math_expert"
    findHandoff(triage, "math_expert") → mathExpert
    emit(agent_transfer, phase=start, from=triage, to=math_expert)
    inheritedMsgs = triage 消息历史 + 人工 tool_result

6. [math_expert Agent]
    SuppressBookends = true（跳过 start/question）
    继承 triage 的完整对话上下文
    LLM 收到 system prompt: "You are the arithmetic specialist..."
    调工具: call_llm_start → tool_call(add, a=7, b=8) → 56
            → tool_call(multiply, a=56, b=3) → 59
    LLM 输出: "The result is 59."

7. RunLoop 返回 nil（正常结束）

8. Runner 收到 nil → 退出 transfer 循环
   最终 emit(query_end, Metrics=累计, TransferChain=[triage, math_expert])
```

---

## 6. 约束与边界

| 约束 | 说明 |
|------|------|
| **串行执行** | 一次只能转移到一个 Agent，不能同时 transfer 到多个 Agent |
| **消息历史继承** | Agent B 看到 Agent A 的完整对话，不隔离 |
| **变量共享** | 通过 `Runner.WithVarStore` 共享，Agent A 写的变量 Agent B 可见 |
| **线程安全** | Runner 为每个 Agent hop 调用 `Clone()`，避免并发写入同一 Agent 实例 |
| **双向 transfer** | Agent B 也可以 `AddHandoff(triage)` 转回去（但受 maxTransfers 限制） |
| **与 spawn 互斥** | 有活跃子 Agent 时禁止 transfer（v0.8.0 决议，当前代码以 system prompt 约束形式实现） |

---

## 7. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/agent/transfer.go` | BuildTransferTools、BuildTransferPrompt、IsTransferTool、TargetAgent、ExtractReason |
| `pkg/agent/agent.go` | AddHandoff、Handoffs、ToolInterceptor、InterceptedCall |
| `pkg/agent/loop.go` | runLoopInterceptIfNeeded（拦截点） |
| `pkg/runner/runner.go` | runTransferLoop、appendSyntheticToolResponsesAfterTransfer、findHandoff |

---

## 8. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) §5 | RunLoop 中的 ToolInterceptor 拦截点 |
| [02-runner-core.md](02-runner-core.md) §3.3 | runTransferLoop 完整伪代码流程 |
| [03-events.md](03-events.md) §2.8 | agent_transfer 事件的载荷与语义 |
| [04-tools.md](04-tools.md) §2.3 | transfer_to_* 作为注入工具（ExtraTools） |

---

## 9. Multi-Agent vs Single-Agent with Skills：架构取舍

Transfer（handoff）不是实现多能力的唯一路径。loopForge 的另一条路径是 **Skill**：将领域知识以 `SKILL.md` 文本注入到单个 Agent 的 system prompt 中，让 LLM 遵循指令行事。两条路径各有适用场景。

### 9.1 机制对比

| 维度 | Transfer（Multi-Agent） | Skill（Single-Agent） |
|------|------------------------|----------------------|
| **注入方式** | 切换到独立 Agent 实例 | 文本拼入 system prompt |
| **系统提示词** | 每个 Agent 有独立的 `SystemInstructions`（互不干扰） | 所有 Skill 指令叠加在同一段 system prompt 中 |
| **工具集** | 每个 Agent 可绑定**不同工具**（如数学 Agent 只有算术工具） | 所有 Skill 共享同一套工具注册表 |
| **模型** | 不同 Agent 可以选**不同模型**（贵的做复杂推理，便宜的做简单回答） | 一个模型处理所有任务 |
| **上下文窗口** | 每个 Agent 有**独立上下文**（Transfer 时移交消息历史，但各自的 system prompt 不叠加） | 所有 Skill 指令+对话全在同一上下文中，**窗口压力大** |
| **隔离性** | Agent 互不可见对方的内部逻辑（tool_calls、中间结果等） | LLM 能看到所有 Skill 指令，可能"串用" |

### 9.2 定性比较

| 场景 | 推荐 | 原因 |
|------|------|------|
| **领域划分明确**（如：路由员 vs 数学专家 vs 写作者） | Transfer | 每个 Agent 的 system prompt 简短专一，LLM 不易混淆角色 |
| **工具集不同**（数学 Agent 只需要 `add/subtract/multiply/divide`，文件 Agent 需要 `read_file/write_file`） | Transfer | 每个 Agent 只看到自己需要的工具——LLM 的工具选择更准确，token 消耗更低 |
| **成本敏感**（简单问题不值得用大模型） | Transfer | 路由 Agent 用小模型，专家 Agent 用大模型，按需付费 |
| **上下文窗口紧张** | Transfer | 每个 Agent 的 system prompt 短，技能指令不叠加 |
| **逻辑简单、工具共享**（如通知、标记、简单格式化） | Skill | 不需要独立 Agent 的开销 |
| **需要细粒度指令控制** | Skill | `SKILL.md` 可以写详细的 checklist、格式要求、step-by-step 步骤，LLM 照做 |
| **动态加载知识**（用户说"用客服话术回复"） | Skill | 通过 `load_skill` 工具动态加载，不需要预注册 Agent |

### 9.3 混合使用

Transfer 和 Skill **不互斥**。同一个 Agent 可以同时带着 Skill 指令并参与 handoff：

```
triage Agent (带 skill: "routing-guide")
  ├── transfer_to_math_expert  (math Expert 带 skill: "calculation-checklist")
  └── transfer_to_writer       (writer 带 skill: "professional-tone")
```

这样的混合架构中，Skill 负责"怎么做"（执行细节），Transfer 负责"谁来做"（角色分工）。

### 9.4 决策原则

- **Agent 之间差异大（system prompt、工具集、模型不同）→ Transfer**
- **Agent 之间差异小（共享工具、共享上下文、只需指令差异）→ Skill**
- **举棋不定时先试 Skill**——Skill 的试错成本低（改一个 md 文件 vs 新增一个 Agent 实例+handoff 配置）

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：Transfer 概念、核心组件（AddHandoff/BuildTransferTools/ToolInterceptor/InterceptedCall/BuildTransferPrompt）、Runner transfer 主循环、事件流、端到端示例。 |
| 2026-04-30 | v0.9.5 | 新增 §9：Transfer vs Skill 机制对比与架构取舍讨论。 |

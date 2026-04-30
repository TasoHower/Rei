# loopForge Agent 核心

> 本文档仅描述 `pkg/agent` 中 Agent 的**自身逻辑与启动方案**。工具注册、MCP 集成、transfer 手递手、spawn 子 Agent 等能力不在此说明，见对应设计文档与 ADR。

---

## 1. Agent 是什么

Agent 是 loopForge 中最基本的执行单元。它封装一个 LLM 模型实例（`ChatModel`）及相关运行参数，在 `RunLoop` 中反复执行「拼消息 → 调 LLM → 处理返回」的迭代。每次迭代称为一步（step），每一步的输入是累积的消息列表（`[]*model.Message`），输出是 LLM 的回复（可能含工具调用）。

```
┌──────────┐    ┌────────────────────────────────────┐
│ 用户请求  │───→│  Agent.RunLoop                    │
│ Runtime  │    │                                    │
│ Request   │    │  for step := 0; step < maxSteps   │
└──────────┘    │      messages → LLM → response     │
                │      → 追加到消息列表 → 下一步      │
                │                                    │
                │  → emit(query_end) 结束             │
                └──────────┬─────────────────────────┘
                           │ events (RuntimeEvent)
                           v
                    ┌──────────────┐
                    │  事件 channel │
                    │  (SSE/前端)   │
                    └──────────────┘
```

---

## 2. Agent 结构体

`pkg/agent/agent.go`：

```go
type Agent struct {
	// === 标识 ===
	Name               string      // Agent 标识名（日志/观测用）
	Description        string      // 对 LLM 可见的自我描述

	// === 模型 ===
	ModelName          string      // 模型名（如 "deepseek-chat"），供 Runner 选择适配器
	ChatModel          model.ToolCallingChatModel  // LLM 调用接口
	CallOptions        []model.CallOption          // LLM 调用参数（温度、top_p 等）
	MaxSteps           int         // 一次 Run 的最大步数

	// === 提示词 ===
	SystemInstructions string              // 系统提示词
	SystemPromptBuilder SystemPromptBuilder // 自定义 system prompt 组装管线（可选）

	// === 能力型字段（以下字段在本文档中不展开） ===
	ToolInfos          []*model.ToolInfo     // 工具注册表
	Executor           tool.ToolExecutor     // 工具执行器兜底
	MCPServerProfiles  []cfg.MCPServerProfile // MCP 服务器配置
	ExtraTools         []*model.ToolInfo     // 注入工具（如 transfer_to_*）
	ToolInterceptor    ToolInterceptor       // 工具拦截器
	Variable           bool                  // var_set + [Variables]
	SkillRegistry      *skill.SkillRegistry  // 技能注册表
	SkillNames         []string              // 技能包名列表
	SkillShellTool     bool                  // 技能 shell 执行
	LoadSkillTool      bool                  // 动态加载技能
	SpawnEnabled       bool                  // 是否允许 spawn 子 Agent
	ChildAgentBuilder  func(*exchange.SpawnSpec) *Agent  // spawn 子 Agent 构建器
	Spawner            Spawner               // Spawner 接口
	handoffs           []*Agent              // transfer 目标
}
```

**核心字段**：`Name` / `ChatModel` / `MaxSteps` / `SystemInstructions` / `CallOptions`。其余字段对应各扩展能力，Agent 本身不强制依赖它们。

---

## 3. 构造与选项

`pkg/agent/options.go`：

```go
// New 构造 Agent。chat 是必需的模型实例；opts 按顺序应用。
func New(chat model.ToolCallingChatModel, opts ...Option) *Agent
```

**核心选项**（仅列出 Agent 自身运行时相关）：

| Option | 作用 |
|--------|------|
| `WithName(s)` | 设置 Agent 标识名 |
| `WithModelName(s)` | 设置模型名（供模型适配层选择） |
| `WithSystemInstructions(s)` | 设置系统提示词 |
| `WithMaxSteps(n)` | 设置一次 Run 的最大步数 |
| `WithCallOptions(opts...)` | 设置 LLM 调用参数 |

**完整选项（本文档不展开）**：`WithToolInfos`、`WithMCPServerProfiles`、`WithExtraTools`、`WithToolInterceptor`、`WithVariable`、`WithSpawn`、`WithSkills`、`WithSkillShellTool`、`WithLoadSkillTool`。

### 构造示例

```go
chat := deepseekadapter.NewDeepSeekChatModel(apiKey, baseURL, modelName)

ag := agent.New(chat,
	agent.WithName("assistant"),
	agent.WithSystemInstructions("You are a helpful assistant."),
	agent.WithMaxSteps(32),
	agent.WithCallOptions(model.WithTemperature(0.7)),
)
```

---

## 4. Runnable 接口与启动

`pkg/agent/interface.go`：

```go
type Runnable interface {
	Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent
}
```

`Agent` 实现了 `Runnable` 接口：

```go
func (a *Agent) Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent
```

`Run()` 的内部构造：

```
Agent.Run(ctx, req)
  │
  ├── 创建事件 channel ch（缓冲 256）
  │
  ├── 启动 goroutine：
  │     ├── 初始化 LoopState（若 req 传入 RunModeInherit 则复用上下文）
  │     ├── 调用 RunLoop(ctx, req, ch, nil, state)
  │     └── 关闭 ch（RunLoop 返回后）
  │
  └── 返回 ch（调用方从此 channel 消费事件）
```

调用方从 channel 读取事件，直到 channel 关闭。正常结束时最后一个事件是 `EventQueryEnd`（含 `RuntimeOutcome`）；出错时可能是 `EventError`。

### 典型对接

```go
ch := ag.Run(ctx, &request.RuntimeRequest{
	SessionID:   "sess-1",
	UserMessage: "Hello",
})

for ev := range ch {
	switch ev.Type {
	case event.EventQueryEnd:
		_ = ev.QueryEnd().Outcome.FinalText
	case event.EventError:
		_ = ev.Error().Message
	}
}
```

---

## 5. RunLoop 核心逻辑

`pkg/agent/loop.go`：

```go
func (a *Agent) RunLoop(
	ctx context.Context,
	req *request.RuntimeRequest,
	ch chan<- *event.RuntimeEvent,
	inheritedMsgs []*model.Message,   // 继承消息（transfer 时传入）
	state *LoopState,                  // 跨 Agent 状态累加器
) *InterceptedCall
```

### 5.1 启动阶段（RunLoop 前部）

```
RunLoop 进入后，按顺序执行：
  1. 校验 req 和 ChatModel 不为 nil
  2. 确定 VarStore（从 state 继承或新建）
  3. 解析技能和系统提示词基座
  4. 确定消息列表：
     - 首轮（inheritedMsgs==nil）：从 req.UserMessage 构建消息
     - 继承轮（inheritedMsgs!=nil）：使用传入的历史消息
  5. 确定 maxSteps / modelName / callOptions
  6. 若未抑制书签：emit(start) → emit(question)
```

### 5.2 迭代阶段

```
for step := 0; step < maxSteps; step++:
  │
  ├── 1. 检查 ctx.Done()，已取消则直接返回 nil
  │
  ├── 2. 构建本轮 system prompt：
  │       └── a.runLoopFullSystem() → 合并基座 system + [Variables] + Skills + 自定义 builder
  │
  ├── 3. emit(call_llm_start)：
  │       └── 含 modelName、temperature、systemPrompt、tools（ToolInfo 摘要）
  │
  ├── 4. consumeStream() → 调用 LLM：
  │       ├── 流式读取：emit(answer) 逐 delta
  │       └── 返回 streamResult{sr.Text, sr.ToolCalls, sr.InputTokens, sr.OutputTokens}
  │
  ├── 5. emit(call_llm_end)：
  │       └── 含 finishReason（stop | tool_calls）、token 统计
  │
  ├── 6. LLM 回复追加到消息列表
  │
  ├── 7. 判断下一步：
  │       ├── 无 tool_calls → 结束：
  │       │     check async spawns → emit(query_end) → return nil
  │       │
  │       ├── 无 tool_calls 但有 async spawn 结果 → 注入为 user message → continue
  │       │
  │       ├── ToolInterceptor 命中 → 返回 *InterceptedCall（transfer 入口）
  │       │
  │       └── 有 tool_calls → executeToolCalls：
  │             └── 对每个 tool_call：emit(tool_call_start) → tool.Invoke
  │                 → emit(tool_call_end) → result 追加到消息列表
  │             └── continue（进入下一步）
  │
  └── maxSteps 耗尽 → emit(query_end, TerminationMaxSteps) → return nil
```

### 5.3 结束阶段

```
无论是正常结束（无 tool_calls）还是 maxSteps 耗尽：
  ├── emit(query_end, RuntimeOutcome)
  ├── Outcome 含：FinalText、TerminationReason、Metrics、TransferChain
  └── channel 关闭（RunLoop 返回后由 Agent.Run 的 goroutine close(ch)）
```

出错时 emit 流程为：

```
emit(call_llm_end, FinishReason=error) 或直接
emit(error, {Code, Message}) → emit(query_end, Termination=error) → return nil
```

---

## 6. LoopState（跨 Agent 上下文）

`pkg/agent/interface.go`：

```go
type LoopState struct {
	AccumulatedMetrics outcome.RunMetrics   // 多 Agent 累积指标
	TransferChain      []string             // transfer 经过的 Agent 名链
	SuppressBookends   bool                 // 是否跳过 start/question 书签事件
	VarStore           *variable.VarStore   // 共享变量存储
	ExtraSkills        []skill.SkillSpec    // 本轮动态加载的技能
	CurrentRunRef      *exchange.RunRef     // 当前运行引用（spawn 深度追踪）
	// asyncWG / asyncSpawnResults / asyncSpawnMu 用于 spawn 异步场景
}
```

`LoopState` 在以下场景使用：

- **单 Agent**：传入 nil，RunLoop 内部自动维护
- **Transfer 编排**：由 Runner 创建，跨 Agent 传递，携带累积指标、变量、transfer 链
- **RunLoop 内部**：读取 `CurrentRunRef.Depth` 控制 spawn 深度，`SuppressBookends` 控制书签事件

`LoopState` 不参与 Agent 的核心 loop 决策（是否调用 LLM、是否继续步进），仅承载编排层需要聚合的数据。

---

## 7. 事件流

Agent 每步产生以下事件序列（单 Agent 无错误）：

```
step 0:  start → question
         call_llm_start → [answer × N] → call_llm_end
            → (有 tool_calls) tool_call_start → tool_call_end ...
            → call_llm_start → [answer × N] → call_llm_end
         ...
         → (无 tool_calls) → query_end(Outcome)
```

| 事件 | 发射时机 | 载荷 |
|------|---------|------|
| `start` | Run 开始 | 无 |
| `question` | 用户消息进入 loop | `UserMessage` |
| `call_llm_start` | 每轮 LLM 调用前 | `Model`、`SystemPrompt`、`Tools` 摘要 |
| `answer` | LLM 流式输出中 | `Delta`、`IsReasoning`、`IsFinal` |
| `call_llm_end` | 每轮 LLM 调用结束 | `FinishReason`、`InputTokens`、`OutputTokens` |
| `tool_call_start` | 每个工具调用前 | `ToolCallID`、`Name`、`Arguments` |
| `tool_call_end` | 每个工具调用后 | `ToolCallID`、`OK`、`Output`、`IsError` |
| `query_end` | Run 结束 | `RuntimeOutcome`（FinalText、Metrics、Termination） |
| `error` | 运行时错误 | `Code`、`Message` |

---

## 8. 与 Runner 的分工

Agent 自身不关心：

- 从什么渠道接收请求（HTTP / CLI / 测试）
- 在多个 Agent 之间如何传递消息（transfer 编排）
- 如何注入模型适配器（Agent 只持有 `ChatModel` 接口，适配器的装配在构造时完成）
- 事件如何序列化传输给前端（Agent 只向 `chan<- *event.RuntimeEvent` 写入）

这些职责由上层模块（`pkg/runner`、`test-server`、适配器工厂）完成。Agent 的核心价值是提供一个**纯净的、可独立测试的 LLM 循环执行单元**。

---

## 9. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/agent/agent.go` | Agent 结构体定义、Clone、AddHandoff |
| `pkg/agent/interface.go` | Runnable 接口、LoopState、InterceptedCall |
| `pkg/agent/loop.go` | RunLoop 与辅助函数（runLoopFullSystem、buildOutcome） |
| `pkg/agent/options.go` | New 构造与所有 WithXxx 选项 |
| `pkg/agent/stream.go` | consumeStream（LLM 流式消费与事件发射） |
| `pkg/agent/helpers.go` | 工具合并、消息构建、runID 生成 |
| `pkg/agent/user_message.go` | {{key}} 模板替换、[Variables] 注入 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-29 | v0.9.5 | 初稿：聚焦 Agent 自身逻辑与启动方案，不涉及 tool/MCP/transfer/spawn 能力。 |

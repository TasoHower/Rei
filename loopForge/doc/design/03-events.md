# loopForge 事件协议

> 本文档描述 Agent 执行期间对外（SDK 调用方）输出的事件类型、载荷结构与时序。事件通过 `RuntimeEvent` 类型从 `Agent.Run()` / `Runner.Run()` 返回的 channel 中读取。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md) §5——理解 RunLoop 的迭代步骤有助于理解事件在何时发射。
>
> **关联文档**：[00-abstractions.md](00-abstractions.md) §8——RuntimeEvent 结构体的代码级定义。

***

## 1. RuntimeEvent 结构

`pkg/runtime/event/event.go`：

```go
type RuntimeEvent struct {
	Type    EventMessageType  // 事件类型字符串
	RunID   string            // 所属运行的唯一标识
	Step    int               // 当前的 LLM 调用轮次（从 0 递增）
	Payload EventPayload      // 结构化载荷，根据不同 Type 转型访问
}
```

每个事件由 4 个字段组成。`Type` 决定 `Payload` 的具体类型，调用方按 `Type` 做类型断言或使用对应的类型安全访问器（如 `ev.CallLLMStart()`）。

### 1.1 事件枚举

```go
const (
	EventStart         EventMessageType = "start"             // Run 开始
	EventQuestion      EventMessageType = "question"          // 本轮用户消息
	EventAnswer        EventMessageType = "answer"            // LLM 流式文本 delta
	EventToolCallStart EventMessageType = "tool_call_start"   // 工具调用开始
	EventToolCallEnd   EventMessageType = "tool_call_end"     // 工具调用结束
	EventCallLLMStart  EventMessageType = "call_llm_start"    // LLM 调用开始
	EventCallLLMEnd    EventMessageType = "call_llm_end"      // LLM 调用结束
	EventAgentTransfer EventMessageType = "agent_transfer"    // Agent 手递手转移
	EventSpawnStart    EventMessageType = "spawn_start"       // 子 Agent 启动
	EventSpawnEnd      EventMessageType = "spawn_end"         // 子 Agent 结束
	EventVarChange     EventMessageType = "var_change"        // 变量变更
	EventQueryEnd      EventMessageType = "query_end"         // 本轮执行结束
	EventError         EventMessageType = "error"             // 运行时错误
)
```

### 1.2 使用方式

调用方从 channel 消费事件，按 `Type` 分发：

```go
ch := ag.Run(ctx, req)

for ev := range ch {
	switch ev.Type {
	case event.EventAnswer:
		p := ev.Answer()           // 类型安全访问器
		_ = p.Delta               // 增量文本
		_ = p.IsReasoning         // 是否为推理内容
		_ = p.IsFinal             // 是否为最终完整输出

	case event.EventQueryEnd:
		p := ev.QueryEnd()
		_ = p.Outcome.FinalText   // 最终回答
		_ = p.Outcome.Metrics    // 运行指标

	case event.EventError:
		p := ev.Error()
		_ = p.Code               // 错误码
		_ = p.Message            // 错误描述
	}
}
```

***

## 2. 事件类型详述

### 2.1 `start` — Run 开始

在单 Agent 模式下为首个事件，transfer 模式中仅在首个 Agent 发射（后续 Agent 的 `SuppressBookends` 为 true）。

```go
type StartPayload struct{}
```

**载荷**：空结构体，无额外字段。

**发射时机**：RunLoop 进入迭代前。

***

### 2.2 `question` — 用户消息

```go
type QuestionPayload struct {
	UserMessage string `json:"user_message"`  // 本轮用户输入文本
}
```

**发射时机**：start 事件之后、首次 LLM 调用之前。`UserMessage` 是替换 `{{key}}` 占位符后的最终文本。

***

### 2.3 `answer` — 流式文本

LLM 回复的流式增量。可能在一个 `call_llm_start` / `call_llm_end` 周期内发射 0 到 N 次。

```go
type AnswerPayload struct {
	Delta       string `json:"delta"`                // 增量文本
	IsReasoning bool   `json:"is_reasoning,omitempty"` // true 表示推理内容（reasoning_content）
	IsFinal     bool   `json:"is_final,omitempty"`     // true 表示本轮 LLM 回复的最终完整文本
}
```

**发射时机**：

- LLM 流式返回过程中，每个 chunk 的 content/reasoning\_content 作为普通 delta 发射
- LLM 流式结束后，将完整文本以 `IsFinal=true` 再发射一次（前端需用此值覆盖已累积的文本，解决流式乱序/去重问题）

**IsReasoning 区分**：

- `IsReasoning=true`：推理过程内容（如 DeepSeek 的 `reasoning_content` 字段），前端通常渲染在独立气泡中
- `IsReasoning=false`：普通回复文本

**IsFinal 语义**：

- `IsFinal=false`（默认）：增量文本，前端追加到当前气泡
- `IsFinal=true`：本轮 LLM 回复的最终完整输出，前端**替换**当前气泡内容而非追加

***

### 2.4 `call_llm_start` — LLM 调用开始

```go
type CallLLMStartPayload struct {
	Model        string           `json:"model"`                  // 模型名
	Temperature  *float64         `json:"temperature,omitempty"`  // 温度参数
	MaxTokens    *int             `json:"max_tokens,omitempty"`   // 最大 token 数
	TopP         *float64         `json:"top_p,omitempty"`        // Top-P 采样参数
	AgentName    string           `json:"agent,omitempty"`        // 当前执行 Agent 名
	MCPServerIDs []string         `json:"mcp_server_ids,omitempty"` // 关联的 MCP 服务器 ID
	MCPToolNames []string         `json:"mcp_tool_names,omitempty"` // MCP 暴露的工具名
	SystemPrompt string           `json:"system_prompt,omitempty"` // 最终系统提示词
	Tools        []LLMToolSummary `json:"tools,omitempty"`         // 发给模型的工具定义摘要
}
```

其中 `LLMToolSummary`：

```go
type LLMToolSummary struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}
```

**发射时机**：每次调用 LLM 之前。一轮 RunLoop 可能多次发射（每次 LLM 调用一次）。

***

### 2.5 `call_llm_end` — LLM 调用结束

```go
type CallLLMEndPayload struct {
	FinishReason FinishReason `json:"finish_reason"` // 终止原因
	FullText     string       `json:"full_text,omitempty"`   // LLM 回复的完整文本
	InputTokens  int64        `json:"input_tokens"`          // 本次调用的输入 token
	OutputTokens int64        `json:"output_tokens"`         // 本次调用的输出 token
}
```

**FinishReason 枚举**：

| 值              | 含义                   |
| -------------- | -------------------- |
| `"stop"`       | LLM 正常结束，无工具调用       |
| `"tool_calls"` | LLM 发起了工具调用          |
| `"length"`     | LLM 因 max\_tokens 截断 |
| `"error"`      | LLM 调用出错             |

**发射时机**：每次 LLM 调用结束之后。与 `call_llm_start` 成对出现。

***

### 2.6 `tool_call_start` — 工具调用开始

```go
type ToolCallStartPayload struct {
	ToolCallID string `json:"tool_call_id"` // 工具调用唯一 ID
	Name       string `json:"name"`         // 工具名
	Arguments  string `json:"arguments"`    // JSON 格式的参数
}
```

**发射时机**：LLM 的 tool\_calls 解析后，每个工具执行前。

***

### 2.7 `tool_call_end` — 工具调用结束

```go
type ToolCallEndPayload struct {
	ToolCallID string `json:"tool_call_id"`
	OK         bool   `json:"ok"`
	Output     string `json:"output,omitempty"`  // JSON 格式的执行结果
	IsError    bool   `json:"is_error"`          // 是否执行出错
}
```

**发射时机**：每个工具执行完成后。与 `tool_call_start` 成对出现。

***

### 2.8 `agent_transfer` — Agent 手递手转移

```go
type AgentTransferPayload struct {
	Phase     TransferPhase `json:"phase"`               // "start" | "end"
	FromAgent string        `json:"from_agent"`          // 来源 Agent 名
	ToAgent   string        `json:"to_agent"`            // 目标 Agent 名
	Reason    string        `json:"reason,omitempty"`    // 转移原因
}
```

**语义**：Agent 通过 `transfer_to_*` 工具将会话控制权转移给另一个 Agent。

- `Phase=start`：当前 Agent 交出控制权，即将切换到目标 Agent
- `Phase=end`：目标 Agent 完成，控制权回到 Runner（实际未发射 end 事件，以目标 Agent 的 `query_end` 为转移结束标志）

**发射时机**：Runner 的 `runTransferLoop` 中，在 `RunLoop` 返回 `InterceptedCall` 后立即发射。

***

### 2.9 `spawn_start` — 子 Agent 启动

```go
type SpawnStartPayload struct {
	ChildRunID  string `json:"child_run_id"`
	ParentRunID string `json:"parent_run_id,omitempty"`
	AgentRole   string `json:"agent_role"`
	Depth       int    `json:"depth"`
	TaskSummary string `json:"task_summary,omitempty"`
}
```

**语义**：父 Agent 通过 `spawn_subagent` 工具创建了一个子 Agent，子 RunLoop 已启动。

**字段说明**：
- `ChildRunID`：子运行的唯一标识
- `ParentRunID`：父运行的 RunID
- `AgentRole`：子 Agent 的角色名
- `Depth`：子节点从根起算的深度（根为 0，第一层 spawn 为 1）
- `TaskSummary`：交给子 Agent 的任务描述（截断至 200 字符）

**发射时机**：`DefaultSpawner.Spawn()` 事件循环中，收到子 Agent 的 `EventStart` 后通过 `SpawnSpec.OutputCh` 非阻塞转发。

***

### 2.10 `spawn_end` — 子 Agent 结束

```go
type SpawnEndPayload struct {
	ChildRunID   string              `json:"child_run_id"`
	ParentRunID  string              `json:"parent_run_id,omitempty"`
	AgentRole    string              `json:"agent_role"`
	Depth        int                 `json:"depth"`
	OK           bool                `json:"ok"`
	Status       string              `json:"status"`
	ErrorCode    string              `json:"error_code,omitempty"`
	FinalTextLen int                 `json:"final_text_len,omitempty"`
	Metrics      *outcome.RunMetrics `json:"metrics,omitempty"`
}
```

**语义**：子 Agent 运行结束（正常完成、失败或父级取消）。

**字段说明**：
- `ChildRunID` / `ParentRunID` / `AgentRole` / `Depth`：同 `spawn_start`
- `OK`：子 Agent 是否正常完成（`Termination == completed`）
- `Status`：状态字符串（`completed` / `failed` / `rejected`）
- `ErrorCode`：失败时的错误码（如 `parent_cancelled`）
- `FinalTextLen`：子 Agent 最终回答的字符长度（不含完整文本，避免大文本驻留事件通道）
- `Metrics`：子 Agent 的 token 消耗指标（InputTokens / OutputTokens / TotalTokens / Steps / TotalCostUSD），父 Agent 最终 `query_end` 的 `Metrics` 已累加子树消耗

**发射时机**：`DefaultSpawner.Spawn()` 事件循环中，收到子 Agent 的 `EventQueryEnd` 后通过 `SpawnSpec.OutputCh` 非阻塞转发。若父级取消导致子未正常结束，同样发射 `spawn_end` 但 `OK=false`、`ErrorCode=parent_cancelled`。

***

### 2.11 `var_change` — 变量变更

```go
type VarChangePayload struct {
	Operation string `json:"operation"`          // "set" | "delete"
	Key       string `json:"key"`                // 变量键名
	Value     any    `json:"value,omitempty"`    // 新值（delete 时为空）
	Agent     string `json:"agent,omitempty"`    // 触发变更的 Agent 名
}
```

**发射时机**：变量通过 `var_set` 工具写入或删除时。当前 RunLoop 尚未统一 emit 该事件，载荷已定义但运行时可观测性待补充。

***

### 2.12 `query_end` — 本轮执行结束

```go
type QueryEndPayload struct {
	Outcome *outcome.RuntimeOutcome `json:"outcome"`
}
```

`RuntimeOutcome` 包含最终的运行结果：

```go
type RuntimeOutcome struct {
	RunID         string                    // 运行 ID
	FinalText     string                    // 最终回答文本
	Termination   TerminationReason         // 终止原因
	Metrics       RunMetrics                // 运行指标
	ChildRunIDs   []string                  // 子运行 ID 列表
	TransferChain []string                  // transfer 经过的 Agent 名链
	VarStore      *variable.VarStore        // 变量快照（JSON 序列化时排除）
}
```

**TerminationReason 枚举**：

| 值             | 含义                                    |
| ------------- | ------------------------------------- |
| `"completed"` | 正常完成（LLM 无 tool\_calls 且无待处理异步 spawn） |
| `"max_steps"` | 达到最大步数                                |
| `"cancelled"` | 被取消（ctx 取消）                           |
| `"error"`     | 运行时出错                                 |

**RunMetrics**：

```go
type RunMetrics struct {
	Model        string  `json:"model"`          // 模型名
	InputTokens  int64   `json:"input_tokens"`   // 输入 token 总数
	OutputTokens int64   `json:"output_tokens"`  // 输出 token 总数
	TotalTokens  int64   `json:"total_tokens"`   // 总 token 数
	TotalCostUSD float64 `json:"total_cost_usd"` // 估算成本（美元）
	Steps        int     `json:"steps"`          // ReAct 步数
}
```

Transfer 模式下，Metrics 为所有 Agent hop 的累积值。

**发射时机**：每个 Run **只会发射一次** `query_end`，是事件流中的最后一个有意义事件。后续 channel 关闭。

***

### 2.13 `error` — 运行时错误

```go
type ErrorPayload struct {
	Code    string `json:"code"`    // 错误码（如 "invalid_config"、"generate"、"tool_exec"）
	Message string `json:"message"` // 可读的错误描述
}
```

**发射时机**：发生不可恢复的错误时。`error` 事件后通常紧跟着 `query_end`（Termination=error），然后 channel 关闭。

***

## 3. 事件时序

### 3.1 单 Agent 模式（无工具调用）

```
start → question → call_llm_start → answer(Δ) × N → call_llm_end(stop)
    → query_end(completed)
```

### 3.2 单 Agent 模式（含工具调用）

```
start → question → call_llm_start → answer(Δ) × N → call_llm_end(tool_calls)
    → tool_call_start → tool_call_end → call_llm_start → answer(Δ) × N → call_llm_end(stop)
    → query_end(completed)
```

### 3.3 单 Agent 模式（含 spawn）

```
start → question → call_llm_start → call_llm_end(tool_calls)
    → tool_call_start(name=spawn_subagent)
    → spawn_start(depth=1)                                    ← spawn 子启动
    → ...（子 Agent 内部事件不转发到父事件流）
    → spawn_end(depth=1, ok)                                  ← spawn 子结束
    → tool_call_end
    → call_llm_start → answer(Δ) × N → call_llm_end(stop)
    → query_end(completed)
```

### 3.4 Transfer 模式

```
[Agent A] start → question → call_llm_start → ... → call_llm_end(tool_calls)
    → tool_call_start(name=transfer_to_B)
    → agent_transfer(phase=start, from=A, to=B, reason)
    → tool_call_end
[Agent B] call_llm_start → answer(Δ) × N → call_llm_end(stop)
    → query_end(completed, TransferChain=[A, B])
```

注意：transfer 模式下 `start`/`question` 仅在首个 Agent 发射，后续 Agent 的 `SuppressBookends=true`。

### 3.5 错误场景

```
start → question → call_llm_start → call_llm_end(error)
    → error(code=generate, message=...) → query_end(termination=error)
```

或配置错误直接结束：

```
error(code=invalid_config, message=...) → query_end(termination=error)
```

***

## 4. 事件组合关系

| 事件配对                                                        | 说明                                             |
| ----------------------------------------------------------- | ---------------------------------------------- |
| `call_llm_start` ↔ `call_llm_end`                           | 一次 LLM 调用，必有 1:1 配对                            |
| `tool_call_start` ↔ `tool_call_end`                         | 一个工具调用，必有 1:1 配对                               |
| `start` ↔ `query_end`                                       | 一个 Run，可能有 0 或 1 对（transfer 模式子 Agent 无 start） |
| `agent_transfer(phase=start)` ↔ `agent_transfer(phase=end)` | 一个 transfer 配对                                          |
| `spawn_start` ↔ `spawn_end`                                 | 一个 spawn 配对                                             |

***

## 5. SDK 调用方消费示例

### 5.1 Go

```go
ch := r.Run(ctx, req)

var finalText string
for ev := range ch {
	switch ev.Type {
	case event.EventAnswer:
		p := ev.Answer()
		if p.IsReasoning {
			// 推理内容，可选在前端"思考过程"气泡中展示
		}
		if p.IsFinal {
			finalText = p.Delta  // 最终完整文本，替换而非追加
		} else {
			// 普通流式增量，追加到气泡
		}

	case event.EventQueryEnd:
		p := ev.QueryEnd()
		outcome := p.Outcome
		fmt.Printf("完成: termination=%s, tokens=%d\n",
			outcome.Termination, outcome.Metrics.TotalTokens)

	case event.EventError:
		p := ev.Error()
		fmt.Printf("错误: code=%s message=%s\n", p.Code, p.Message)
	}
}
```

### 5.2 JavaScript / TypeScript（SSE 消费）

test-server 将事件序列化为 SSE，前端消费示例：

```javascript
function handleEvent(eventType, dataStr) {
    let payload = JSON.parse(dataStr);
    const data = payload.data || {};

    switch (eventType) {
        case 'answer':
            if (data.is_reasoning) {
                // 渲染推理气泡
                reasoningBubble.textContent += data.delta;
            } else {
                if (data.is_final) {
                    // 替换气泡内容
                    bubble.textContent = data.delta;
                } else {
                    // 追加增量
                    bubble.textContent += data.delta;
                }
            }
            break;

        case 'query_end':
            const oc = data.outcome || {};
            console.log('完成:', oc.termination, '步数:', oc.Metrics?.steps);
            break;

        case 'error':
            console.error('错误:', data.code, data.message);
            break;
    }
}
```

***

## 6. 序列化（SSE JSON 格式）

test-server 中 `runtimeEventToSSE` 将每个 `RuntimeEvent` 映射为：

```go
type SSEEvent struct {
	Type string `json:"type"`           // 事件类型字符串
	Step int    `json:"step,omitempty"`  // 步数
	Data any    `json:"data,omitempty"`  // 对应 Payload 的 JSON 序列化
}
```

SSE 协议格式（test-server 使用 CloudWeGo Hertz SSE）：

```
event: answer
data: {"type":"answer","step":0,"data":{"delta":"Hello","is_reasoning":false}}
```

`query_end` 有特殊处理——若 `VarStore` 非空，额外序列化 `variables` 字段：

```json
{
    "type": "query_end",
    "step": 0,
    "data": {
        "outcome": { "final_text": "...", "termination": "completed", "metrics": {...} },
        "variables": { "vars": [...] }
    }
}
```

***

## 7. 与代码目录的对应

| 文件                               | 职责                                                          |
| -------------------------------- | ----------------------------------------------------------- |
| `pkg/runtime/event/event.go`     | RuntimeEvent 结构体、所有 Payload 类型、EventMessageType 常量、Emit 构造器 |
| `pkg/runtime/outcome/outcome.go` | RuntimeOutcome、RunMetrics、TerminationReason                 |
| `pkg/agent/loop.go`              | RunLoop 中各事件的发射时机                                           |
| `pkg/agent/stream.go`            | consumeStream 中 answer 事件的发射与 IsFinal 语义                    |
| `pkg/runner/runner.go`           | transfer 模式下 agent\_transfer 事件的发射                          |
| `pkg/agent/spawn.go`             | spawn 子树 `spawn_start` / `spawn_end` 事件的发射（通过 OutputCh） |
| `test-server/main.go`            | runtimeEventToSSE 序列化函数                                     |

***

## 8. 相关文档

| 文档                                          | 关系                     |
| ------------------------------------------- | ---------------------- |
| [01-agent-core.md](01-agent-core.md) §5     | RunLoop 迭代步骤——事件发射的源头  |
| [02-runner-core.md](02-runner-core.md) §7 | Transfer 模式下的事件流差异 |
| [00-abstractions.md](00-abstractions.md) §8 | RuntimeEvent 的代码级定义    |
| `doc/design/data-fusion.md` | 事件类型与现网 runner 的类型语义对齐 |
| `doc/design/spawn-runtime-rules.md` | spawn 子 Agent 的事件隔离规则（§1 动态隔离 / §7 React 聚合） |

***

# 变更日志

| 日期         | 版本     | 变更说明                                            |
| ---------- | ------ | ----------------------------------------------- |
| 2026-04-30 | v0.9.5 | 初稿：全面描述事件协议、11 种事件类型的载荷与语义、5 种场景的事件时序、SDK 消费示例。 |


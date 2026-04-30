# loopForge ChatModel（模型抽象层）

> 本文档描述 loopForge 的 **ChatModel 接口体系**——Agent 与 LLM 之间的抽象层。它定义了 LLM 调用的最小契约（`BaseChatModel` + `ToolCallingChatModel`），以及三个适配器实现（Lark/方舟、DeepSeek、OpenAI SDK）和通用能力（非流式包装、CallOption 配置体系）。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md)——Agent 通过 `ChatModel` 字段调用 LLM；[10-spawn.md](10-spawn.md)——子 Agent 使用 `WrapNonStream` 强制非流式。

---

## 1. 概念

### 1.1 为什么需要模型抽象层

loopForge 的 Agent 不绑定任何特定 LLM 厂商。`Agent.ChatModel` 字段的类型是 `model.ToolCallingChatModel` 接口——Agent 只调用 `Generate()` 和 `Stream()`，不需要知道背后是火山方舟、DeepSeek、还是 OpenAI SDK。

```
Agent.RunLoop
  │
  ├── ChatModel.Generate(ctx, messages, opts...)  // 非流式
  ├── ChatModel.Stream(ctx, messages, opts...)     // 流式
  └── ChatModel.WithTools(tools)                   // 绑定工具
         ↑
         │ 接口层 (pkg/model)
    ┌────┴────────────────────┐
    │                          │
    v                          v
 OpenAI SDK    DeepSeek / Lark HTTP
```

### 1.2 包结构

```
pkg/model/
├── chatmodel.go            # 别名重导出（BaseChatModel/ToolCallingChatModel）
├── option.go               # CallOption 体系别名
├── message.go / toolinfo.go / stream.go  # 类型别名
├── nonstream.go            # WrapNonStream（强制非流式包装）
├── interface/
│   └── chatmodel.go         # 接口定义（BaseChatModel / ToolCallingChatModel）
├── types/
│   ├── message.go           # Message / ToolCallPart / Role 枚举
│   ├── toolinfo.go          # ToolInfo / ToolCallHandler
│   ├── option.go            # CallConfig / CallOption / ApplyCallOptions
│   └── stream.go            # MessageStreamReader / SliceStreamReader
└── adapters/
    ├── lark/                # 火山方舟 (Ark) 适配器
    ├── deepseek/            # DeepSeek API 适配器
    └── openai/              # OpenAI SDK 适配器
```

`pkg/model` 包暴露的名字通过别名指向 `internal/interface` 和 `internal/types`——调用方只用 `import "loopforge/pkg/model"` 即可拿到全部公开类型。

---

## 2. ChatModel 接口

### 2.1 BaseChatModel

`pkg/model/interface/chatmodel.go`：

```go
type BaseChatModel interface {
	Generate(ctx context.Context, input []*types.Message, opts ...types.CallOption) (*types.Message, error)
	Stream(ctx context.Context, input []*types.Message, opts ...types.CallOption) (types.MessageStreamReader, error)
}
```

两个方法覆盖两种 LLM 调用模式：

| 方法 | 行为 | 返回值 |
|------|------|--------|
| `Generate` | 非流式调用——发送全部消息，阻塞等待完整回复 | `*Message`（含 Content + ToolCalls + token 计数） |
| `Stream` | 流式调用——发送全部消息，返回 Reader | `MessageStreamReader`（可逐 chunk `Recv()`） |

### 2.2 ToolCallingChatModel

```go
type ToolCallingChatModel interface {
	BaseChatModel

	// WithTools 返回绑定了工具列表的**新实例**，不得修改原实例。
	// 并发安全：Generate/Stream/WithTools 可被多个 goroutine 同时调用。
	WithTools(tools []*types.ToolInfo) (ToolCallingChatModel, error)
}
```

关键约束（注释中明确要求）：

- `WithTools` **不可变模式**——返回新实例，不减异动原对象
- **并发安全**——多个 goroutine 同时 Generate/Stream/WithTools 是合法的

Agent 在 `bindModel` 中调用 `WithTools` 后获得带工具能力的模型实例，该实例被用于本次 `RunLoop` 的所有 `Generate` / `Stream` 调用。

### 2.3 MessageStreamReader

`pkg/model/types/stream.go`：

```go
type MessageStreamReader interface {
	Recv() (*Message, error)
}
```

- 正常 chunk → `(*Message, nil)`
- 流式结束 → `(*Message, io.EOF)`
- 调用出错 → `(nil, error)`

`consumeStream`（`pkg/agent/loop.go`）循环调用 `Recv()` 直到 `io.EOF`，期间发射 `event.Answer` 增量。

---

## 3. Message 与 ToolInfo

### 3.1 Message

`pkg/model/types/message.go`：

```go
type Role string

const (
	RoleUser      Role = "user"       // 用户消息
	RoleAssistant Role = "assistant"   // 模型回复（含 tool_calls）
	RoleSystem    Role = "system"      // 系统提示词
	RoleTool      Role = "tool"        // 工具调用结果
)

type Message struct {
	Role             Role            // 消息角色
	Content          string          // 文本内容
	ToolCalls        []ToolCallPart  // 模型发起的工具调用列表（仅 Role=Assistant 时有效）
	ToolCallID       string          // 工具调用 ID（Role=Tool 时关联到某次调用）
	Name             string          // 工具名（Role=Tool 时标识来源）
	ReasoningContent string          // 推理内容（适配层透传，DeepSeek reasoning_content）
	InputTokens      int64           // 本条消息的输入 token（适配层填充）
	OutputTokens     int64           // 本条消息的输出 token（适配层填充）
}
```

### 3.2 ToolInfo 与 ToolCallPart

```go
type ToolInfo struct {
	Name        string                 // 工具名（对 LLM 可见）
	Description string                 // 工具描述
	Parameters  map[string]interface{} // JSON Schema 参数定义
	Handle      ToolCallHandler `json:"-"` // 执行回调（Go 函数）
}

type ToolCallHandler func(ctx context.Context, argumentsJSON string) (string, error)

type ToolCallPart struct {
	ID        string          // 工具调用 ID
	Name      string          // 工具名
	Arguments string          // JSON 参数
	Handle    ToolCallHandler // 快捷回调（可在此直接调用）
}
```

---

## 4. CallOption 配置体系

`pkg/model/types/option.go`：

```go
type CallConfig struct {
	Temperature   *float64    // 采样温度
	MaxTokens     *int        // 最大生成 token
	TopP          *float64    // Top-P 采样
	ModelOverride string      // 模型名覆盖
	Tools         []*ToolInfo // 工具列表
}

type CallOption func(*CallConfig)
```

**标准选项**：

| 选项 | 作用 |
|------|------|
| `WithTemperature(v float64)` | 设置采样温度 |
| `WithMaxTokens(n int)` | 设置最大生成 token |
| `WithTopP(v float64)` | 设置 Top-P 采样 |
| `WithModel(name string)` | 设置模型名（覆盖 Agent 默认模型） |
| `WithTools(tools []*ToolInfo)` | 替换工具列表 |

**ApplyCallOptions** 合并多个 Option 到一个 `CallConfig`：

```go
func ApplyCallOptions(opts ...CallOption) CallConfig
```

Agent 在 `resolveCallOptions` 中合并 Agent 级 defaults + Request 级覆盖 → 每次 LLM 调用使用的最终配置。

---

## 5. 三个适配器

### 5.1 Lark（火山方舟 / Ark）

`pkg/model/adapters/lark/client.go`：

```go
const DefaultBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

type LarkChatModel struct {
	client    *arkruntime.Client     // 官方 volcengine-go-sdk
	modelName string
	tools     []*lpmodel.ToolInfo
}
```

构造：

```go
func NewLarkChatModel(apiKey, baseURL, modelName string) *LarkChatModel
```

**特点**：
- 基于官方 `volcengine-go-sdk`（`arkruntime`）
- 支持流式和非流式
- Runner 的 `applyDefaultLarkIfNeeded` 在 `ChatModel == nil` 时自动从环境变量装配

### 5.2 DeepSeek

`pkg/model/adapters/deepseek/client.go`：

```go
const DefaultBaseURL = "https://api.deepseek.com/"

type DeepSeekChatModel struct {
	client    *dspk.Client       // 官方 deepseek-go SDK
	modelName string
	tools     []*lpmodel.ToolInfo
}
```

构造：

```go
func NewDeepSeekChatModel(apiKey, baseURL, modelName string) *DeepSeekChatModel
```

**特点**：
- 基于官方 `deepseek-go` SDK
- 支持 `ReasoningContent`（`reasoning_content` 字段）的透传——`Message.ReasoningContent` 由 DeepSeek 适配器填充，Agent 的 `consumeStream` 通过 `IsReasoning=true` 的 `answer` 事件发到前端
- Runner 的 `ApplyDeepSeekFromConfig` 在 `ChatModel == nil` 时自动从环境变量装配

### 5.3 OpenAI SDK

`pkg/model/adapters/openai/client.go`：

```go
type OpenAIChatModel struct {
	client    *openai.Client       // github.com/openai/openai-go
	modelName string
	tools     []*lpmodel.ToolInfo
}
```

构造：

```go
func NewOpenAIChatModel(apiKey, baseURL, modelName string) lpmodel.ToolCallingChatModel
```

**特点**：
- 基于官方 `github.com/openai/openai-go` SDK——OpenAI 兼容 API 的事实标准
- 支持流式和非流式
- 作为 Lark 和 DeepSeek 之后的**最后兜底**适配器：Runner 的 `applyDefaultOpenAIIfNeeded` 在 Lark 装配之后、若 `OPENAI_API_KEY` 存在则自动装配
- 环境变量：`OPENAI_API_KEY`、`OPENAI_BASE_URL`、`OPENAI_MODEL`

> **v0.9.6 变更**：此适配器替代了之前基于 `github.com/agentizen/agent-sdk-go` 的 `SDKChatModel`（`agentsdk` 包），该包已被整体移除。

### 5.4 适配器对比

| 维度 | Lark | DeepSeek | OpenAI SDK |
|------|------|----------|-------------|
| 底层 SDK | `volcengine-go-sdk` | `deepseek-go` | `openai-go` |
| ReasoningContent | 不支持 | ✅ 支持 | 不支持 |
| 构造 API | `NewLarkChatModel(key, url, name)` | `NewDeepSeekChatModel(key, url, name)` | `NewOpenAIChatModel(key, url, name)` |
| 环境变量装配 | `LARK_API_KEY` 等 | `DEEPSEEK_API_KEY` 等 | `OPENAI_API_KEY` |

### 5.5 agent-sdk-go 已移除

agent-sdk-go（`github.com/agentizen/agent-sdk-go` v0.17.0）是 loopForge v0.1.0 初期引入的依赖，在早期版本中曾被用作模型调用层。在 v0.9.6 中已完成全量移除：

- `pkg/model/adapters/agentsdk/` 整个包已删除
- `go.mod` 中 `github.com/agentizen/agent-sdk-go` 依赖已移除
- 模型兜底能力由 `github.com/openai/openai-go`（OpenAI SDK）替代

移除后 loopForge 的模型适配由以下三个适配器承担：
- **Lark（火山方舟/Ark）** — 基于 `volcengine-go-sdk`
- **DeepSeek** — 基于 `deepseek-go`
- **OpenAI SDK** — 基于 `openai-go`（通用兜底）

### 5.6 自定义 ChatModel

loopForge 的模型抽象层设计刻意保持极简。任何实现了 `ToolCallingChatModel` 接口的类型都可以作为 `Agent.ChatModel` 使用——不限制厂商、不限制 SDK、不限制实现方式。

```go
// 自定义 ChatModel 只需实现三个方法
type MyChatModel struct {
	// 内部状态：HTTP client、API key、model name 等
}

// 1. 生成式调用（必需）
func (m *MyChatModel) Generate(ctx context.Context, input []*model.Message, opts ...model.CallOption) (*model.Message, error) {
	// 转换 loopForge Message → OpenAI / Anthropic / Gemini / ... 格式 → HTTP 调用 → 转换回
}

// 2. 流式调用（必需）
func (m *MyChatModel) Stream(ctx context.Context, input []*model.Message, opts ...model.CallOption) (model.MessageStreamReader, error) {
	// 同上，但返回 SSE → MessageStreamReader 适配
}

// 3. 工具绑定（必需）——不可变模式，返回新实例
func (m *MyChatModel) WithTools(tools []*model.ToolInfo) (model.ToolCallingChatModel, error) {
	dup := make([]*model.ToolInfo, len(tools))
	copy(dup, tools)
	return &MyChatModel{/* copy other fields */, tools: dup}, nil
}
```

三个接口加起来不到 50 行模板代码。工作量集中在**消息格式转换**（`loopforge Message` ↔ 厂商 API 的 request/response）——这是任何 LLM 接入都无法避免的。loopForge 做了的是**不强迫用户绑定特定厂商的 SDK**。

```go
// 使用自定义模型
ag := agent.New(&MyChatModel{apiKey: "...", model: "gpt-4o"},
	agent.WithSystemInstructions("You are a helpful assistant."),
	agent.WithMaxSteps(32),
)
```

---

## 6. WrapNonStream：强制非流式包装

`pkg/model/nonstream.go`：

```go
func WrapNonStream(chat ToolCallingChatModel) ToolCallingChatModel
```

用于 spawn 子 Agent 的场景（`spawn-runtime-rules.md` §6）：

```go
type nonStreamModel struct {
	inner ToolCallingChatModel
}

func (m *nonStreamModel) Stream(ctx, input, opts...) (MessageStreamReader, error) {
	// 降级：把 Stream 转为 Generate 调用
	msg, err := m.inner.Generate(ctx, input, opts...)
	return types.NewSliceStreamReader([]*types.Message{msg}), nil
}
```

子 Agent 的 `ChatModel` 在 `DefaultSpawner.Spawn()` 中被调用 `model.WrapNonStream()` 包装——所有流式调用被强制降级为一次生成式调用，降低网络开销。

---

## 7. 环境变量自动装配

Runner 在 `Run()` 中调用 `applyDefaultLarkIfNeeded(a)`——若 Agent 的 `ChatModel` 为 nil，自动从环境变量构造默认适配器。

### 7.1 Lark 优先级

`LARK_API_KEY` > `DOUBAO_API_KEY` > `ARK_API_KEY`（取第一个非空）

`LARK_BASE_URL` > `DOUBAO_BASE_URL`（取第一个非空，空则默认 `https://ark.cn-beijing.volces.com/api/v3`）

`LARK_MODEL` > `ARK_MODEL` > `DOUBAO_MODEL`（取第一个非空，空则默认 `deepseek-v3-2-251201`）

### 7.3 OpenAI 兜底

`OPENAI_API_KEY`（必填，无则不装配）

`OPENAI_BASE_URL`（空则默认 `https://api.openai.com/v1`）

`OPENAI_MODEL`（空则默认 `gpt-4o-mini`）

OpenAI 装配在 Lark 和 DeepSeek 之后执行，作为最后兜底。即：优先专用适配器 → 最后回退到 OpenAI SDK 通用适配器。

### 7.2 DeepSeek 优先级

`DEEPSEEK_API_KEY`（必填，无则不装配）

`DEEPSEEK_BASE_URL`（空则默认 `https://api.deepseek.com/`）

`DEEPSEEK_MODEL`（空则默认 `deepseek-chat`）

---

## 8. 完整数据流

```
Runner.Run()
  │
  ├── applyDefaultLarkIfNeeded(agent)  / ApplyDeepSeekFromConfig(agent)
  │     └── ChatModel == nil? → 从环境变量构造适配器
  │
  ├── Agent.RunLoop:
  │     │
  │     ├── resolveCallOptions(req)
  │     │     └── Agent.CallOptions + req.Options.Model → CallConfig
  │     │
  │     ├── bindModel(ctx, varTools...):
  │     │     ├── 合并 ToolInfos + mcpToolInfos + ExtraTools + varTools
  │     │     └── ChatModel.WithTools(allTools) → 带工具的新实例
  │     │
  │     └── for step:
  │           ├── emit(call_llm_start, Model=modelName, SystemPrompt=..., Tools=...)
  │           ├── ChatModel.Stream(ctx, messages, ApplyCallOptions(opts...))
  │           │     ├── AnswerPayload{Delta, IsReasoning, IsFinal} → emit(answer)
  │           │     └── 返回 StreamResult{Text, ToolCalls, InputTokens, OutputTokens}
  │           ├── emit(call_llm_end, FinishReason, FullText, InputTokens, OutputTokens)
  │           └── 若有 tool_calls → executeToolCalls → tool.Invoke → 循环
```

---

## 9. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/model/interface/chatmodel.go` | BaseChatModel、ToolCallingChatModel 接口定义 |
| `pkg/model/types/message.go` | Message、Role、ToolCallPart |
| `pkg/model/types/toolinfo.go` | ToolInfo、ToolCallHandler |
| `pkg/model/types/option.go` | CallConfig、CallOption（Temperature/MaxTokens/TopP/Model/Tools）、ApplyCallOptions |
| `pkg/model/types/stream.go` | MessageStreamReader、SliceStreamReader |
| `pkg/model/chatmodel.go` | 接口别名重导出 |
| `pkg/model/option.go` | CallConfig/CallOption 别名 + 函数重导出 |
| `pkg/model/nonstream.go` | WrapNonStream（Spawn 非流式包装） |
| `pkg/model/adapters/lark/client.go` | LarkChatModel（火山方舟/Ark） |
| `pkg/model/adapters/deepseek/client.go` | DeepSeekChatModel（DeepSeek API） |
| `pkg/model/adapters/openai/client.go` | OpenAIChatModel（OpenAI SDK） |
| `pkg/runner/lark_default.go` | applyDefaultLarkIfNeeded（环境变量自动装配） |
| `pkg/runner/deepseek_default.go` | ApplyDeepSeekFromConfig（环境变量自动装配） |
| `pkg/runner/openai_default.go` | applyDefaultOpenAIIfNeeded（环境变量自动装配，最后兜底） |

---

## 10. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) §3 | Agent 的 `ChatModel` 字段——此接口的使用方 |
| [10-spawn.md](10-spawn.md) §3.4 | `WrapNonStream` 在子 Agent 中的使用 |
| [02-runner-core.md](02-runner-core.md) §4 | Runner 的环境变量装配逻辑 |
| [00-abstractions.md](00-abstractions.md) §10 | Message/ToolInfo 的代码级定义 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：ChatModel 接口体系、CallOption 配置、三个适配器对比、WrapNonStream、环境变量装配。 |
| 2026-04-30 | v0.9.6 | 移除 agent-sdk-go 适配器，新增 OpenAI SDK 适配器（`pkg/model/adapters/openai/`）。 |

# loopForge 实施计划 — v0.9.6（全量移除 agent-sdk-go，OpenAI SDK 填充兜底）

> **对应版本进度**：`doc/log/progress-v0.9.6.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的任务拆分与执行顺序；**先于编码**成文。  
> **主目标**：全量移除 `github.com/agentizen/agent-sdk-go` 残留，使用 `github.com/openai/openai-go` 重新实现 `OpenAIChatModel` 兜底适配器。

---

## 1. 背景

### 1.1 当前依赖状态

经过全量调查，`agent-sdk-go` 在 loopForge 中遗留的依赖关系如下。

**依赖链路（当前）**：

```
go.mod
  └── github.com/agentizen/agent-sdk-go v0.17.0
        └── pkg/model/adapters/agentsdk/   (4 个文件)
              ├── chatmodel.go      — SDKChatModel：sdkmodel.Provider + sdkmodel.Model 适配
              ├── convert.go        — MessagesToSDKRequest / FromSDKResponse 消息转换
              ├── settings.go       — CallConfig → sdkmodel.Settings 映射
              └── stream_reader.go  — chanStreamReader（MessageStreamReader 实现，无 agent-sdk-go 导入）
```

**被依赖关系（外部引用）**：

| 维度 | 详情 |
|------|------|
| 外部 Go 代码导入 `agentsdk` | **零** — 没有其他包 `import "loopforge/pkg/model/adapters/agentsdk"` |
| 外部 Go 代码导入 `github.com/agentizen/agent-sdk-go` | **零** — 只在 agentsdk 包内部引用 |
| `toolinfo.go` 中 `ToOpenAITool()` | 调用方仅 `agentsdk/convert.go`（toolsToIface 第 175 行），其他适配器（lark、deepseek）有各自的 tool 转换函数 |
| `toolinfo.go` 中引用 agent-sdk-go 的注释 | 第 12 行注释 `returns a value suitable for agent-sdk-go Request.Tools` — 仅注释，不影响编译 |

### 1.2 v0.9.5 铺垫

v0.9.5 已修正 `doc/decision/sdk-selection.md` 中将 agent-sdk-go 描述为"循环内核"的错误，将其角色修正为"模型适配层"，并在设计文档 `doc/design/11-chatmodel.md` 的 §5.5 中明确标注"将在未来版本中彻底移除 agent-sdk-go"。

### 1.3 模型兜底能力

`agentsdk.SDKChatModel` 是 loopForge 的"通用兜底"适配器——它通过 agent-sdk-go 的 `Provider` 接口抽象，能够适配任何实现了该接口的 LLM 厂商。

当前 loopForge 已有两个专用适配器：
- **Lark（火山方舟）** — 基于 `volcengine-go-sdk`（`arkruntime`）
- **DeepSeek** — 基于 `deepseek-go`

agent-sdk-go 的兜底角色在 v0.9.6 中被以下两者替代：
1. **OpenAI SDK**（`github.com/openai/openai-go`） — 作为新的通用兜底适配器，因为 OpenAI 兼容 API 已成为业界事实标准
2. **Runner 环境变量装配** — 新增 `ApplyOpenAIFromConfig` 函数，使 Agent 未设置 ChatModel 时自动从 `OPENAI_API_KEY` 等环境变量装配

### 1.4 变更范围总结

| 类别 | 变更 | 操作 |
|------|------|------|
| **新增** | `pkg/model/adapters/openai/` (3 文件) | 新增——OpenAI SDK 兜底适配器 |
| **新增** | `pkg/runner/openai_default.go` | 新增——环境变量自动装配 |
| **新增** | `go.mod` 新增 `github.com/openai/openai-go` | 新增——新依赖 |
| **删除** | `pkg/model/adapters/agentsdk/` (4 文件) | 删除——agent-sdk-go 适配器 |
| **删除** | `go.mod`/`go.sum` agent-sdk-go | 删除——旧依赖 |
| **更新** | `pkg/model/types/toolinfo.go` | 更新——注释 |
| **文档** | `doc/design/11-chatmodel.md` | 修订 |
| **文档** | `doc/design/00-abstractions.md` | 修订 |
| **文档** | `doc/decision/sdk-selection.md` | 修订 |
| **文档** | `README.md`（根目录） | 修订 |

### 1.5 不涉及的范围

以下模块与 agent-sdk-go 无任何依赖，不在本版本范围：

- `pkg/agent/` — 完整的自实现 Agent loop
- `internal/engine/` — 引擎核心状态管理
- `pkg/model/adapters/lark/` — 火山方舟适配器（基于 `volcengine-go-sdk`）
- `pkg/model/adapters/deepseek/` — DeepSeek 适配器（基于 `deepseek-go`）
- `pkg/tool/`、`pkg/mcp/`、`pkg/skill/`、`pkg/variable/` — 工具/MCP/Skills 层
- `test-server/`、`SeRagLF/` — 其他子项目

---

## 2. 任务拆分与执行顺序

### 作业 #1 — `go.mod` 新增 OpenAI SDK 依赖

**目标**：在 go.mod 中加入 `github.com/openai/openai-go`

操作：
1. 在 `go.mod` `require` 块中新增 `github.com/openai/openai-go v0.1.0`（以实际最新稳定版为准）
2. 运行 `go mod tidy` 拉取依赖并生成 `go.sum`

影响：新增直接依赖，无其他 Go 代码变更。

### 作业 #2 — 创建 `pkg/model/adapters/openai/` 包（OpenAI SDK 兜底适配器）

**目标**：参考 Lark 和 DeepSeek 适配器的模式，用 `github.com/openai/openai-go` 实现 `ToolCallingChatModel`。

新建 3 个文件：

#### 2.1 `pkg/model/adapters/openai/client.go`

```go
package openai

// OpenAIChatModel implements lpmodel.ToolCallingChatModel using github.com/openai/openai-go.
type OpenAIChatModel struct {
    client    *openai.Client       // OpenAI SDK 客户端
    modelName string
    tools     []*lpmodel.ToolInfo
}
```

构造方法：
```go
// NewOpenAIChatModel returns a ToolCallingChatModel backed by OpenAI-compatible chat completions.
func NewOpenAIChatModel(apiKey, baseURL, modelName string) lpmodel.ToolCallingChatModel
```

必须实现：
- `WithTools(tools []*lpmodel.ToolInfo) (lpmodel.ToolCallingChatModel, error)` — 不可变模式，返回新实例
- `Generate(ctx, input, opts...) (*lpmodel.Message, error)` — 非流式调用
  - 转换 loopForge Message → OpenAI ChatCompletionMessage（调用 convert 层）
  - 转换 loopForge ToolInfo → OpenAI ChatCompletionToolParam（调用 convert 层）
  - 应用 CallOption（Temperature/MaxTokens/TopP/ModelOverride）
  - 调用 `client.Chat.Completions.New(ctx, params)`
  - 转换 OpenAI Response → loopForge Message
- `Stream(ctx, input, opts...) (lpmodel.MessageStreamReader, error)` — 流式调用
  - 同上，但设置 `StreamOptions: &openai.ChatCompletionStreamParams{IncludeUsage: true}`
  - 调用 `client.Chat.Completions.NewStreaming(ctx, params)`
  - goroutine 内循环 `stream.Next()` → 逐 chunk 投递到 `chan streamPart`
  - 工具调用片段累积（tool call accumulator，与 lark/deepseek 相同的 `toolCallAccumulator` 模式）

#### 2.2 `pkg/model/adapters/openai/convert.go`

消息转换函数：

| 函数 | 作用 |
|------|------|
| `toOpenAIMessages(msgs []*lpmodel.Message) ([]openai.ChatCompletionMessageParamPart, error)` | loopForge Message → OpenAI API 参数格式 |
| `toOpenAITools(tools []*lpmodel.ToolInfo) []openai.ChatCompletionToolParam` | loopForge ToolInfo → OpenAI tool 定义 |
| `fromOpenAIResponse(choice *openai.ChatCompletionToken) *lpmodel.Message` | OpenAI 非流式响应 → loopForge Message |
| `mergeToolLists(bound, perCall []*lpmodel.ToolInfo) []*lpmodel.ToolInfo` | 工具列表合并（与 lark/deepseek 相同模式） |

消息角色映射：
| loopForge Role | OpenAI Role |
|----------------|-------------|
| `RoleSystem` | `openai.ChatCompletionMessageParamRoleSystem`（合并到 messages 列表头部） |
| `RoleUser` | `openai.ChatCompletionMessageParamRoleUser` |
| `RoleAssistant` | `openai.ChatCompletionMessageParamRoleAssistant` + `tool_calls` |
| `RoleTool` | `openai.ChatCompletionMessageParamRoleTool` + `tool_call_id` |

#### 2.3 `pkg/model/adapters/openai/stream.go`

流式处理设施（与 lark/deepseek 的 stream.go 相同模式）：

```go
type streamPart struct {
    msg *lpmodel.Message
    err error
}

type chanStreamReader struct { ch <-chan streamPart }
func (r *chanStreamReader) Recv() (*lpmodel.Message, error)
func newChanStreamReader(ch <-chan streamPart) lpmodel.MessageStreamReader

type toolCallAccumulator struct { ... }     // 流式 tool_call delta 累积器
func (a *toolCallAccumulator) addDeltas(deltas []openai.ChatCompletionTokenToolCall)
func (a *toolCallAccumulator) toToolCalls() []lpmodel.ToolCallPart

func runChatCompletionStream(stream *openai.ChatCompletionStream, out chan<- streamPart)
```

**stream 处理逻辑**（与 lark/deepseek 一致）：
1. 逐 chunk 从 `stream.Next()` 接收
2. 文本 delta → 即时投递到 out channel（`RoleAssistant, Content=delta`）
3. tool_call delta → 累积到 `toolCallAccumulator`
4. reasoning_content delta（如果有）→ 即时投递（`ReasoningContent=delta`）
5. 流结束（`stream.Err() == io.EOF`）→ 投递最终消息（含累积的 ToolCalls + Usage）

验证：
- `go vet ./pkg/model/adapters/openai/...` 无报错
- 该包可直接被 runner 装配

### 作业 #3 — 新增 `pkg/runner/openai_default.go`

**目标**：参照 `lark_default.go` 和 `deepseek_default.go` 的模式，实现 OpenAI 的环境变量自动装配。

```go
package runner

// ApplyOpenAIFromConfig sets Agent.ChatModel to OpenAI when it is nil.
func ApplyOpenAIFromConfig(a *agent.Agent, apiKey, baseURL, modelFallback string)
```

环境变量读取顺序：
| 配置 | 环境变量（优先级从高到低） | 默认值 |
|------|--------------------------|--------|
| apiKey | `OPENAI_API_KEY` | 无（为空则不装配） |
| baseURL | `OPENAI_BASE_URL` | `https://api.openai.com/v1` |
| model | `OPENAI_MODEL` → Agent.ModelName | `gpt-4o-mini` |

```go
func applyDefaultOpenAIIfNeeded(a *agent.Agent) {
    ApplyOpenAIFromConfig(a, "", "", "")
}
```

在 `runner.go` 的 `Run()` 方法调用链中，将 `applyDefaultOpenAIIfNeeded` 作为最后兜底装配（放置在 Lark 和 DeepSeek 装配之后）。即：优先专用适配器 → 最后回退到 OpenAI SDK 通用适配器。

### 作业 #4 — `go.mod` / `go.sum` 清理 agent-sdk-go

**目标**：移除 agent-sdk-go 依赖声明

操作：
1. 从 `go.mod` 删除 `github.com/agentizen/agent-sdk-go v0.17.0` 行
2. 运行 `go mod tidy` 清理 `go.sum`

影响：无——`agentsdk/` 包将在下一步被整体删除。

### 作业 #5 — 删除 `pkg/model/adapters/agentsdk/` 包

**目标**：删除 4 个文件

| 文件 | 说明 |
|------|------|
| `pkg/model/adapters/agentsdk/chatmodel.go` | SDKChatModel 实现 |
| `pkg/model/adapters/agentsdk/convert.go` | 消息格式转换（loopForge ↔ agent-sdk-go） |
| `pkg/model/adapters/agentsdk/settings.go` | CallConfig → SDK Settings 映射 |
| `pkg/model/adapters/agentsdk/stream_reader.go` | chanStreamReader |

验证：
- `go vet ./...` 无报错（OpenAI 适配器已先就位）
- 搜索 `adapters/agentsdk` 无残留

### 作业 #6 — 更新 `pkg/model/types/toolinfo.go` 注释

**目标**：去除 agent-sdk-go 的注释引用

修改点：
- 第 12 行 `// ToOpenAITool returns a value suitable for agent-sdk-go Request.Tools ([]interface{} entries).`
  → `// ToOpenAITool returns a value suitable for OpenAI-compatible chat completion API ([]interface{} entries).`

### 作业 #7 — 修订 `doc/design/11-chatmodel.md`

**目标**：将 agent-sdk-go 适配器替换为 OpenAI SDK 适配器说明

具体修改：

| 原内容 | 新内容 |
|--------|--------|
| §1.1 "三个适配器实现（Lark/方舟、DeepSeek、agent-sdk-go）" | "三个适配器实现（Lark/方舟、DeepSeek、OpenAI SDK）" |
| §1.1 架构图 agent-sdk-go | OpenAI SDK |
| §1.2 包结构图 `agentsdk/` | `openai/` |
| §5.3 agent-sdk-go 整节 | **OpenAI SDK** 适配器描述 |
| §5.4 对比表 agent-sdk-go 列 | OpenAI SDK 列 |
| §5.5 agent-sdk-go 的未来 | 移除说明 + OpenAI SDK 替代方案 |
| §9 文件表 agentsdk 行 | openai 行 |
| §7 环境变量装配 | 追加 OpenAI 装配说明 |
| 全文 "agent-sdk-go" 引用 | 更新为 OpenAI SDK |

### 作业 #8 — 修订 `doc/design/00-abstractions.md`

修改点：
- 第 70 行 `agentsdk / deepseek` → `openai / deepseek`
- 第 627 行 `pkg/model/adapters/agentsdk/`（Lark/方舟 SDK）→ `pkg/model/adapters/openai/`（OpenAI SDK）

### 作业 #9 — 修订 `doc/decision/sdk-selection.md`

修改点：
- 标题：体现"曾选 agent-sdk-go 做模型适配层，v0.9.6 已完全移除"
- §1 术语表：agent-sdk-go 标注"v0.9.6 已完全移除，由 OpenAI SDK 替代"
- §2：更新现状描述—agent-sdk-go 已移除，兜底适配器改为 OpenAI SDK
- §6 最终决策：第 1 条模型适配改为 OpenAI SDK
- §8：移除许可证审计说明
- §9 变更记录：添加 v0.9.6 条目

### 作业 #10 — 修订 `README.md`（根目录）

修改点：
- 第 16 行 "基于 `agent-sdk-go` 构建" → "基于 OpenAI 兼容 API 构建"
- 第 107 行 技术栈表 "默认 Agent 循环与工具（非必要）" → 更新为 "模型调用：OpenAI SDK（兜底）、火山引擎 Ark（volcengine-go-sdk）、DeepSeek（deepseek-go）"

### 作业 #11 — 创建 `doc/log/progress-v0.9.6.md`

**目标**：版本进度日志，记录 v0.9.6 的交付清单

### 作业 #12 — 最终验证

- `go build ./...` 编译通过
- `go vet ./...` 无问题
- 搜索 `github.com/agentizen` 在非文档 Go 文件中无残留
- 搜索 `pkg/model/adapters/agentsdk` 无残留
- 搜索 `openai-adapter` 相关新代码编译正确
- 所有修订文档交叉引用一致

---

## 3. 执行顺序示意图

```
作业 #1  go.mod 新增 openai-go         → 你确认 → 我实施
作业 #2  创建 openai/ 适配器包(3文件)    → 你确认 → 我实施
作业 #3  新增 runner/openai_default.go   → 你确认 → 我实施
作业 #4  go.mod 清理 agent-sdk-go       → 你确认 → 我实施
作业 #5  删除 agentsdk/ 包              → 你确认 → 我实施
作业 #6  toolinfo.go 注释更新           → 你确认 → 我实施
作业 #7  11-chatmodel.md 修订           → 你确认 → 我实施
作业 #8  00-abstractions.md 修订        → 你确认 → 我实施
作业 #9  sdk-selection.md 修订          → 你确认 → 我实施
作业 #10 README.md 修订                 → 你确认 → 我实施
作业 #11 progress-v0.9.6.md 创建        → 你确认 → 我实施
作业 #12 最终验证                       → 你确认 → 我实施
```

---

## 4. 后置依赖 / 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| `openai-go` SDK API 与预期不一致 | 包结构或类型名可能不同 | 编写前先 `go get` 并检查 godoc；以实际发布的 API 为准调整 |
| `openai-go` 流式接口的 tool_call delta 类型差异 | 与 lark/deepseek 的`toolCallAccumulator` 需适配 | `addDeltas` 接受 `openai.ChatCompletionTokenToolCall` 类型 |
| 删除 agentsdk 包后 `toopenai.Tool` 引用中断 | 无——`toolinfo.go` 的 `ToOpenAITool` 仅被 agentsdk 内部调用 | 确认删除后 `go vet` 通过 |
| agent-sdk-go 的间接依赖（如 `ollama`、`segmentio`）被带入 | go.sum 体积增大 | `go mod tidy` 在删除 agent-sdk-go 后自动清理不再需要的间接依赖 |
| 装配顺序：OpenAI 兜底优先级 | OpenAI SDK 不应覆盖 Lark/DeepSeek 的专用装配 | `applyDefaultOpenAIIfNeeded` 必须在 Lark 和 DeepSeek 之后调用 |

---

## 5. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-30 | 初稿：v0.9.6 全量移除 agent-sdk-go 实施计划。 |

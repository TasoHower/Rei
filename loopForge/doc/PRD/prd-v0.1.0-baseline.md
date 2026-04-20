# loopForge 产品需求文档 — v0.1.0 可运行基座

> **版本**：v0.1.0  
> **日期**：2026-04-15  
> **里程碑**：可运行基座 — agent-sdk-go 接入、`pkg/model` 抽象、方舟（Doubao）流式对话 demo 跑通  
> **状态**：已完成  
> **关联文档**：[progress-v0.1.0.md](../log/progress-v0.1.0.md)、[progress-v0.0.1.md](../log/progress-v0.0.1.md)、[sdk-selection.md](../decision/sdk-selection.md)

---

## 1. 概述

### 1.1 产品定位

**v0.1.0** 是 loopForge 的**第一个可运行版本**，在 v0.0.1 文档与 sdk-selection 决策下，落地最小可运行代码：依赖 `agent-sdk-go`，自建与 Eino 风格对齐的 ChatModel 抽象，提供火山方舟 OpenAI 兼容路径上的流式对话验证。

### 1.2 核心价值

1. **可运行** — 最小可运行代码，验证核心架构
2. **模型抽象** — ChatModel 抽象，支持多种后端（Mock / Ark / Doubao）
3. **流式对话** — 支持流式输出，为后续事件驱动奠定基础
4. **工具支持** — ToolInfo 抽象，支持 function call

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.0.1** | 设计文档基线；v0.1.0 实现设计文档中的核心概念 |
| **v0.2.0** | 流式事件驱动；v0.1.0 的流式对话能力在 v0.2.0 中升级为完整的事件驱动架构 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.1.0）

#### P0（必须实现）

1. **依赖管理**
   - `go.mod` / `go.sum` 固定 `agent-sdk-go` 等依赖
   - 模块名 `loopforge`

2. **`pkg/model` 抽象**
   - `Message`、`ToolInfo`、`CallOption`、`BaseChatModel` / `ToolCallingChatModel`、`MessageStreamReader`
   - 与 Eino 风格对齐的 ChatModel 抽象

3. **`pkg/model/adapters/agentsdk/`**
   - 基于 agent-sdk-go `Provider`/`Model` 的 `ChatModel` 实现
   - 含增量流式转发

4. **`pkg/model/adapters/doubao/`**
   - 方舟 `ArkChatModel`
   - `agentsdk.SDKChatModel` 仅作模型 HTTP 能力

5. **`pkg/runtime/*`**
   - 原 `pkg/rt`：请求 / 结果 / 事件 / 交换 / 工具契约
   - `DonePayload` 携带完整 `RuntimeOutcome`

6. **`pkg/agent`**
   - `Agent`、`Planner`、`RunnerAgent`
   - `pkg/model` 工具循环，不依赖 SDK `Agent`/`Runner`

7. **`internal/engine`**
   - `Engine` 嵌入 `agent.Agent`
   - `AgentRunner.RunLoop` 返回 `<-chan event.RuntimeEvent` 流式输出（签名预留）

8. **Demo**
   - `cmd/doubaodemo` — 方舟流式：`pkg/model` 上 `Stream` + `Recv`
   - `cmd/loopforged` — Mock / Ark runner demo（`LOOPFORGE_USE_MOCK` 等）
   - `cmd/agentdemo` — `RunnerAgent` + 豆包联调

9. **启动脚本**
   - `start.sh` — 本地启动示例（**不含密钥**，需事先 export API key）

#### P1（可选增强）

- 文档完善（README、示例）
- 性能优化

### 2.2 不在本版本范围

1. **全量引擎实现** — 与 `doc/design` 全量对齐的实现留待后续版本
2. **单测与 CI** — 测试基础设施留待后续版本
3. **AgentRunner 实现** — 当前仅有接口与类型

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够使用 ChatModel 抽象  
**验收标准**：
- 创建 `ToolCallingChatModel` 实例
- 调用 `Stream()` 方法流式输出
- 调用 `Recv()` 方法接收响应

**US-2**：能够使用 Mock 模式开发  
**验收标准**：
- 设置 `LOOPFORGE_USE_MOCK=true`
- 使用 Mock ChatModel
- 无需真实 API key

**US-3**：能够使用火山方舟 API  
**验收标准**：
- 设置 `ARK_API_KEY` 环境变量
- 使用 `ArkChatModel`
- 流式对话正常

**US-4**：能够运行 Demo 验证功能  
**验收标准**：
- 运行 `cmd/doubaodemo` 验证方舟流式
- 运行 `cmd/loopforged` 验证 Mock/Ark 模式
- 运行 `cmd/agentdemo` 验证 `RunnerAgent`

**US-5**：能够定义工具（ToolInfo）  
**验收标准**：
- 创建 `ToolInfo` 结构
- 定义 `Name`、`Description`、`Parameters`、`Handle`
- 工具可在对话中调用

### 3.2 作为 Agent，我希望...

**US-6**：能够流式输出对话  
**验收标准**：
- 调用 `ChatModel.Stream()` 流式输出
- 使用 `MessageStreamReader` 消费流
- 实时输出 token

**US-7**：能够调用工具  
**验收标准**：
- LLM 返回 tool call
- 调用 `ToolInfo.Handle` 执行工具
- 返回工具结果给 LLM

---

## 4. 功能需求

### 4.1 ChatModel 抽象

#### 4.1.1 接口定义

```go
// ChatModel is the interface for chat models.
type ChatModel interface {
    // Stream starts a streaming chat.
    Stream(ctx context.Context, messages []Message, opts ...CallOption) (*MessageStreamReader, error)
}

// ToolCallingChatModel extends ChatModel with tool calling support.
type ToolCallingChatModel interface {
    ChatModel
    
    // WithTools sets the tools for the model.
    WithTools(tools []*ToolInfo) ToolCallingChatModel
}
```

#### 4.1.2 Message

```go
type Message struct {
    Role    string `json:"role"`    // "system", "user", "assistant", "tool"
    Content string `json:"content"` // 消息内容
    Name    string `json:"name,omitempty"`    // 可选：角色名
    // ...
}
```

#### 4.1.3 ToolInfo

```go
type ToolInfo struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters"` // JSON Schema
    Handle      ToolCallHandler        `json:"-"`          // 工具执行函数
}

type ToolCallHandler func(ctx context.Context, argumentsJSON string) (string, error)
```

### 4.2 Adapters

#### 4.2.1 agentsdk Adapter

```go
// Package agentsdk provides a ChatModel implementation based on agent-sdk-go.
package agentsdk

import "github.com/agentizen/agent-sdk-go"

// SDKChatModel implements ChatModel using agent-sdk-go.
type SDKChatModel struct {
    provider *agent_sdk.Provider
    model    *agent_sdk.Model
}

// NewSDKChatModel creates a new SDKChatModel.
func NewSDKChatModel(provider *agent_sdk.Provider, modelName string) *SDKChatModel {
    model := provider.GetModel(modelName)
    return &SDKChatModel{
        provider: provider,
        model:    model,
    }
}

// Stream implements ChatModel.
func (c *SDKChatModel) Stream(ctx context.Context, messages []model.Message, opts ...model.CallOption) (*model.MessageStreamReader, error) {
    // 转换为 agent-sdk-go 格式
    sdkMessages := toSDKMessages(messages)
    
    // 调用 SDK
    stream, err := c.model.Stream(ctx, sdkMessages)
    if err != nil {
        return nil, err
    }
    
    // 包装为 MessageStreamReader
    return wrapStreamReader(stream), nil
}
```

#### 4.2.2 doubao Adapter

```go
// Package doubao provides a ChatModel implementation for Volcano Ark (Doubao).
package doubao

import "loopforge/pkg/model/adapters/agentsdk"

// ArkChatModel implements ChatModel for Volcano Ark.
type ArkChatModel struct {
    *agentsdk.SDKChatModel
}

// NewArkChatModel creates a new ArkChatModel.
func NewArkChatModel(apiKey, modelName string) *ArkChatModel {
    provider := agentsdk.NewProvider(apiKey)
    sdkModel := NewSDKChatModel(provider, modelName)
    
    return &ArkChatModel{
        SDKChatModel: sdkModel,
    }
}
```

### 4.3 RunnerAgent

#### 4.3.1 结构

```go
type RunnerAgent struct {
    Name               string
    ModelName          string
    SystemInstructions string
    ChatModel          ToolCallingChatModel
    ToolInfos          []*ToolInfo
    Executor           ToolExecutor
    MaxSteps           int
}
```

#### 4.3.2 Run 方法

```go
func (a *RunnerAgent) Run(ctx context.Context, req *Request) (*RuntimeOutcome, error) {
    // 构建消息
    messages := buildMessages(req)
    
    // 绑定模型
    chatModel := bindModel(a.ChatModel, a.ToolInfos)
    
    // 执行循环
    outcome, err := a.runLoop(ctx, messages, chatModel)
    if err != nil {
        return nil, err
    }
    
    return outcome, nil
}
```

#### 4.3.3 runLoop

```go
func (a *RunnerAgent) runLoop(ctx context.Context, messages []Message, chatModel ToolCallingChatModel) (*RuntimeOutcome, error) {
    step := 0
    
    for step < a.MaxSteps {
        // 流式调用
        stream, err := chatModel.Stream(ctx, messages)
        if err != nil {
            return nil, err
        }
        
        // 消费流
        response, toolCalls, err := consumeStream(stream)
        if err != nil {
            return nil, err
        }
        
        // 添加到消息历史
        messages = append(messages, response)
        
        // 无工具调用，结束
        if len(toolCalls) == 0 {
            break
        }
        
        // 执行工具
        for _, tc := range toolCalls {
            result, err := executeToolCall(ctx, tc, a.Executor)
            if err != nil {
                return nil, err
            }
            
            // 添加工具结果
            messages = append(messages, toolResultMessage(tc, result))
        }
        
        step++
    }
    
    return &RuntimeOutcome{
        Messages: messages,
        // ...
    }, nil
}
```

### 4.4 Demo

#### 4.4.1 doubaodemo

```go
// cmd/doubaodemo/main.go
func main() {
    // 创建 ArkChatModel
    apiKey := os.Getenv("ARK_API_KEY")
    model := doubao.NewArkChatModel(apiKey, "doubao-pro-4k")
    
    // 流式对话
    messages := []model.Message{
        {Role: "user", Content: "你好"},
    }
    
    stream, err := model.Stream(context.Background(), messages)
    if err != nil {
        log.Fatal(err)
    }
    
    // 消费流
    for stream.Recv() {
        fmt.Print(stream.Text())
    }
}
```

#### 4.4.2 loopforged

```go
// cmd/loopforged/main.go
func main() {
    useMock := os.Getenv("LOOPFORGE_USE_MOCK") == "true"
    
    var chatModel model.ToolCallingChatModel
    if useMock {
        chatModel = mock.NewChatModel()
    } else {
        apiKey := os.Getenv("ARK_API_KEY")
        chatModel = doubao.NewArkChatModel(apiKey, "doubao-pro-4k")
    }
    
    // 创建 RunnerAgent
    agent := agent.NewRunnerAgent(chatModel,
        agent.WithName("demo"),
        agent.WithSystemInstructions("你是助手"),
    )
    
    // 运行
    req := &agent.Request{
        Messages: []model.Message{
            {Role: "user", Content: "你好"},
        },
    }
    
    outcome, err := agent.Run(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println(outcome.Messages[len(outcome.Messages)-1].Content)
}
```

#### 4.4.3 agentdemo

```go
// cmd/agentdemo/main.go
func main() {
    // 创建工具
    tools := []*model.ToolInfo{
        {
            Name:        "get_weather",
            Description: "Get weather for a city",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "city": map[string]any{"type": "string"},
                },
                "required": []string{"city"},
            },
            Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
                var args struct {
                    City string `json:"city"`
                }
                json.Unmarshal([]byte(argumentsJSON), &args)
                return fmt.Sprintf("Weather in %s: Sunny", args.City), nil
            },
        },
    }
    
    // 创建 ArkChatModel
    apiKey := os.Getenv("ARK_API_KEY")
    chatModel := doubao.NewArkChatModel(apiKey, "doubao-pro-4k")
    chatModel = chatModel.WithTools(tools)
    
    // 创建 RunnerAgent
    agent := agent.NewRunnerAgent(chatModel,
        agent.WithName("weather"),
        agent.WithSystemInstructions("你是天气助手"),
    )
    
    // 运行
    req := &agent.Request{
        Messages: []model.Message{
            {Role: "user", Content: "北京天气如何？"},
        },
    }
    
    outcome, err := agent.Run(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println(outcome.Messages[len(outcome.Messages)-1].Content)
}
```

---

## 5. 技术需求

### 5.1 依赖管理

```go
// go.mod
module loopforge

go 1.21

require (
    github.com/agentizen/agent-sdk-go v0.1.0
    // ...
)
```

### 5.2 项目结构

```
loopforge/
├── cmd/
│   ├── doubaodemo/       # 方舟流式 demo
│   ├── loopforged/       # Mock / Ark runner demo
│   └── agentdemo/        # RunnerAgent + 豆包联调
├── pkg/
│   ├── model/            # ChatModel 抽象
│   │   ├── adapters/
│   │   │   ├── agentsdk/ # agent-sdk-go adapter
│   │   │   └── doubao/   # Ark adapter
│   │   ├── types/        # 数据类型
│   │   └── interface/    # 接口定义
│   ├── runtime/          # 请求 / 结果 / 事件 / 交换 / 工具契约
│   └── agent/            # Agent、Planner、RunnerAgent
└── internal/
    └── engine/           # Engine 嵌入 agent.Agent
```

### 5.3 环境变量

```bash
# 方舟 API Key
export ARK_API_KEY="your-api-key"

# 使用 Mock 模式
export LOOPFORGE_USE_MOCK="true"
```

### 5.4 启动脚本

```bash
#!/bin/bash
# start.sh

# 检查 API Key
if [ -z "$ARK_API_KEY" ]; then
    echo "Error: ARK_API_KEY is not set"
    echo "Please export ARK_API_KEY=your-api-key"
    exit 1
fi

# 运行 demo
go run cmd/agentdemo/main.go
```

---

## 6. 验收标准

### 6.1 功能验收

- [ ] ChatModel 抽象可正常工作
- [ ] agentsdk adapter 可调用 agent-sdk-go
- [ ] doubao adapter 可调用火山方舟 API
- [ ] RunnerAgent 可运行对话
- [ ] 工具调用可正常工作
- [ ] Demo 可运行

### 6.2 性能验收

- [ ] 流式输出延迟 < 500ms
- [ ] 工具调用延迟 < 1s
- [ ] 内存占用合理

### 6.3 兼容性验收

- [ ] Mock 模式可正常工作
- [ ] Ark 模式可正常工作
- [ ] 环境变量配置正确

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **API Key 泄漏** | API Key 可能误提交到仓库 | 环境变量、start.sh 去密钥化 |
| **依赖不稳定** | agent-sdk-go 可能不稳定 | 固定版本、监控更新 |
| **模型兼容性** | 不同模型 API 可能不兼容 | 抽象层适配、充分测试 |
| **错误处理** | 网络错误、API 错误 | 重试机制、错误日志 |

---

## 8. 参考文档

- [progress-v0.1.0.md](../log/progress-v0.1.0.md) — 版本进度日志
- [progress-v0.0.1.md](../log/progress-v0.0.1.md) — 设计文档基线
- [sdk-selection.md](../decision/sdk-selection.md) — SDK 选型决策
- [agent-sdk-go](https://github.com/agentizen/agent-sdk-go) — Agent SDK

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-15 | v0.1.0 | 初始版本 |

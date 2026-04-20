# loopForge 产品需求文档 — v0.2.0 流式事件驱动

> **版本**：v0.2.0  
> **日期**：2026-04-15  
> **里程碑**：流式事件驱动 — Agent.Run 返回 `<-chan *RuntimeEvent`，类型安全 Payload，多工具链式调用 demo  
> **状态**：已完成  
> **关联文档**：[progress-v0.2.0.md](../log/progress-v0.2.0.md)、[progress-v0.1.0.md](../log/progress-v0.1.0.md)

---

## 1. 概述

### 1.1 产品定位

**v0.2.0** 是 loopForge 的**架构升级版本**，将 Agent 从**同步请求 - 响应**模型升级为**流式事件驱动**模型。通过返回 channel 和类型安全的 Payload，实现细粒度的过程观测和实时控制，为多 Agent 协作和复杂编排奠定基础。

### 1.2 核心价值

1. **流式输出** — 所有中间过程（token、tool 调用、step）和最终结果（done）均通过事件流实时输出
2. **类型安全** — 消除 `Payload any`，通过 sealed interface 实现编译期类型安全
3. **细粒度观测** — 可观测每个 step、每次 tool 调用的详细过程
4. **链式调用** — 支持多工具链式调用，覆盖复杂场景

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.1.0** | 可运行基座；v0.2.0 在此基础上升级为流式事件驱动 |
| **v0.3.0** | Agent Transfer；v0.2.0 的事件驱动架构为 v0.3.0 的 Transfer 能力奠定基础 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.2.0）

#### P0（必须实现）

1. **`Agent.Run` 流式化**
   - 签名从 `(*outcome.RuntimeOutcome, error)` 改为 `<-chan *event.RuntimeEvent`
   - 错误通过 `RuntimeEventError` 事件传递，不再通过 Go error 返回

2. **`EventPayload` sealed interface**
   - 替代 `Payload any`
   - 私有 `eventPayload()` 方法形成 sealed 约束
   - 外部包无法实现

3. **`Emit` 构造器**
   - 从 payload 自动推导 `Type`
   - 消除手动拼装的冗余与不一致风险

4. **类型安全访问器**
   - `RuntimeEvent` 上新增 `.Token()` / `.ToolStart()` / `.ToolEnd()` / `.Done()` / `.Error()` / `.StepEvent()` 方法
   - 调用方无需手写类型断言

5. **Payload 字段增强**
   - `ToolStartPayload` 新增 `Arguments` 字段
   - `ToolEndPayload` 新增 `Output` 和 `IsError` 字段
   - `StepPayload` 新增具体类型

6. **Agent 配置**
   - `RunnerAgent.ModelName` 新增字段 + `WithModelName` option
   - `modelName` 取值优先级：`a.ModelName` → `req.Options.Model` 覆盖

7. **Demo**
   - 多工具链式调用 demo：四个算术工具 `add` / `subtract` / `multiply` / `divide`
   - 默认 prompt：需要跨 3 步链式调用（add → multiply → subtract）
   - 事件流可视化输出

#### P1（可选增强）

- Spawn 能力（父子递归子任务）
- 工具权限控制（whitelist / blacklist）

### 2.2 不在本版本范围

1. **Transfer 能力** — 多 Agent 平级移交留待 v0.3.0
2. **Skill 系统** — 技能包机制留待后续版本
3. **Memory** — 长短期记忆机制留待后续版本

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够消费 Agent 运行的事件流  
**验收标准**：
- 调用 `agent.Run(ctx, req)` 返回 `<-chan *RuntimeEvent`
- 使用 `for event := range eventChan` 消费事件
- 事件包含 token、tool 调用、step、done 等所有中间过程

**US-2**：能够类型安全地访问事件 Payload  
**验收标准**：
- 使用 `.Token()` / `.ToolStart()` / `.ToolEnd()` / `.Done()` / `.Error()` 访问器
- 无需手写类型断言
- 编译期类型检查

**US-3**：能够看到工具调用的详细过程  
**验收标准**：
- `ToolStartPayload` 包含 `Arguments`（模型给出的原始 JSON 入参）
- `ToolEndPayload` 包含 `Output`（工具返回值）和 `IsError`（是否报错）
- 可观测工具调用的完整生命周期

**US-4**：能够运行多工具链式调用 demo  
**验收标准**：
- 四个算术工具 `add` / `subtract` / `multiply` / `divide`
- 默认 prompt 需要跨 3 步链式调用
- 事件流可视化输出清晰

**US-5**：能够指定模型名称  
**验收标准**：
- 通过 `WithModelName("model-name")` 指定模型
- `modelName` 取值优先级正确
- Metrics 中正确显示模型名

### 3.2 作为 Agent，我希望...

**US-6**：能够流式输出执行过程  
**验收标准**：
- 每步执行都发射对应事件
- token 输出实时发射
- tool 调用发射 `ToolStart` / `ToolEnd` 事件
- 执行结束发射 `Done` 事件

**US-7**：能够正确处理错误  
**验收标准**：
- 错误通过 `RuntimeEventError` 事件传递
- 不中断事件流
- 消费方可通过 `.Error()` 访问器获取错误

---

## 4. 功能需求

### 4.1 流式化 API

#### 4.1.1 Agent.Run 签名变更

```go
// 旧签名（v0.1.0）：
func (a *RunnerAgent) Run(ctx context.Context, req *Request) (*RuntimeOutcome, error)

// 新签名（v0.2.0）：
func (a *RunnerAgent) Run(ctx context.Context, req *Request) (<-chan *event.RuntimeEvent, error)
```

#### 4.1.2 错误处理

```go
// 错误通过 RuntimeEventError 事件传递
eventChan <- &RuntimeEvent{
    Type: EventError,
    Payload: &RuntimeEventError{
        Err: err.Error(),
    },
}
```

### 4.2 类型安全 Payload

#### 4.2.1 EventPayload sealed interface

```go
// EventPayload is a sealed interface for all event payloads.
// External packages cannot implement this interface.
type EventPayload interface {
    eventPayload() // private marker method
}

// TokenPayload implements EventPayload.
type TokenPayload struct {
    Text string
}

func (p *TokenPayload) eventPayload() {}

// ToolStartPayload implements EventPayload.
type ToolStartPayload struct {
    Name      string
    Arguments string  // 新增字段
}

func (p *ToolStartPayload) eventPayload() {}

// ToolEndPayload implements EventPayload.
type ToolEndPayload struct {
    Output  string  // 新增字段
    IsError bool    // 新增字段
}

func (p *ToolEndPayload) eventPayload() {}

// DonePayload implements EventPayload.
type DonePayload struct {
    Outcome *RuntimeOutcome
}

func (p *DonePayload) eventPayload() {}

// StepPayload implements EventPayload.
type StepPayload struct {
    StepNumber int
}

func (p *StepPayload) eventPayload() {}
```

#### 4.2.2 Emit 构造器

```go
// Emit creates a RuntimeEvent from a payload, automatically deriving the Type.
func Emit(payload EventPayload) *RuntimeEvent {
    return &RuntimeEvent{
        Type:    deriveType(payload),
        Payload: payload,
    }
}

func deriveType(payload EventPayload) EventType {
    switch payload.(type) {
    case *TokenPayload:
        return EventToken
    case *ToolStartPayload:
        return EventToolStart
    case *ToolEndPayload:
        return EventToolEnd
    case *DonePayload:
        return EventDone
    case *StepPayload:
        return EventStep
    default:
        return EventUnknown
    }
}
```

#### 4.2.3 类型安全访问器

```go
// Token returns the TokenPayload if the event is a token event.
func (e *RuntimeEvent) Token() (*TokenPayload, bool) {
    p, ok := e.Payload.(*TokenPayload)
    return p, ok
}

// ToolStart returns the ToolStartPayload if the event is a tool start event.
func (e *RuntimeEvent) ToolStart() (*ToolStartPayload, bool) {
    p, ok := e.Payload.(*ToolStartPayload)
    return p, ok
}

// ToolEnd returns the ToolEndPayload if the event is a tool end event.
func (e *RuntimeEvent) ToolEnd() (*ToolEndPayload, bool) {
    p, ok := e.Payload.(*ToolEndPayload)
    return p, ok
}

// Done returns the DonePayload if the event is a done event.
func (e *RuntimeEvent) Done() (*DonePayload, bool) {
    p, ok := e.Payload.(*DonePayload)
    return p, ok
}

// Error returns the RuntimeEventError if the event is an error event.
func (e *RuntimeEvent) Error() (*RuntimeEventError, bool) {
    p, ok := e.Payload.(*RuntimeEventError)
    return p, ok
}

// StepEvent returns the StepPayload if the event is a step event.
func (e *RuntimeEvent) StepEvent() (*StepPayload, bool) {
    p, ok := e.Payload.(*StepPayload)
    return p, ok
}
```

### 4.3 多工具链式调用 Demo

#### 4.3.1 工具定义

```go
tools := []*model.ToolInfo{
    {
        Name:        "add",
        Description: "Add two numbers",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "a": map[string]any{"type": "number"},
                "b": map[string]any{"type": "number"},
            },
            "required": []string{"a", "b"},
        },
        Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
            var args struct {
                A float64 `json:"a"`
                B float64 `json:"b"`
            }
            json.Unmarshal([]byte(argumentsJSON), &args)
            result := args.A + args.B
            return fmt.Sprintf("%.2f", result), nil
        },
    },
    // subtract, multiply, divide 类似定义
}
```

#### 4.3.2 默认 Prompt

```text
你是一个数学助手。请完成以下计算：

(5 + 3) * 2 - 4 = ?

请使用提供的工具逐步计算。
```

**预期执行流程**：
1. Step 1: `add(5, 3)` → 8
2. Step 2: `multiply(8, 2)` → 16
3. Step 3: `subtract(16, 4)` → 12

#### 4.3.3 事件流可视化输出

```text
── step 1 ──
→ Tool call: add({"a": 5, "b": 3})
← Tool result: 8.00

── step 2 ──
→ Tool call: multiply({"a": 8, "b": 2})
← Tool result: 16.00

── step 3 ──
→ Tool call: subtract({"a": 16, "b": 4})
← Tool result: 12.00

✓ Done: 12.00
```

### 4.4 Agent 配置

#### 4.4.1 ModelName 字段

```go
type RunnerAgent struct {
    Name               string
    ModelName          string  // 新增：模型标识
    SystemInstructions string
    // ...
}
```

#### 4.4.2 WithModelName Option

```go
// WithModelName sets the model name for the agent.
func WithModelName(name string) Option {
    return func(a *RunnerAgent) {
        a.ModelName = name
    }
}
```

#### 4.4.3 取值优先级

```go
// modelName 取值优先级：
// 1. a.ModelName（Agent 配置）
// 2. req.Options.Model（请求时覆盖）

modelName := a.ModelName
if req.Options.Model != "" {
    modelName = req.Options.Model
}
```

---

## 5. 技术需求

### 5.1 数据结构

#### 5.1.1 RuntimeEvent

```go
type RuntimeEvent struct {
    Type    EventType
    Payload EventPayload
}

type EventType string

const (
    EventToken       EventType = "token"
    EventToolStart   EventType = "tool_start"
    EventToolEnd     EventType = "tool_end"
    EventDone        EventType = "done"
    EventError       EventType = "error"
    EventStep        EventType = "step"
    EventSpawnStart  EventType = "spawn_start"
    EventSpawnEnd    EventType = "spawn_end"
    EventUnknown     EventType = "unknown"
)
```

#### 5.1.2 RunMetrics

```go
type RunMetrics struct {
    Model       string  // 新增：模型标识
    TotalTokens int     // 新增：总 token
    Steps       int     // 新增：执行步数
    // ...
}
```

### 5.2 并发模型

```go
func (a *RunnerAgent) Run(ctx context.Context, req *Request) (<-chan *event.RuntimeEvent, error) {
    eventChan := make(chan *event.RuntimeEvent, 64)
    
    go func() {
        defer close(eventChan)
        
        // runLoop 内部发射事件
        err := a.runLoop(ctx, req, event_chan)
        if err != nil {
            event_chan <- &RuntimeEvent{
                Type: EventError,
                Payload: &RuntimeEventError{
                    Err: err.Error(),
                },
            }
        }
    }()
    
    return event_chan, nil
}
```

### 5.3 错误处理

#### 5.3.1 RuntimeEventError

```go
type RuntimeEventError struct {
    Err string
}

func (e *RuntimeEventError) eventPayload() {}
```

### 5.4 观测性

#### 5.4.1 日志

```go
// Agent started
slog.Info("Agent started", "name", a.Name, "model", modelName)

// Step completed
slog.Debug("Step completed", "step", stepNumber)

// Tool called
slog.Debug("Tool called", "name", toolName, "arguments", argumentsJSON)

// Agent done
slog.Info("Agent done", "name", a.Name, "steps", metrics.Steps)
```

#### 5.4.2 事件

- `EventToken` — token 输出
- `EventToolStart` — tool 调用开始
- `EventToolEnd` — tool 调用结束
- `EventDone` — 执行完成
- `EventError` — 错误
- `EventStep` — step 完成

---

## 6. 验收标准

### 6.1 功能验收

- [ ] `Agent.Run` 返回 channel
- [ ] 事件流包含所有中间过程
- [ ] Payload 类型安全
- [ ] 访问器无需类型断言
- [ ] 多工具链式调用 demo 运行正常
- [ ] ModelName 正确显示在 Metrics 中

### 6.2 性能验收

- [ ] 事件发射延迟 < 1ms
- [ ] channel 缓冲大小合理（64）
- [ ] 内存占用 < 1KB/事件

### 6.3 测试验收

- [ ] 单测适配流式 channel 消费
- [ ] 验证 `DonePayload`、`ToolStart`、`ToolEnd` 出现在事件流中
- [ ] Demo 运行正常

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **channel 泄漏** | goroutine 未正确关闭导致 channel 泄漏 | defer close、充分测试 |
| **事件丢失** | channel 缓冲满导致事件丢失 | 合理设置缓冲大小、监控 |
| **类型断言失败** | 访问器类型断言失败 | 返回 ok 标志、文档说明 |
| **错误处理复杂** | 错误通过事件传递，消费方需特殊处理 | 提供 `.Error()` 访问器、示例代码 |

---

## 8. 参考文档

- [progress-v0.2.0.md](../log/progress-v0.2.0.md) — 版本进度日志
- [progress-v0.1.0.md](../log/progress-v0.1.0.md) — 前置版本
- [Reactive Streams](http://www.reactive-streams.org/) — 流式处理参考

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-15 | v0.2.0 | 初始版本 |

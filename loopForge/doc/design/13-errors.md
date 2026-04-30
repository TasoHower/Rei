# loopForge Errors（错误体系）

> 本文档描述 loopForge 的错误体系——哨兵错误（sentinel errors）、结构化错误类型，以及 SDK 调用方如何通过 `errors.Is` / `errors.As` 判定具体的失败条件。
>
> **设计原则**：引擎在关键路径上返回具名哨兵错误，而不是随意构造错误字符串。调用方无须解析消息文本即可通过 `errors.Is` 判断错误类别。

---

## 1. 概念

### 1.1 哨兵错误

`pkg/errors/errors.go` 中定义的所有哨兵错误由 `errors.New(...)` 创建，以全局变量的形式导出。调用方使用 `errors.Is(err, lferrors.ErrXxx)` 进行判定：

```go
if errors.Is(err, lferrors.ErrInvalidConfig) {
    // Agent 配置有误
}
if errors.Is(err, lferrors.ErrReadOnly) {
    // LLM 试图写入 const_ 前缀的只读变量
}
```

### 1.2 结构化错误

除哨兵外，部分场景需要携带额外上下文：

| 类型 | 携带信息 | 判定方式 |
|------|---------|---------|
| `*SetupError` | Code + Msg | `errors.Is(err, ErrInvalidConfig)` 或 `errors.Is(err, ErrToolBind)` |
| `*ToolError` | Name + Cause | `errors.Is(err, ErrToolExec)` 或 `errors.As(err, &te)` 提取工具名 |

### 1.3 错误到事件的映射

Agent loop 中，不可恢复的错误通过 `emitError` 转为事件流中的 `error` 事件：

```go
// loop.go L52-L63
emitError := func(code, msg string, step int) {
    emit(step, &event.ErrorPayload{Code: code, Message: msg})
    oc := &outcome.RuntimeOutcome{
        RunID:       runID,
        Termination: outcome.TerminationError,
    }
    // ... emit(query_end)
}
```

`error` 事件后紧跟着 `query_end(termination=error)`，然后 channel 关闭。

---

## 2. 哨兵错误清单

### 2.1 配置/初始化错误

| 哨兵 | 触发场景 | 关联代码 |
|------|---------|---------|
| `ErrInvalidConfig` | Agent 或 Runner 配置有误（ChatModel 为 nil、工具绑定校验失败等），也是 `SetupError` 的默认 Unwrap | `loop.go` emitError("invalid_config") |
| `ErrToolBind` | `WithTools` 在 pre-loop 阶段失败（如适配器拒绝工具 schema） | `setup.go` / `bindModel` |
| `ErrInvalidRequest` | 传入 nil 或格式错误的 `RuntimeRequest` | `loop.go` emitError("invalid_request") |

### 2.2 运行时执行错误

| 哨兵 | 触发场景 | 关联代码 |
|------|---------|---------|
| `ErrGenerate` | LLM 调用（Generate / Stream）失败——网络错误、API 限流、无效模型名等 | `loop.go` emitError("generate") |
| `ErrToolExec` | 工具执行出错（`executeToolCalls` 中 `tool.Invoke` 返回非 nil 错误）；`*ToolError` 的 Unwrap 可以到达此哨兵 | `tools.go` emitError("tool_exec") |
| `ErrMaxTransfers` | Runner transfer 循环超过 `maxTransfers` 上限 | `runner.go` emitError("max_transfers") |
| `ErrInvalidTransfer` | LLM 尝试 transfer 到一个不在 handoffs 列表中的目标 | `runner.go` emitError("invalid_transfer") |
| `ErrStreamEmpty` | 流式调用结束，但 LLM 未产生任何文本或响应对象 | `stream.go` |

### 2.3 工具层错误

| 哨兵 | 触发场景 | 关联代码 |
|------|---------|---------|
| `ErrNoHandler` | `tool.Invoke` 无法找到任何可用的 handler（无 ToolInfo.Handle、无 ToolCallPart.Handle、无 ToolExecutor） | `dispatch.go` |
| `ErrUnknownTool` | `MapToolExecutor.Execute` 被调用时传入的工具名未注册 | `map.go` |
| `ErrExecutorNil` | `MapToolExecutor.Execute` 被调用时 executor 本身为 nil | `map.go` |
| `ErrUnsupportedRole` | 消息适配层遇到不支持的 Role 值 | `convert.go` |

### 2.4 变量层错误

| 哨兵 | 触发场景 | 关联代码 |
|------|---------|---------|
| `ErrReadOnly` | LLM 通过 `var_set` 尝试写入 `const_` 前缀的只读键 | `store.go` AgentSet |
| `ErrNotPresent` | 请求的变量键不存在或值类型不匹配 | `store.go` GetRequired |
| `ErrNoStore` | `var_set` 工具 handler 找不到 VarStore（闭包和 context 中都无可用 store） | `tools.go` var_set Handle |

### 2.5 调试层错误

| 哨兵 | 触发场景 | 关联代码 |
|------|---------|---------|
| `ErrDebugNilConnection` | `debug.Conn` 方法在 nil 连接上被调用 | `debug/dial.go` |

---

## 3. 结构化错误类型

### 3.1 SetupError

`SetupError` 在 `bindModel` 阶段发射——此时 RunLoop 尚未进入迭代，错误直接结束：

```go
// pkg/errors/errors.go
type SetupError struct {
	Code string  // "invalid_config" | "tool_bind"
	Msg string   // 可读的错误描述
	err error    // 底层的哨兵（ErrInvalidConfig 或 ErrToolBind）
}
```

构造：

```go
func NewSetupError(code, msg string) *SetupError
```

`Unwrap()` 返回对应的哨兵，因此可以同时满足两种判定方式：

```go
// 方式 1：判定是否配置有误（不限哪个代码）
if errors.Is(err, lferrors.ErrInvalidConfig) { ... }

// 方式 2：提取详细信息
var se *lferrors.SetupError
if errors.As(err, &se) {
    fmt.Println(se.Code, se.Msg)
}
```

### 3.2 ToolError

`ToolError` 在执行工具过程中出错时由 `executeToolCalls` 构造：

```go
type ToolError struct {
	Name  string  // 出错的工具名
	Cause error   // Handler 返回的原始错误
}
```

自定义 `Is` 方法返回 `true` 当目标为 `ErrToolExec`：

```go
if errors.Is(err, lferrors.ErrToolExec) {
    // 工具执行失败
}
var te *lferrors.ToolError
if errors.As(err, &te) {
    fmt.Println("工具", te.Name, "失败:", te.Cause)
}
```

---

## 4. 错误分布与事件码

Agent loop 中的 `emitError` 使用以下 `code` 字符串：

| 事件码 | 哨兵 | 发射源 |
|--------|------|--------|
| `"invalid_request"` | `ErrInvalidRequest` | `loop.go` — req == nil |
| `"invalid_config"` | `ErrInvalidConfig` | `loop.go` — ChatModel 为 nil / system_prompt_builder 错误 / `bindModel` 失败 |
| `"generate"` | `ErrGenerate` | `loop.go` — consumeStream 出错 |
| `"tool_exec"` | `ErrToolExec` | `tools.go` — tool.Invoke 失败（第一个出错工具终止整步） |
| `"system_prompt_builder"` | `ErrInvalidConfig` | `loop.go` — SystemPromptBuilder 返回错误 |
| `"max_transfers"` | `ErrMaxTransfers` | `runner.go` — transfer 循环超限 |
| `"invalid_transfer"` | `ErrInvalidTransfer` | `runner.go` — 找不到 handoff 目标 |

---

## 5. 使用示例

### 5.1 基本判定

```go
ch := r.Run(ctx, req)
for ev := range ch {
    switch ev.Type {
    case event.EventError:
        p := ev.Error()
        fmt.Println("错误码:", p.Code, "消息:", p.Message)
    case event.EventQueryEnd:
        if p.Outcome.Termination == outcome.TerminationError {
            fmt.Println("运行终止——请查看前面的 error 事件")
        }
    }
}

// 或在同步调用后判定 Go 返回值
result, err := sp.Spawn(ctx, parent, spec)
if err != nil {
    if errors.Is(err, lferrors.ErrInvalidConfig) {
        fmt.Println("spawn 配置有误")
    }
    var te *lferrors.ToolError
    if errors.As(err, &te) {
        fmt.Println("工具出错:", te.Name, te.Cause)
    }
}
```

### 5.2 区分工具错误与配置错误

```go
if errors.Is(err, lferrors.ErrInvalidConfig) {
    // Agent 构造/RunLoop 启动时配置错误
} else if errors.Is(err, lferrors.ErrToolExec) {
    // 工具执行时错误（动作已开始，但某个工具调用失败）
} else if errors.Is(err, lferrors.ErrGenerate) {
    // LLM 调用失败（网络、限流、无效模型）
} else if errors.Is(err, lferrors.ErrReadOnly) {
    // LLM 尝试写入 const_ 变量
}
```

---

## 6. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/errors/errors.go` | 所有哨兵错误定义 + SetupError / ToolError |
| `pkg/agent/loop.go` | emitError（将哨兵错误转为事件流中的 error 事件） |
| `pkg/agent/tools.go` | executeToolCalls 中构造 ToolError |
| `pkg/runner/runner.go` | runTransferLoop 中 emitError("max_transfers" / "invalid_transfer") |
| `pkg/tool/dispatch.go` | 返回 ErrNoHandler |
| `pkg/tool/map.go` | 返回 ErrUnknownTool / ErrExecutorNil |
| `pkg/variable/store.go` | AgentSet 返回 ErrReadOnly |
| `pkg/variable/tools.go` | var_set Handle 返回 ErrNoStore |

---

## 7. 相关文档

| 文档 | 关系 |
|------|------|
| [03-events.md](03-events.md) §2.11 | error 事件类型与载荷（ErrorPayload.Code / Message） |
| [01-agent-core.md](01-agent-core.md) §5 | RunLoop 中 emitError 的调用时机 |
| [04-tools.md](04-tools.md) §5.1 | executeToolCalls 中 ToolError 的构造路径 |
| [07-transfer.md](07-transfer.md) §3.3 | max_transfers 防循环保护的错误处理 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：哨兵错误清单（5 类 14 个）、SetupError/ToolError 结构化类型、事件码映射表。 |

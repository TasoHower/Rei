# 事件系统数据融合 — loopForge ↔ multi-agent-server

> **角色**：本文件记录 loopForge 运行时事件系统与 multi-agent-server（现网 runner）事件系统的**类型语义对齐**。v0.9.8 已完成两端完全对齐——`pkg/events/message.go` 的事件类型常量以 `pkg/runtime/event/event.go` 为权威源同步。  
> **关联实现**：[03-events.md](03-events.md)（loopForge 事件协议完整说明）  
> **关联代码**：`pkg/runtime/event/event.go`（RuntimeEvent）、`pkg/events/message.go`（EventMessage 信封）  
>
> **本文件与 `03-events.md` 的约定**：  
> `03-events.md` 提供 loopForge 内部事件的完整设计文档（payload 定义、时序图、消费者示例）。  
> 本文件聚焦 **两端对齐**——信封差异、常量映射、历史演变、序列化适配。

---

## 1. 信封结构对比

### 1.1 loopForge 内部：RuntimeEvent

`pkg/runtime/event/event.go`：

```go
type RuntimeEvent struct {
    Type    EventMessageType  // 事件类型字符串
    RunID   string            // 所属运行 ID
    Step    int               // LLM 调用轮次
    Payload EventPayload      // 结构化载荷
}
```

轻量级结构体，建议消费者直接访问内部字段而非 JSON 序列化后使用。

### 1.2 multi-agent-server 现网：EventMessage

`pkg/events/message.go`：

```go
type EventMessage struct {
    Code     string                `json:"code"`
    Data     *EventMessageData     `json:"data,omitempty"`
    Type     string                `json:"type"`
    Msg      string                `json:"msg"`
    MetaData *EventMessageMetaData `json:"meta_data,omitempty"`
}
```

重型信封，含 session 维度字段：
- `Code` / `Msg`：结果码 + 可读消息（类似 HTTP 状态码）
- `Data.SegmentCode` / `Data.ConversationID` / `Data.MessageID`：三层 id 坐标
- `Data.Role` / `Data.ContentType`：角色与内容类型
- `MetaData.Extra`：额外的元数据键值对

### 1.3 test-server SSE 适配层

test-server 的 `runtimeEventToSSE` 将 `RuntimeEvent` 转为轻量 SSE 格式：

```go
type SSEEvent struct {
    Type string `json:"type"`
    Step int    `json:"step,omitempty"`
    Data any    `json:"data,omitempty"`
}
```

SSE 序列化时不经过 `EventMessage` 信封，直接将 Payload 作为 `data` 字段值。

---

## 2. 事件类型常量映射

两端常量已完全对齐。`pkg/events/message.go` 的 `EventMessageType` 定义与 `pkg/runtime/event/event.go` 的 `EventMessageType` 定义一一对应：

| loopForge (pkg/runtime/event) | multi-agent-server (pkg/events) | 说明 |
|------------------------------|--------------------------------|------|
| `"start"` | `EventMessageTypeStart` | Run 开始 |
| `"question"` | `EventMessageTypeQuestion` | 本轮用户消息 |
| `"answer"` | `EventMessageTypeAnswer` | 流式文本 delta |
| `"tool_call_start"` | `EventMessageTypeToolCallStart` | 工具调用开始 |
| `"tool_call_end"` | `EventMessageTypeToolCallEnd` | 工具调用结束 |
| `"call_llm_start"` | `EventMessageTypeCallLLMStart` | LLM 调用开始 |
| `"call_llm_end"` | `EventMessageTypeCallLLMEnd` | LLM 调用结束 |
| `"agent_transfer"` | `EventMessageTypeAgentTransfer` | Agent 手递手转移 |
| `"spawn_start"` | `EventMessageTypeSpawnStart` | 子 Agent 启动 |
| `"spawn_end"` | `EventMessageTypeSpawnEnd` | 子 Agent 结束 |
| `"var_change"` | `EventMessageTypeVarChange` | 变量变更 |
| `"query_end"` | `EventMessageTypeQueryEnd` | 本轮执行结束 |
| `"error"` | `EventMessageTypeError` | 运行时错误 |

### 对齐说明

- 🗑️ **已移除**（现网旧类型、loopForge 不需要）：`call_rag_start`、`call_rag_end`、`welcome`
- ✨ **已新增**（loopForge 以新为准同步到现网）：`spawn_start`、`spawn_end`、`var_change`、`error`
- 📝 **payload 仍存在差异**：两端常量字符串一致，但 payload 结构体不同（`event.go` 有类型安全 Payload，`message.go` 用 `Data.MetaData` 承载），由适配层处理。

---

## 3. 事件命名演变（v0.2.0 → 当前）

| v0.2.0 EventType | 当前 EventMessageType | 变更说明 |
|------------------|----------------------|---------|
| `"token"` | `"answer"` | 重命名为流式文本 delta，消除歧义 |
| `"tool_start"` | `"tool_call_start"` | 对齐 OpenAI tool_call 术语 |
| `"tool_end"` | `"tool_call_end"` | 同上 |
| `"done"` | `"query_end"` | 范围扩大：query 级而非 run 级 |
| `"error"` | `"error"` | 相同名，payload 从字符串升级为 `{code, message}` |
| `"step"` | 已移除 | Step 移至 `RuntimeEvent.Step` 字段 |
| `"spawn_start"` | `"spawn_start"` | 命名幸存，payload 从 `AgentTransferPayload` 独立为 `SpawnStartPayload` |
| `"spawn_end"` | `"spawn_end"` | 命名幸存，payload 独立为 `SpawnEndPayload`（含 Metrics） |
| `"unknown"` | 已移除 | 不再需要 |

---

## 4. 序列化适配指南

### 4.1 从 RuntimeEvent → EventMessage

适配器（如需与现网信封对齐）需完成以下映射：

```go
func ToEventMessage(ev *event.RuntimeEvent, segCode, convID, msgID string) *events.EventMessage {
    out := &events.EventMessage{
        Code: "0",
        Type: string(ev.Type),
        Msg:  "ok",
        Data: &events.EventMessageData{
            SegmentCode:    segCode,
            ConversationID: convID,
            MessageID:      msgID,
            EventType:      string(ev.Type),
        },
    }
    // Payload → Data.Answer / Data.MetaData...
    return out
}
```

### 4.2 从 RuntimeEvent → SSE

test-server 已有实现（`runtimeEventToSSE`），将 Payload 直接映射为 SSEEvent.Data。

### 4.3 向前兼容（现网迁移）

现网若此前按 `agent_transfer(phase=start/end, depth>0)` 识别 spawn，需改为监听独立的 `spawn_start` / `spawn_end` 事件：

| 以前（v0.9.7） | 现在（v0.9.8） |
|---------------|---------------|
| `agent_transfer(phase=start, depth=1, child_run_id=..., to_agent=...)` | `spawn_start(agent_role=..., child_run_id=..., depth=1, task_summary=...)` |
| `agent_transfer(phase=end, depth=1, ok=true, child_run_id=...)` | `spawn_end(agent_role=..., child_run_id=..., depth=1, ok=true, status="completed", metrics=...)` |
| `agent_transfer(phase=start, from=triage, to=expert, reason=...)` | `agent_transfer(phase=start, from=triage, to=expert, reason=...)`（不变） |

---

## 5. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/runtime/event/event.go` | RuntimeEvent 结构体、13 种 EventMessageType 常量、12 种 Payload 类型、Emit 构造器 |
| `pkg/events/message.go` | 现网 EventMessage 信封结构体、13 种 EventMessageType 常量（与 event.go 1:1 对齐） |
| `test-server/main.go` | runtimeEventToSSE 序列化适配层 |
| `doc/design/03-events.md` | loopForge 事件协议完整设计文档 |

---

## 变更记录

| 日期 | 说明 |
|------|------|
| 2026-05-01 | 初稿：事件类型映射表、信封对比、命名演变、v0.9.8 spawn 事件迁移指南。 |
| 2026-05-01 | 两端对齐：`pkg/events/message.go` 移除 `call_rag_*`/`welcome`、新增 `spawn_*`/`var_change`/`error`，与 `pkg/runtime/event` 完全同步。 |

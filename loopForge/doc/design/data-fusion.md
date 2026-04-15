# 类型与结构对齐（multi-agent-server 参考）

> **范围**：仅约定 **结构统一** 与 **关键类型语义统一**，使 loopForge Agent 引擎对「可观测输出」的建模与现有 **`multi-agent-server/pkg/runner/types.go`** 一致。  
> **不包含**：Conversation Push、Memory Server API、冷热缓存、落库条数限制等——这些属于 **工程化 / 接入层**，**不属于 Agent engine 本体**；由网关或独立 persistence 适配器实现时可再对齐字段。

参考源码（外仓路径）：

- `multi-agent-server/pkg/runner/types.go` — `EventMessage`、`EventMessageType`、`ContentType` 等

---

## 1. 对齐原则（引擎侧）

| 原则 | 说明 |
|------|------|
| **对外事件壳一致** | 需要把 Run 过程暴露给 UI 时，**单条事件** 的 JSON 形状与 `EventMessage` **同构**（键名不随意删减） |
| **事件类型语义一致** | 使用同一套 **`EventMessageType` 字符串** 表达「开始 / 问答 / 工具 / LLM / RAG / 转交 / 结束」等语义 |
| **引擎不实现存储** | `segment_code` / `conversation_id` / `message_id` 仅作为 **关联上下文的标识语义**；是否写入远端、如何 Push 由 **引擎外** 完成 |

---

## 2. `EventMessage`（对外单条事件）

与现网一致的外层结构（字段语义：包装一次可展示或可审计的增量）：

```go
type EventMessage struct {
	Code     string                `json:"code"`
	Data     *EventMessageData     `json:"data,omitempty"`
	Type     string                `json:"type"` // e.g. "multi_agent"
	Msg      string                `json:"msg"`
	MetaData *EventMessageMetaData `json:"meta_data,omitempty"`
}
```

`EventMessageData` 承载 `segment_code`、`message_id`、`event_type`、`role`、`answer`、`content_type`、`meta_data` 等——**语义上**表示「本条事件绑定的会话坐标 + 业务类型 + 载荷」；具体 JSON 子结构以 `types.go` 为准。

---

## 3. `EventMessageType`（关键类型语义）

引擎内部若产生「用户可见事件」，**`Data.EventType` 建议使用下列枚举值**（与现网同值）：

```go
type EventMessageType string

const (
	EventMessageTypeStart         EventMessageType = "start"
	EventMessageTypeQuestion      EventMessageType = "question"
	EventMessageTypeAnswer        EventMessageType = "answer"
	EventMessageTypeToolCallStart EventMessageType = "tool_call_start"
	EventMessageTypeToolCallEnd   EventMessageType = "tool_call_end"
	EventMessageTypeCallLLMStart  EventMessageType = "call_llm_start"
	EventMessageTypeCallLLMEnd    EventMessageType = "call_llm_end"
	EventMessageTypeCallRAGStart  EventMessageType = "call_rag_start"
	EventMessageTypeCallRAGEnd    EventMessageType = "call_rag_end"
	EventMessageTypeAgentTransfer EventMessageType = "agent_transfer"
	EventMessageTypeQueryEnd      EventMessageType = "query_end"
	EventMessageTypeWelcome       EventMessageType = "welcome"
)
```

### 3.1 引擎阶段 → 类型语义（建议映射）

| 引擎侧含义 | 建议 `EventMessageType` |
|------------|-------------------------|
| Run / 会话开始 | `start` / `welcome` |
| 用户输入已进入本轮 loop | `question` |
| 一轮 LLM 调用起止 | `call_llm_start` / `call_llm_end` |
| 工具（含 MCP）调用起止 | `tool_call_start` / `tool_call_end` |
| RAG 检索起止（若单独打点） | `call_rag_start` / `call_rag_end` |
| Agent 切换或子 Agent 交接 | `agent_transfer` |
| 助手内容片段 | `answer` |
| 本轮用户轮次结束 | `query_end` |

**扩展**：若需单独表达 spawn 生命周期，优先用 **`agent_transfer` + `meta_data`**；新增枚举值需与前端约定，仍属 **类型语义** 问题，不改变引擎核心 loop。

---

## 4. 三层标识：仅结构语义（非落库方案）

与现网 **概念上** 一致的三元组，用于 **关联**「一条用户线 → 一轮交互 → 一条展示消息」：

| 层级 | 标识字段 | 语义（引擎内） |
|------|----------|----------------|
| **Segment** | `segment_code` | 一条可连续交互的会话线 id（由接入层分配或透传） |
| **Conversation** | `conversation_id` / `dialog_id` | 该线下某一 **轮** 交互 id |
| **Message** | `message_id` | 该轮内 **单条** 消息 id |

引擎 **只保证**：在发出 `EventMessage` 时，若需要带这些 id，**字段名与语义**与 `types.go` / 现有消息模型一致。**不**规定何时写入 DB、不规定 Push API。

若需参考「单条消息行」的字段语义，可与外仓 `MultiAgentMessage`（`content`、`content_type`、`event_type`、`role`、`index`、`timestamp` 等）**对齐类型含义**；持久化映射由 **非引擎** 模块完成。

---

## 5. 与 `abstractions.md` 的关系

- **`RuntimeEvent`（§3.2）**：引擎内部归一事件。
- **对外 UI**：适配为 **`EventMessage`**，**`EventType` 字符串** 使用 §3 枚举值，保证 **类型语义统一**。

---

## 6. 相关文档

| 文档 | 作用 |
|------|------|
| `abstractions.md` | `RuntimeEvent`、`SpawnSpec` 等内部契约 |
| `multi-agent-engine.md` | Agent 能力与 loop 行为（与存储解耦） |
| `architecture.md` | 模块划分；持久化、网关属接入层 |

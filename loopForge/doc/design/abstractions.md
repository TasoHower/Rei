# loopForge 关键抽象结构

> 本文档定义引擎对外与对内的 **契约型抽象**：**RuntimeAction（用户侧）**、**最小 MVP 心智（三种 action）**、**Agent 间信息交换**、**Tool**、**MCP**。实现时可映射为 Go 类型或 agent-sdk-go 已有类型；字段以 **英文** 命名为准。  
> 与业务流程细节见 `multi-agent-engine.md`；系统拓扑见 `architecture.md`。

---

## 1. 命名与分层

| 层次 | 含义 |
|------|------|
| **User boundary** | 人类或上游系统通过 **Runtime API**（HTTP/CLI 等）与引擎交互 |
| **Agent boundary** | 多个 **Run** / **Agent 实例** 之间通过 **Spawn** 或 **Network** 交换结构化信息 |
| **Tool boundary** | 模型通过 **ToolCall** 调用；实现侧区分 **local**、**builtin**（如 spawn）、**MCP** |
| **MCP boundary** | 与 **MCP Server** 的会话、工具发现、RPC 调用 |

---

## 2. 最小内核与 MVP 心智（外部参考融合）

本节把「极简可演示」的 Agent 模型与本文其余 **契约字段** 对齐；实现栈默认 **agent-sdk-go** 的 `runner` / `tool`。

### 2.1 两个本质（最小必要能力）

| # | 能力 | 本质（一句话） | loopForge 落点 |
|---|------|----------------|----------------|
| **1** | **Skill（可执行层）** | **MCP + Tool Registry**：发现远端能力 → 映射成 **具名 tool** → 与本地函数 **同一套调用面** | `internal/mcp/` + `ToolRegistry`；指令类 **`SKILL.md`** 仍通过 **注入 prompt** 补充，**不**替代 MCP 发现 |
| **2** | **动态子 Agent（Spawn）** | **LLM 决策 → 生成子目标 → 启动新的 agent loop → 结果回注**；与 Claude Code 类 **sub-agent** 同构 | **递归** `runner.Run`；结构化契约见 **§4** `SpawnSpec` / `SpawnResult` |

**不做的事（demo 先别碰）**：复杂 DAG 工作流、一上来就做重型 **多 Agent 协调编排**（先 **单主循环 + spawn** 即可）。

### 2.2 三种 Action（概念上的一步决策）

外部参考把一步决策压成 **三类**，足够讲清 **Agent loop**：

| `action.Type` | 含义 | 典型实现方式（与 SDK 对齐） |
|---------------|------|-----------------------------|
| **`tool`** | 调已注册工具（含 MCP） | LLM 返回 `tool_calls` → `ToolRegistry` 分发 |
| **`spawn_agent`** | 子任务复杂，起 **子 loop** | 内置 `spawn_subagent` 或解析结构化输出 → `Spawner` |
| **`finish`** | 收束，向用户返回答案 | 无更多 tool call、或显式 `stop` / 自然结束条件 |

概念上存在一个 **Planner**（通常是 **同一次 LLM 调用** 的产出），不必单独进程；若教学/面试需要，可抽 **接口** 便于单测（见 §2.4）。

### 2.3 概念循环伪代码

```go
// Pseudocode: conceptual loop only — wire to agent-sdk-go runner in production.
for step := 0; step < maxSteps; step++ {
	action := planner.Plan(ctx) // one LLM turn + parse

	switch action.Type {

	case "tool":
		result := toolRegistry.Call(action.Tool, action.Input)
		ctx.AddObservation(result)

	case "spawn_agent":
		subResult := spawnAgent(action.Prompt, action.AllowedTools)
		ctx.AddObservation(subResult)

	case "finish":
		return action.Result, nil
	}
}
```

**映射说明**：真实实现里很少用 `switch` 手写三层——多数是 **OpenAI 风格 tool_calls**；**spawn** 可做成 **builtin tool**（参数里带 `prompt` + `allowed_tools`），仍只有一个 **LLM→action** 通道。

### 2.4 教学用核心接口（可选薄封装）

下列接口用于 **讲清楚分层**；生产代码可直接用 agent-sdk-go 类型。

```go
type Agent interface {
	Run(ctx Context) (Result, error)
}

type Tool interface {
	Name() string
	Call(input string) (string, error)
}

// Planner emits the next step; often implemented by LLM + schema / tool routing.
type Planner interface {
	Plan(ctx Context) Action
}

type Action struct {
	Type         string // "tool" | "spawn_agent" | "finish"
	Tool         string
	Input        string
	Prompt       string
	AllowedTools []string
	Result       string
}
```

**注意**：`Planner` **不是** LangGraph；它只是 **一步决策** 的抽象，便于测试 mock。

### 2.5 动态 Sub-Agent：递归 loop 与沙箱

```go
func spawnAgent(prompt string, allowedTools []string) string {
	subCtx := NewContext(prompt)
	subCtx.RestrictTools(allowedTools)

	subAgent := NewAgent() // same loop implementation
	result, _ := subAgent.Run(subCtx)
	return result
}
```

| 角色 | 工具面 |
|------|--------|
| 主 Agent | 全量或默认 allowlist |
| 子 Agent | `allowedTools` **收紧**（如仅 `search` + `code`） |

引擎层须配合 **MaxDepth**、并发与 budget（见 `multi-agent-engine.md` §3.8）。

### 2.6 最小架构图（ASCII）

```
          ┌──────────────┐
          │    Agent     │
          └──────┬───────┘
                 │ loop
         ┌───────▼────────┐
         │    Planner     │   ← LLM (one turn)
         └───────┬────────┘
                 │ action
    ┌────────────┼────────────┐
    │            │            │
    ▼            ▼            ▼
 Tool        SpawnAgent     Finish
(MCP/本地)    (递归 loop)
```

### 2.7 Demo 阶段避坑

| 坑 | 建议 |
|----|------|
| ❌ 复杂 **workflow 引擎** | ✅ 保持 **loop + 三种 action**；编排图交给 **SeRagLF（MCP 内）** 若需要 RAG 闭环 |
| ❌ 一上来 **多 Agent 协调框架** | ✅ **单主 Agent** + **spawn** 先跑通；`pkg/network` 静态策略可第二阶段再加 |
| ❌ 复杂 **长期记忆基建** | ✅ 短期：`[]Message` / slice 即可；长期：**MCP（SeRagLF）** 或 mock |
| ❌ 与 **Eino Graph** 抢职责 | ✅ 本仓库 **只做 loop runtime**；Graph 在 SeRagLF 进程内 |

### 2.8 对外表述（README / 答辩可用）

- **Go Agent Runtime**：**MCP-first skill 系统**（Tool Registry + 多 MCP）。
- **动态 sub-agent spawning**（递归 loop、能力沙箱），语义对齐 **Claude Code** 类体验。
- **自反思 / RAG** 通过 **SeRagLF MCP** 接入，而非在引擎内再造一套图。

（关键词：**MCP**、**dynamic spawn**、**capability scoping**、**agent loop**、可选 **self-RAG via MCP**。）

---

## 3. RuntimeAction（给到用户的运行时动作）

用户侧不关心内部 `runner` 细节，只看到 **请求 → 事件流 / 最终结果**。

### 3.1 请求：`RuntimeRequest`

一次用户发起的「会话内动作」，引擎据此创建或续跑 **Run**。

```go
// RuntimeRequest is the user-visible intent for one interaction.
type RuntimeRequest struct {
	SessionID   string            // stable session for continuity
	UserMessage string            // natural language input
	RunMode     RunMode           // single_agent | network | inherit
	Options     RuntimeOptions  // optional overrides
}

type RunMode string

const (
	RunModeSingleAgent RunMode = "single_agent"
	RunModeNetwork     RunMode = "network"
)

// RuntimeOptions: model, skills, budgets; omit means use server defaults.
type RuntimeOptions struct {
	Model           string
	SkillIDs        []string
	MaxSteps        *int
	SpawnMaxDepth   *int
	NetworkStrategy string // when RunModeNetwork: parallel | sequential | competitive
}
```

### 3.2 流式事件：`RuntimeEvent`（可选 SSE / WebSocket）

用于 **打字机、步骤反馈、调试 UI**；每条为 **判别联合体**（实现可用 `type` 字段 + payload）。

```go
type RuntimeEventType string

const (
	EventToken       RuntimeEventType = "token"
	EventStep        RuntimeEventType = "step"
	EventToolStart   RuntimeEventType = "tool_start"
	EventToolEnd     RuntimeEventType = "tool_end"
	EventSpawnStart  RuntimeEventType = "spawn_start"
	EventSpawnEnd    RuntimeEventType = "spawn_end"
	EventError       RuntimeEventType = "error"
	EventDone        RuntimeEventType = "done"
)

// RuntimeEvent is one outbound chunk to the user client.
type RuntimeEvent struct {
	Type    RuntimeEventType
	RunID   string
	Step    int    // logical step index after LLM or tool round, engine-defined
	Payload any    // typed per Type; see below
}

// Examples of Payload shapes (define as separate structs in implementation):
// TokenPayload: { "delta": "..." }
// ToolStartPayload: { "tool_call_id": "...", "name": "..." }
// ToolEndPayload: { "tool_call_id": "...", "ok": true }
// SpawnStartPayload: { "child_run_id": "...", "depth": 1 }
// SpawnEndPayload: { "child_run_id": "...", "ok": true }
// ErrorPayload: { "code": "...", "message": "..." }
// DonePayload: { "termination": "completed|max_steps|cancelled" }
```

### 3.3 最终结果：`RuntimeOutcome`

同步 API 或流结束时的 **汇总**；与 **观测**（tokens、cost）对齐。

```go
type RuntimeOutcome struct {
	RunID         string
	FinalText     string
	Termination   TerminationReason
	Metrics       RunMetrics
	ChildRunIDs   []string // if spawn occurred; optional flat list
}

type TerminationReason string

const (
	TerminationCompleted TerminationReason = "completed"
	TerminationMaxSteps  TerminationReason = "max_steps"
	TerminationCancelled TerminationReason = "cancelled"
	TerminationError     TerminationReason = "error"
)

type RunMetrics struct {
	InputTokens  int64
	OutputTokens int64
	TotalCostUSD float64 // estimated, from model catalog + usage
}
```

---

## 4. Agent 之间的信息交换结构

包含 **静态 Network**（多 Agent 流水线）与 **动态 Spawn**（父子 Run）。

### 4.1 公共标识

```go
// RunRef identifies one agent loop execution in the engine.
type RunRef struct {
	RunID       string
	AgentRole   string // logical name: "planner", "worker", user-defined
	ParentRunID string // empty if root
	Depth       int    // 0 = root; spawn increments
}
```

### 4.2 静态 Network：`NetworkTurn`（示意）

预置 roster 时，各 slot 的 **输入/输出** 由 `pkg/network` 策略定义；引擎可包装为：

```go
// NetworkTurn describes one agent slot output feeding synthesis or next slot.
type NetworkTurn struct {
	SlotName   string
	RunRef     RunRef
	InputHint  string // optional: condensed task from orchestrator
	OutputText string
	ToolCalls  []ToolCall // if exposing mid-turn detail
}
```

合成阶段由 **orchestrator** 产生 **FinalText**；对用户仍映射为 **RuntimeOutcome**。

### 4.3 动态 Spawn：父 → 子 `SpawnSpec`

父 Run 内模型调用 **spawn** 时，引擎解析为 **结构化规格**（可与 JSON Schema 对齐）。

```go
// SpawnSpec is the cross-agent contract from parent to child run.
type SpawnSpec struct {
	Task            string   // sub-goal in natural language
	SystemAddendum  string   // appended to child system prompt
	SkillIDs        []string // optional extra skills for child only
	ToolAllowlist   []string // empty means inherit engine default subset
	LoopOverrides   LoopOverrides
	ModelOverride   string // optional
}

type LoopOverrides struct {
	MaxSteps *int
	Timeout  *string // ISO-8601 duration string in API; time.Duration in Go
}
```

### 4.4 动态 Spawn：子 → 父 `SpawnResult`

子 Run 结束后 **唯一回注通道**（与 tool result 一致）。

```go
// SpawnResult is returned to the parent agent as tool result content.
type SpawnResult struct {
	ChildRunRef RunRef
	Status      SpawnStatus
	FinalText   string
	Error       *SpawnError
	Metrics     RunMetrics
}

type SpawnStatus string

const (
	SpawnCompleted SpawnStatus = "completed"
	SpawnFailed    SpawnStatus = "failed"
	SpawnRejected  SpawnStatus = "rejected" // policy: depth, budget, allowlist
)

type SpawnError struct {
	Code    string
	Message string
}
```

### 4.5 信息交换原则

| 原则 | 说明 |
|------|------|
| **默认隔离** | 子 Run **不**自动包含父级全量消息；仅 `SpawnSpec` + 引擎注入的元数据（如 `parent_run_id`） |
| **显式传递** | 若需上下文，由父在 `Task` / `SystemAddendum` 写入摘要 |
| **可观测** | `ParentRunID` / `Depth` 进入 trace 与 **RuntimeEvent**（`spawn_start` / `spawn_end`） |

---

## 5. Tool 抽象

统一 **本地函数**、**MCP 映射工具**、**内置 spawn** 的 **同一套表面**。

### 5.1 描述：`ToolDescriptor`（暴露给模型）

```go
type ToolOrigin string

const (
	ToolOriginLocal   ToolOrigin = "local"
	ToolOriginMCP     ToolOrigin = "mcp"
	ToolOriginBuiltin ToolOrigin = "builtin"
)

// ToolDescriptor is what the LLM sees in tools/list equivalent.
type ToolDescriptor struct {
	Name        string // exposed name, possibly prefixed for MCP
	Description string
	InputSchema string // JSON Schema as string
	Origin      ToolOrigin
	// Optional: for debugging and policy
	SourceRef   string // e.g. "mcp:seraglf#retrieve"
}
```

### 5.2 调用与结果

```go
// ToolCall is emitted by the model (one round may have many).
type ToolCall struct {
	ID        string
	Name      string
	Arguments string // JSON object as string
}

// ToolResult is fed back into the message history for the next loop round.
type ToolResult struct {
	ToolCallID string
	Content    string // text or JSON string; engine-defined
	IsError    bool
}
```

### 5.3 注册表键

```go
// ToolRegistryKey is internal stable id; exposed Name may differ (MCP prefix).
type ToolRegistryKey struct {
	Origin ToolOrigin
	Name   string
}
```

### 5.4 MCP 包装示例（Skill 可执行层）

每个 **对外能力** 在注册表里占一个 **名字**；MCP 侧 `tools/list` 映射后 **看起来像普通 Tool**。与 **§2.1**「Skill = MCP + Registry」一致。

```go
// Conceptual: one MCP-backed tool wraps one MCP tool name.
type MCPSkill struct {
	ServerID  string
	ToolName  string
	Client    MCPClient // session + tools/call
}

func (s *MCPSkill) Name() string {
	return s.ServerID + "__" + s.ToolName // or alias from config
}

func (s *MCPSkill) Call(input string) (string, error) {
	return s.Client.CallTool(s.ToolName, input)
}
```

```go
toolRegistry.Register("search", mcpSearchTool)
toolRegistry.Register("memory", mcpMemoryTool)
```

**与 `SKILL.md` 的关系**：文档负责 **行为约束与流程**；**MCP** 负责 **可执行能力**；二者叠加，不是二选一。

---

## 6. MCP 抽象

### 6.1 连接配置：`MCPServerProfile`

每个 MCP Server 一条配置；引擎启动 **connector**。

```go
type MCPTransportKind string

const (
	MCPTransportStdio          MCPTransportKind = "stdio"
	MCPTransportStreamableHTTP MCPTransportKind = "streamable_http"
)

type MCPServerProfile struct {
	ID          string // stable id: "seraglf", "filesystem"
	Transport   MCPTransportKind
	Command     []string // stdio: argv
	URL         string   // http: base URL if applicable
	Env         map[string]string
	Headers     map[string]string // http auth
	ToolPrefix  string            // exposed name prefix, e.g. "seraglf__"
}
```

### 6.2 映射：`MCPMappedTool`

将 MCP 的 tool 名称映射到 **ToolDescriptor.Name**（带前缀防冲突）。

```go
type MCPMappedTool struct {
	ServerID     string
	MCPToolName  string // as returned by MCP tools/list
	ExposedName  string // equals profile.ToolPrefix + MCPToolName or custom alias
	InputSchema  string // from MCP tool definition
}
```

### 6.3 调用路径

| 步骤 | 说明 |
|------|------|
| Discover | `tools/list` per connection → build `MCPMappedTool` list |
| Expose | merge into **ToolRegistry** as `ToolOriginMCP` |
| Invoke | model calls `ToolCall.Name` → engine routes to MCP `tools/call` with **MCPToolName** + arguments |

### 6.4 Resources / Prompts（可选）

若产品需要，可并行维护：

```go
type MCPResourceRef struct {
	ServerID string
	URI      string
}

type MCPPromptRef struct {
	ServerID string
	Name     string
}
```

引擎策略决定是 **预取为上下文** 还是 **暴露为只读工具**；详见 `multi-agent-engine.md` §3.7。

---

## 7. 与实现栈的对应关系

| 抽象 | 典型落点 |
|------|----------|
| **§2** 三种 action / `Planner` | 概念映射到 LLM `tool_calls` + builtin `spawn` |
| `RuntimeRequest` / `RuntimeEvent` / `RuntimeOutcome` | `internal/engine` + 接入层（HTTP handler） |
| `SpawnSpec` / `SpawnResult` | `Spawner` + builtin `spawn_subagent` tool |
| `ToolDescriptor` / `ToolCall` / `ToolResult` | `pkg/tool` 扩展 + `ToolRegistry` |
| `MCPServerProfile` / `MCPMappedTool` | `internal/mcp` |

---

## 8. 相关文档

| 文档 | 内容 |
|------|------|
| `data-fusion.md` | 与现网 runner **类型对齐**：`EventMessage`、`EventMessageType`、三层 **id 语义**（非落库/Push） |
| `multi-agent-engine.md` | Loop、spawn、skills、MCP 行为与验收 |
| `architecture.md` | 模块与部署 |

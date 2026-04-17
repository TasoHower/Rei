# loopForge 关键抽象结构

> 本文档描述仓库 **当前实现** 中的契约与分层，并给出 **与源码一致的代码与结构示例**（代码块内为英文标识符与注释）。  
> 愿景与路线图见 `multi-agent-engine.md`；事件与现网对齐见 `data-fusion.md`；MCP 与 Ark schema 见 `mcp-tool-unification.md`。

---

## 0. 总览：包与调用方向

```
                    +------------------+
                    |  HTTP / CLI /    |
                    |  test-server     |
                    +--------+---------+
                             | *request.RuntimeRequest
                             v
                    +--------+---------+
                    |  pkg/runner      |  Run() -> chan *event.RuntimeEvent
        handoffs    |  (optional)      |
        +---------->|                  |
        |           +--------+---------+
        |                    |
        |                    | clones *agent.Agent per hop (transfer)
        v                    v
+------------------+  +------+-----------+
| *agent.Agent     |  | bindModel:       |
| RunLoop / Stream |  | ToolInfos +      |
| tool.Invoke      |  | mcpToolInfos +   |
|                  |  | ExtraTools       |
+------------------+  +------------------+
        |                        ^
        |                        | MCPServerProfiles
        v                        | -> mcp.BootstrapToolInfos
+------------------+      +------+--------+
| pkg/model        |      | pkg/mcp       |
| ToolCallingChat  |      | + pkg/mcp/cfg |
+------------------+      +---------------+
        |
        v
+------------------+
| pkg/tool.Invoke  |
+------------------+
```

**`WithVariable`**：`RunLoop` 创建或复用 **`variable.VarStore`**，注入 **`var_set`**（经 **`tool.Invoke`**）；**`{{key}}`** 替换见 **`pkg/agent/brace_params.go`**。

**调试 MCP（不经 Agent）**：`loopforge/debug` → `Dial` → `Ping` / `ListTools` / `CallTool`，与 **`pkg/mcp.ConnectClientSession`** 复用连接逻辑。

---

## 1. 命名与分层

| 层次 | 含义 | 代码落点 |
|------|------|----------|
| **Runtime boundary** | 一次用户输入对应一次 **Run**；对外为 **事件流** + **最终 Outcome** | `pkg/runtime/request`、`pkg/runtime/event`、`pkg/runtime/outcome` |
| **Execution boundary** | **Runnable**：`Run` → `chan`；**Runner** 负责 transfer 与指标汇总 | `pkg/agent`、`pkg/runner` |
| **Model boundary** | Chat 模型、消息、`ToolInfo` schema、`Stream` | `pkg/model`、`pkg/model/types`、`pkg/model/adapters/*` |
| **Tool boundary** | `tool_calls` → **`tool.Invoke`** | `pkg/tool` |
| **Variable boundary** | 会话级 **`VarStore`**、`var_set` 工具、`{{name}}` 替换、`[Variables]` 注入 | `pkg/variable` + **`agent.WithVariable`** |

---

## 2. `Runnable`、`Runner` 与事件消费（示例）

**接口**（`pkg/agent/interface.go`）：

```go
type Runnable interface {
	Run(ctx context.Context, req *request.RuntimeRequest) <-chan *event.RuntimeEvent
}
```

**典型用法**：用 **`runner.NewRunner`** 包装入口 Agent，从 channel 读事件直到 **`query_end`**（payload 内嵌 **`RuntimeOutcome`**）或 **`error`**。

```go
r := runner.NewRunner(entryAgent,
	runner.WithMaxTransfers(10),
	runner.WithVarStore(sharedStore), // optional; nil -> new store per run
)

ch := r.Run(ctx, &request.RuntimeRequest{
	SessionID:   "sess-1",
	UserMessage: "Hello",
	RunMode:     request.RunModeSingleAgent,
	Options:     request.RuntimeOptions{Model: "..."},
})

for ev := range ch {
	switch ev.Type {
	case event.EventQueryEnd:
		if p := ev.QueryEnd(); p != nil && p.Outcome != nil {
			_ = p.Outcome.FinalText
		}
	case event.EventError:
		if p := ev.Error(); p != nil {
			_ = p.Message
		}
	}
}
```

**说明**：若 **`entryAgent` 无 handoff**，Runner 行为接近直接调 **`Agent.Run`**；有 handoff 时 Runner 在内部切换 Agent 并维护 **`LoopState`**（见 `pkg/agent`）。

---

## 3. `Agent`：字段、选项与一轮 loop 心智

### 3.1 核心字段（与 `pkg/agent/agent.go` 对齐）

| 字段 / 行为 | 说明 |
|-------------|------|
| `ChatModel` | `model.ToolCallingChatModel` |
| `ToolInfos` | 本地工具；参与 **`tool.ValidateBindings`**（需 **Handle** 或 **`Executor`**） |
| `mcpToolInfos` | 由 **`mcp.BootstrapToolInfos`** 填充；排在 `ToolInfos` 之后 |
| `ExtraTools` | 发给模型；**不参与** `ValidateBindings`（如 **`transfer_to_*`**） |
| `MCPServerProfiles` | 非空则每次 **`RunLoop`** 内 **`bindModel`** 时 Bootstrap |
| `ToolInterceptor` | 返回 true 时 **不** `Invoke`，返回 **`InterceptedCall`**（transfer） |
| `Variable` | 启用 **`var_set`** 与 **`[Variables]`** 块 |

### 3.2 构造示例（本地工具 + 可选 MCP profile）

```go
chat := larkadapter.NewLarkChatModel(apiKey, "", modelName)

local := []*model.ToolInfo{{
	Name:        "echo",
	Description: "Echo input",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
		"required": []string{"text"},
	},
	Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
		return argumentsJSON, nil
	},
}}

ag := agent.New(chat,
	agent.WithName("main"),
	agent.WithToolInfos(local),
	agent.WithMCPServerProfiles(cfg.MCPServerProfile{
		ID:         "seraglf",
		Transport:  cfg.MCPTransportStreamableHTTP,
		URL:        "http://127.0.0.1:8080/mcp",
		ToolPrefix: "seraglf__",
	}),
	agent.WithMaxSteps(32),
)
```

**手动合并 MCP（不挂在 Agent 上）** 可用 **`agent.AttachMCP`**，再 **`WithToolInfos(append(local, b.ToolInfos...))`** 并 **`defer b.Stop()`**。

### 3.3 主路径 loop（概念对齐 `pkg/agent/loop.go`）

真实实现是 **for step := 0; step < maxSteps; step++**：拼 system → **`emit(call_llm_start)`** → **`Stream`** → **`emit(call_llm_end)`** → 若无 **`tool_calls`** 则结束；若有则 **`executeToolCalls` → `tool.Invoke`** → 把 tool 消息 append 到 **`[]*model.Message`** 再下一轮。

与「Planner 输出 `tool|spawn_agent|finish`」教学模型的关系：**默认路径没有单独 Planner goroutine**；**`pkg/runtime/action.LoopAction`** 与 **`agent.Planner`** 接口存在，供测试或未来上层封装，**不是** `RunLoop` 的必经步骤。

---

## 4. 合并工具顺序与 `bindModel`

**`mergedToolInfos`**（`pkg/agent/helpers.go`）顺序固定为：

1. `ToolInfos`（本地）
2. `mcpToolInfos`（MCP）
3. `ExtraTools`（如 transfer、`var_set` 等注入）
4. 可选 **extraRuntime**（单次 run 动态附加）

发给模型的 **`WithTools`** 使用 **全量 merged**；**`ValidateBindings`** 只检查 **`ToolInfos` + `mcpToolInfos`**（不含 `ExtraTools`）。

```
  bindModel
      |
      +-- MCPServerProfiles non-empty? --> mcp.BootstrapToolInfos -> mcpToolInfos, mcpStop
      |
      +-- ValidateBindings(ToolInfos + mcpToolInfos, Executor)
      |
      +-- ChatModel.WithTools( mergedToolInfos(...) )
```

---

## 5. `tool.Invoke` 解析链（示例）

**源码顺序**（`pkg/tool/dispatch.go`）：

```go
func Invoke(ctx context.Context, infos []*model.ToolInfo, ex ToolExecutor, tc model.ToolCallPart) (string, error) {
	if tc.Handle != nil {
		return tc.Handle(ctx, tc.Arguments)
	}
	for _, info := range infos {
		if info == nil || info.Name != tc.Name {
			continue
		}
		if info.Handle != nil {
			return info.Handle(ctx, tc.Arguments)
		}
	}
	if ex != nil {
		return ex.Execute(ctx, tc.Name, tc.Arguments)
	}
	// ... ErrNoHandler
}
```

MCP 工具在 **`BootstrapToolInfos`** 里为每个远端 tool 设置 **`ToolInfo.Handle`**，因此落在 **第 2 步**（按 **暴露名** 匹配）。

---

## 6. Custom variables（`pkg/variable`）

会话级 **自定义变量** 用于：跨轮次状态、在 **user message** 里用 **`{{key}}`** 做占位符替换、在 **system** 里注入 **`[Variables]`** 块，供模型只读当前值。实现集中在 **`pkg/variable`**，由 **`Agent`** 在启用 **`WithVariable`** 时接入 **`RunLoop`**。

### 6.1 核心类型

| 类型 | 作用 |
|------|------|
| **`VarStore`** | 线程安全的键值容器；每项为 **`VarEntry`**（`Value`、`Description`、`Visitable`）。 |
| **`Manifest` / `Spec`** | 静态清单：声明 key、描述、可选 **Default**、**Visitable**（默认 true）。 |
| **`Materialize`** | 合并 **清单** + 可选 **持久化快照** + **本轮 runtime 绑定** 得到初始 **`VarStore`**（见 `variable.Materialize`）。 |
| **`StoreSnapshot`** | 序列化快照；用于跨请求恢复（如 test-server 按 `session_id` 存 JSON）。 |

### 6.2 只读键：`const_` 前缀

以 **`const_`** 开头的 key 对 **`var_set`（Agent 侧）只读**：**`VarStore.AgentSet`** 会拒绝写入。应用可在 **runtime 绑定** 或 **`Materialize`** 时设置这些键，供提示词说明「只读上下文」（如 `const_session_id`）。

### 6.3 与 `Agent.RunLoop` 的接线

1. **`LoopState.VarStore`**（`pkg/agent`）：若 Runner 传入共享 store，本轮沿用；否则 **`variable.New()`** 新建。
2. **`ctx`**：**`variable.NewContext(ctx, vstore)`**，便于 **`VarSetTool`** 闭包外通过 **`variable.FromContext(ctx)`** 解析 store。
3. **`agent.WithVariable()`**：`Variable == true` 时，向 **`bindModel`** 多传 **`variable.VarSetTool(vstore)`**（工具名 **`var_set`**）。该工具在 **extraRuntime** 一侧合并进 **`mergedToolInfos`**，故 **不参与** **`ValidateBindings`**（与 transfer 工具类似）。
4. **用户消息**：首轮与后续每步会对 **`UserMessage` 模板** 做 **`ReplaceDoubleBraceParams`**，参数来自 **`stringParamsFromVarStore`**（visitiable 键的字符串化展示；未设置可显示为约定占位）。

### 6.4 `var_set` 工具（模型调用）

- **推荐**：`updates` 对象，一次改多个 key。
- **兼容**：单字段 **`key` + `value`**（legacy）。

```go
// Tool name: "var_set". Handle uses closure VarStore or variable.FromContext(ctx).
// Parameters (excerpt): updates map[string]any, or legacy key + value.
```

内部调用 **`store.AgentSet`**；**`const_`** 键被拒绝。

### 6.5 System prompt：`[Variables]` 与 `SystemPromptBuilder`

- 启用 **`WithVariable`** 时，**`runLoopFullSystem`** 会把 **`VarStore.PromptBlock()`** 以 **`[Variables]`** 形式并入 system（具体拼接见 **`pkg/agent/loop.go`** / **`user_message.go`**）。
- 可选 **`SystemPromptBuilder`**：在 **`PromptBuildInput`** 里可拿到 **`VarStore`**、**`VariablePromptBlock`**，用于自定义 system 组装顺序（先 builder 再 **`{{name}}`** 替换等）。

### 6.6 可观测：`var_change` 事件

**`EventVarChange` / `VarChangePayload`** 已在 **`pkg/runtime/event`** 定义（含 **operation / key / value / agent**），客户端可按类型解析。当前 **`RunLoop` 在 `var_set` 成功路径上尚未统一 `emit` 该事件**；若需要 SSE 同步或审计，可在 **`executeToolCalls`** 或 **`VarStore` 层** 增加回调后发射（见 `doc/acceptance/v0.5.0-acceptance.md` 相关说明）。

### 6.7 与 `Runner` / transfer

**`runner.WithVarStore`** 可把 **同一 `VarStore`** 注入 **`LoopState`**，使多 Agent handoff **共享变量**；若未设置，每 Run 仍可在单 Agent 内使用独立 **`VarStore`**。

---

## 7. 用户侧契约：`RuntimeRequest` / `RuntimeEvent` / `RuntimeOutcome`

### 7.1 请求

```go
type RuntimeRequest struct {
	SessionID   string
	UserMessage string
	RunMode     RunMode // RunModeSingleAgent | RunModeNetwork | RunModeInherit
	Options     RuntimeOptions
}

type RuntimeOptions struct {
	Model           string
	SkillIDs        []string
	MaxSteps        *int
	SpawnMaxDepth   *int
	NetworkStrategy string // e.g. parallel | sequential | competitive when RunModeNetwork
}
```

### 7.2 事件：类型与构造

**`RuntimeEvent`**：`Type`（**`EventMessageType`**）、`RunID`、`Step`、`Payload`（**`EventPayload`** 实现类型）。

```go
ev := event.Emit(runID, step, &event.CallLLMStartPayload{
	Model:       "deepseek-...",
	SystemPrompt: fullSystem,
	Tools:       []event.LLMToolSummary{{Name: "echo", Description: "..."}},
})
```

**与 LLM 强相关的 payload**：**`CallLLMStartPayload`**（含 **`Tools`**、**`SystemPrompt`**；**`MCPServerIDs` / `MCPToolNames`** 字段已定义）、**`CallLLMEndPayload`**（**`FinishReason`**：`stop` | `tool_calls` | `length` | `error`**）、**`ToolCallStartPayload` / `ToolCallEndPayload`**。

### 7.3 结束：`RuntimeOutcome`

```go
type RuntimeOutcome struct {
	RunID         string
	FinalText     string
	Termination   TerminationReason
	Metrics       RunMetrics
	ChildRunIDs   []string
	TransferChain []string
	VarStore      *variable.VarStore `json:"-"`
}
```

### 7.4 典型事件顺序（单 Agent、无 error）

```
start -> question -> call_llm_start -> call_llm_end
   -> (repeat: tool_call_start -> tool_call_end -> call_llm_start -> call_llm_end)
   -> answer (streaming deltas) ...
   -> query_end (payload.Outcome)
```

Transfer 模式下会插入 **`agent_transfer`**；变量侧可观测事件类型见 **§6.6**（**`var_change`** 载荷已定义，运行时发射视实现而定）。

---

## 8. `pkg/runtime/exchange`：跨 Agent 契约结构

用于 **Network / Spawn** 等编排，与 **transfer 运行时**（handoff + Runner）互补。

```go
type RunRef struct {
	RunID       string
	AgentRole   string
	ParentRunID string
	Depth       int
}

type SpawnSpec struct {
	Task           string
	SystemAddendum string
	SkillIDs       []string
	ToolAllowlist  []string
	LoopOverrides  LoopOverrides
	ModelOverride  string
}

type LoopOverrides struct {
	MaxSteps *int
	Timeout  *time.Duration
}
```

**`internal/engine`** 声明 **`Spawner`**、**`LoopPolicy`** 等；**当前生产 MCP 接入**不依赖这些接口，而走 **`Agent.MCPServerProfiles` + `pkg/mcp`**。

---

## 9. `pkg/model`：消息与 `ToolInfo`

```go
type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
	Handle      ToolCallHandler `json:"-"`
}

type ToolCallPart struct {
	ID        string
	Name      string
	Arguments string
	Handle    ToolCallHandler `json:"-"`
}
```

**`pkg/runtime/tool`** 另有一套 **`ToolDescriptor` / `ToolOrigin`** 等，用于 **元数据与编排描述**；**Agent 主路径**以 **`model.ToolInfo`** 为准。

---

## 10. MCP：`cfg`、Bootstrap、调试

### 10.1 Profile（节选）

```go
type MCPServerProfile struct {
	ID            string
	Transport     MCPTransportKind // MCPTransportStdio | MCPTransportStreamableHTTP
	Command       []string
	URL           string
	Env           map[string]string
	Headers       map[string]string
	ToolPrefix    string
	ToolAllowlist []string
}
```

### 10.2 集成：Discover → 暴露名 → Handle

- **暴露名**：**`ToolPrefix + MCPToolName`**（**`mcp.ExposedToolName`**）。
- **Bootstrap**：**`mcp.BootstrapToolInfos(ctx, profiles...)`** → **`[]*model.ToolInfo`** + **`stop`**。

### 10.3 调试包（不经 Agent）

```go
import (
	"context"

	lfdebug "loopforge/debug"
	"loopforge/pkg/mcp/cfg"
)

conn, stop, err := lfdebug.Dial(ctx, cfg.MCPServerProfile{
	ID:        "probe",
	Transport: cfg.MCPTransportStreamableHTTP,
	URL:       "http://127.0.0.1:8080/mcp",
})
if err != nil { /* ... */ }
defer stop()

_ = conn.Ping(ctx)
tools, _ := conn.ListTools(ctx)
_, _ = conn.CallTool(ctx, "some_mcp_tool_name", map[string]any{"query": "x"})
```

---

## 11. `internal/engine` 补充

| 项 | 说明 |
|----|------|
| **`Engine`** | **`agent.Runnable`** 薄接口 |
| **`MCPConnector` / `MCPSession`** | 编排层可选抽象；**当前实现**用 **`pkg/mcp`** |
| **`Planner` 别名** | 指向 **`pkg/agent.Planner`** |

---

## 12. 与代码目录的对应关系

| 主题 | 包路径 |
|------|--------|
| 请求 / 事件 / 结果 | `pkg/runtime/request`、`pkg/runtime/event`、`pkg/runtime/outcome` |
| 概念一步三决策 | `pkg/runtime/action` |
| Spawn/Network 结构 | `pkg/runtime/exchange` |
| Agent loop、MCP 合并 | `pkg/agent` |
| Runner、transfer | `pkg/runner` |
| 模型与适配器 | `pkg/model`、`pkg/model/adapters/*` |
| Tool 分发 | `pkg/tool` |
| MCP | `pkg/mcp/cfg`、`pkg/mcp` |
| MCP 调试 | `loopforge/debug` |
| 变量 | `pkg/variable` |

---

## 13. 相关文档

| 文档 | 内容 |
|------|------|
| `doc/design/data-fusion.md` | 事件类型与现网对齐 |
| `doc/design/multi-agent-engine.md` | 多 Agent、spawn、路线图 |
| `doc/design/mcp-tool-unification.md` | MCP 与 Lark/Ark 单出口 |
| `doc/design/architecture.md` | 模块与部署（若存在） |

# loopForge 项目进度日志 — v0.6.0

> **版本**：v0.6.0  
> **日期**：2026-04-16（计划稿）  
> **里程碑**：**MCP（Model Context Protocol）客户端与工具接入** — 在 Agent loop 内将远端 MCP tools 与本地 `ToolInfo` 统一暴露；**联调与验收以 SeRagLF（`seraglf`）为参考 MCP Server**  
> **上一版本**：v0.5.2（Lark 适配器、Runner 默认 Lark、Ark 工具 schema 兼容）  
> **下一版本**：[v0.7.0](progress-v0.7.0.md)（Skills 技能 / 指令包）

---

## Tools：现状与改造计划

### 现状（v0.5.x）

| 层次 | 现状 |
|------|------|
| **模型契约** | `pkg/model/types.ToolInfo`：`Name`、`Description`、`Parameters`（`map[string]interface{}`，OpenAI function 的 JSON Schema）、`Handle`（`ToolCallHandler`，签名为 `func(ctx, argumentsJSON string) (string, error)`）。 |
| **发给 LLM** | `ToolCallingChatModel.WithTools([]*ToolInfo)`；Lark 路径下 `pkg/model/adapters/lark` 将 `Parameters` 经 **`toArkTools` → `arkToolParametersForAPI`** 再请求 Ark（strip 不支持关键字、补 `type` 等）。 |
| **执行路径** | `pkg/agent` 在 `runLoop` 内对每步 `Stream` 返回的 `ToolCalls` 调用 **`pkg/tool.Invoke`**；解析顺序为：`ToolCallPart.Handle` → 同名的 **`ToolInfo.Handle`** → **`ToolExecutor.Execute`**（见 `pkg/tool/dispatch.go`）。 |
| **内置 / 组合工具** | **`variable.VarSetTool`**：带 `Handle` 的 `ToolInfo`；**`transfer_to_*`**：`BuildTransferTools` 生成 **无 Handle** 的 schema，由 **Runner 拦截**（控制流，不经 `Invoke` 默认路径）。 |
| **校验** | `bindModel` 前 **`tool.ValidateBindings`**：对 **`ToolInfos`** 中每条工具，须有 **`ToolInfo.Handle` 或** 非空 **`ToolExecutor`**（否则 `WithTools` 失败）。**`ExtraTools`**（如 **`transfer_to_*`**）无 Handle、**不参与**该校验，由 Runner 拦截。 |
| **引擎抽象** | `internal/engine.ToolHandler` / **`ToolRegistry`** 仅为 **接口定义**，主路径 **尚未**接 MCP；统一面仍以 **`Agent` + `[]*ToolInfo` + `tool.Invoke`** 为准。 |

**结论**：「普通 function call」已跑通：**声明**（`ToolInfo` + schema）与 **执行**（`argumentsJSON` → `Handle`）分离。v0.6.0 起 **MCP 工具在集成路径上应由 SDK 从 profile 自动发现与绑定**（如 **`pkg/mcp.BootstrapToolInfos`**），使用方 **不再** 手写 MCP 侧 `ToolInfo` 或自行拼装绑定逻辑；**排障/调试** 一律走 **`loopforge/debug`**（**`Dial` + `Conn.Ping` / `ListTools` / `CallTool`**，见下文 §**6）**），与 **`pkg/mcp`** 的集成意图 **分离**，避免混淆。缺的是 **实现 Bootstrap、`loopforge/debug` 与合并进 `Agent` 的工程落地**。

### 改造计划（v0.6.0）

1. **对外体验（硬性）**：使用方 **只提供 MCP 客户端侧配置**（如一条或多条 **`MCPServerProfile`**：transport、命令或 URL、鉴权、`ToolPrefix`、可选 allowlist）；**不得**要求使用方在 **应用集成路径**上自行调用底层 **`ListTools`**、手写每条 MCP 的 **`ToolInfo` 列表**、或与协议 **`CallTool`** 手工对齐来「完成注册」。**由 loopForge SDK（建议落点 `pkg/mcp` 或等价入口）在 `BootstrapToolInfos` 内完成**：建连 → **`ListTools`** → 生成 **`MCPMappedTool`** → 归一化 schema → 生成带内部 **`Handle` / 统一 `ToolExecutor`** 的 **`[]*ToolInfo`** → 与本地工具合并并参与 **`bindModel`**。  
   - **与调试 API 的关系**：**`Ping` / `ListTools` / `CallTool`** 仅通过 **`loopforge/debug`** 暴露（**不与 `pkg/mcp` 根包混放**），下文 **§6）**；供 CLI/测试使用，**不属于**「绕过 Bootstrap 手写集成」。实现上宜与 **`BootstrapToolInfos` 复用同一套 session/解析**，避免 **双连接**语义歧义（若短期需两次 `Dial`，文档与示例应标注 **仅调试用**）。  
2. **发现与绑定（SDK 内部）**：按 profile 创建 **`mcp.Client` + `Connect` + `ClientSession`**；分页/游标若协议需要则由 SDK 封装；**暴露名 = `ToolPrefix` + MCP 原名**（或配置别名表）。  
3. **Schema**：MCP `inputSchema` **归一化后**与本地工具走 **同一套** Lark `arkToolParametersForAPI`（见 `doc/design/mcp-tool-unification.md`）。  
4. **生命周期**：SDK 返回 **`close` / `Stop` 回调**（或 **`io.Closer`**），与 **session** 同生命周期；Runner / Agent 构造路径在文档与示例中 **统一**展示「只传 profile + defer stop」。  
5. **实现落点**：内部仍可用手写 **`Handle` 闭包**或 **`ToolExecutor`**，但 **属于 SDK 实现细节**，不暴露在「集成示例」中给使用方照抄。  
6. **调试面对外 API（独立 `debug` 子包）**：除 **`pkg/mcp.BootstrapToolInfos`** 外，SDK 在 **`loopforge/debug`**（`package debug`，建议 import 别名 **`lfdebug`**）中暴露 **可独立调用** 的 MCP 调试能力（不经过 Agent loop），与 **集成 API** 分 package，避免使用方 **分不清「接 Agent」与「纯协议排障」**。以 **会话句柄**（示意名 **`Conn`**，由 **`Dial` 返回**）封装 **`github.com/modelcontextprotocol/go-sdk/mcp`** 的 **`ClientSession`**，并至少提供：  
   - **`Ping(ctx)`** — 对应协议 **`ping`**，用于「链路是否存活 / 握手是否完成」的快速检查；  
   - **`ListTools(ctx)`** — 对应 **`tools/list`**，返回 **MCP 原名 + 元数据**（可与 `BootstrapToolInfos` 共用解析逻辑），便于对照 **LIST** 与绑定结果；  
   - **`CallTool(ctx, name, arguments)`** — 对应 **`tools/call`**，按 **MCP 工具原名**（非带前缀暴露名）调用，便于 **CALL TOOL** 级联调；  
   可选后续扩展：**`ListResources` / `ReadResource`**、**`ListPrompts` / `GetPrompt`** 等同构封装，但不阻塞 v0.6.0 最小调试面。

### 代码示例（示意，与仓库类型对齐）

**1）现有：本地工具 = `ToolInfo` + `Handle`（与 `variable.VarSetTool` 同构）**

```go
localTool := &model.ToolInfo{
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
		// parse argumentsJSON, run logic, return result string
		return argumentsJSON, nil
	},
}
```

**2）现有：`tool.Invoke` 解析链（新增 MCP 时可插入 `ToolExecutor` 或扩展 `Invoke`）**

```go
// pkg/tool/dispatch.go — order today:
// 1) ToolCallPart.Handle
// 2) ToolInfo.Handle for matching name
// 3) ToolExecutor.Execute(name, argumentsJSON)
content, err := tool.Invoke(ctx, infos, executor, tc)
```

**3）目标：使用方只传 MCP 配置，SDK 自动发现并绑定（集成示例形态）**

函数名 **`BootstrapToolInfos`**（或等价名，实现于 v0.6.0）表示 **SDK 内部**完成：`NewClient` → `Connect` → **`ListTools`** → 映射 **`MCPMappedTool`** → 生成 **`[]*model.ToolInfo`**（内部已挂好 **`Handle` 或 `Executor` 路由**，并做 Ark schema 归一化）。**集成代码**中 **不** 再直接调 `go-sdk` 的 `ListTools` 来拼装绑定；若需肉眼核对列表，用 **`mcpdebug.Dial` + `Conn.ListTools`**（§**6）**，**勿**从 **`pkg/mcp` 根包** 拉调试 RPC。

```go
import (
	"context"

	"loopforge/pkg/agent"
	"loopforge/pkg/model"
	larkadapter "loopforge/pkg/model/adapters/lark"
	mcpcfg "loopforge/pkg/mcp/cfg"
	mcptools "loopforge/pkg/mcp" // proposed: BootstrapToolInfos lives here
)

func ExampleUserOnlySuppliesProfile(ctx context.Context, apiKey, modelName string) (ag *agent.Agent, stop func(), err error) {
	chat := larkadapter.NewLarkChatModel(apiKey, "", modelName)

	profile := mcpcfg.MCPServerProfile{
		ID:         "seraglf",
		Transport:  mcpcfg.MCPTransportStdio,
		Command:    []string{"path/to/seraglf-gateway", "--mcp-transport", "stdio"},
		ToolPrefix: "seraglf__",
	}

	mcpTools, stop, err := mcptools.BootstrapToolInfos(ctx, profile)
	if err != nil {
		return nil, nil, err
	}

	local := []*model.ToolInfo{ /* user-defined local tools only */ }

	ag = agent.New(chat,
		agent.WithName("main"),
		agent.WithToolInfos(append(local, mcpTools...)),
		agent.WithMaxSteps(32),
	)
	return ag, stop, nil
}
```

多 Server 时可为 **`BootstrapToolInfos(ctx, profiles ...MCPServerProfile)`** 或 **`BootstrapAll(ctx, []MCPServerProfile)`**；**仍只传配置**，由 SDK 维护多 session 与合并后的工具表。

**4）SDK 内部（实现者参考，非使用方必写）**

`BootstrapToolInfos` 内部等价于：对每条 MCP tool 构造 **`MCPMappedTool`**，再生成 **`ToolInfo`**（`Handle` 闭包内 **`ClientSession.CallTool`**，或单一 **`ToolExecutor`** + 路由表）。该逻辑 **封装在 `pkg/mcp`**，**不**出现在应用集成代码中。

**5）与 `bindModel` 的关系**

`Agent` 启动时 **`mergedToolInfos` + `tool.ValidateBindings`** 不变；**MCP 侧**由 **`BootstrapToolInfos`** 已生成满足校验的 **`ToolInfo`/`Executor` 组合**，使用方只做 **`append(local, mcpTools...)`**。

**6）调试：对外 `Ping` / `ListTools` / `CallTool`（不经 Agent，仅 `loopforge/debug`）**

与 **`pkg/mcp.BootstrapToolInfos`** 分列不同包：**集成**用根包，**排障**用 **`loopforge/debug`**（import 别名建议 **`lfdebug`**）。会话 API 名称为示意，与 `go-sdk` **`ClientSession`** 对齐。

```go
import (
	"context"

	mcpcfg "loopforge/pkg/mcp/cfg"
	lfdebug "loopforge/debug" // Ping / ListTools / CallTool; failures log at WARN/ERROR
)

func ExampleDebugMCP(ctx context.Context) error {
	profile := mcpcfg.MCPServerProfile{
		ID: "seraglf", Transport: mcpcfg.MCPTransportStdio,
		Command: []string{"path/to/seraglf-gateway", "--mcp-transport", "stdio"},
	}
	conn, stop, err := lfdebug.Dial(ctx, profile)
	if err != nil {
		return err
	}
	defer stop()

	if err := conn.Ping(ctx); err != nil {
		return err
	}
	tools, err := conn.ListTools(ctx)
	if err != nil {
		return err
	}
	_ = tools // inspect MCP tool names and schemas

	_, err = conn.CallTool(ctx, "some_mcp_tool_name", map[string]any{"query": "debug"})
	return err
}
```

**说明**：调试 API 使用 **MCP 协议原名**；**带前缀的暴露名**仅出现在发给 LLM 的 **`ToolInfo.Name`** 中。若需「按暴露名调用」，可在 SDK 内提供辅助 **unmap**，或文档约定调试路径一律用 **原名**。

详细路由与 Ark 单出口见 **`doc/design/mcp-tool-unification.md`**（§1.3 使用方 API 约束、§1.4 **`loopforge/debug`** 调试面）。

---

## 本版本目标

1. **MCP 客户端能力**：在 loopForge 进程内实现可配置的 **MCP 连接**（至少支持 **`stdio`** 与 **Streamable HTTP**，与 `pkg/mcp/cfg` 中 `MCPTransportKind` 对齐），完成 **session 生命周期**（启动、重连策略以「最小可用」为准）、**`tools/list` 发现**、**`tools/call` 调用** 与结果回注到现有消息流。
2. **与引擎统一工具面**：将 MCP 返回的工具定义映射为 **`pkg/model` 的 `ToolInfo` 形态**（名称、描述、JSON Schema 参数），经 **工具前缀 / Server ID**（如 `seraglf__<tool>`）避免多 Server 撞名；在 **单 Agent `RunLoop`** 中与本地工具、`var_set`、transfer 工具 **同一套调用与校验路径**（见 `doc/design/abstractions.md`、`multi-agent-engine.md` §3.7）。**集成侧只提交 `MCPServerProfile`（等连接信息）**，由 SDK **`ListTools` + 自动绑定**，不要求使用方逐工具手工注册。
3. **Runner / 配置入口**：提供明确配置面（环境变量、YAML 或代码 Options，实现前定稿），使 **`runner.NewRunner`** 或等价入口能挂载 **一个或多个 MCP Server**，并在每轮 LLM 前将 **合并后的 tools** 交给 `ToolCallingChatModel`（含 Lark 适配器侧 schema 兼容策略复用 v0.5.2 经验）。
4. **可观测**：至少具备 **结构化日志**（连接 id、暴露名、MCP 工具原名、latency、错误）；若排期允许，在现有事件模型上增加 **MCP 相关 RuntimeEvent 类型或子类型**（与 `data-fusion.md` 对齐，不强制本版本完成 Dev UI）。  
5. **调试面**：在 **`loopforge/debug`** 对外提供 **`Ping` / `ListTools` / `CallTool`**（及 **`Dial` + `stop`**），与 **`pkg/mcp`** 集成 API **分包**，使使用方 **不经 Agent** 即可验证 MCP 连接与工具（见上文 §Tools **6）**）。
6. **测试与验收 MCP：SeRagLF（seraglf）**  
   - **参考实现**：工作区内 **SeRagLF** 项目的 **`gateway/`**（Go module **`seraglf`**）已实现基于 **`github.com/modelcontextprotocol/go-sdk`** 的 MCP Server（stdio / HTTP 等，以该仓库当前 `cmd/gateway` 与配置为准）。  
   - **MCP Client 选型（与 SeRagLF 对齐）**：loopForge 侧 **MCP 客户端** 采用同一 **`github.com/modelcontextprotocol/go-sdk`**，主入口包 **`github.com/modelcontextprotocol/go-sdk/mcp`**。官方 README 写明该包为 **构建与使用 MCP client 与 server 的主要 API**：`mcp.NewClient` → `Client.Connect` → **`ClientSession`**，会话上提供 **`ListTools` / `CallTool`**（对应协议 `tools/list`、`tools/call`）及 prompts/resources 等；传输层使用 `CommandTransport`（stdio 子进程）、Streamable HTTP 等，与 v0.6.0 传输目标一致。  
   - **本版本验收标准**：loopForge 作为 **MCP Client**，能连接 **本地或 CI 中启动的 seraglf 进程**，成功完成 **至少一次** `tools/list` → 映射 → 模型可选调用 → **`tools/call`** → 结果回注；文档中记录 **所需环境变量、启动命令、与 loopForge 的对接示例**（可放在 `doc/acceptance/v0.6.0-acceptance.md`）。  
   - **说明**：SeRagLF 提供 **Self-RAG / 记忆 / 检索** 等领域工具；loopForge **不**在本版本实现向量或 RAG 内核，仅验证 **协议与工具链闭环**。

---

## 核心概念

### 为何以 SeRagLF（seraglf）为测试锚点

- 设计与 **`doc/decision/sdk-selection.md`** 一致：领域能力经 **MCP** 外接，**SeRagLF** 为仓库内已有、协议真实的 **MCP Server** 实现。  
- **seraglf** 作为 **Go module 名**与资源 URI 前缀（如 `seraglf://config`）在 SeRagLF 侧已使用；loopForge 侧工具前缀建议与之 **可配置对齐**，避免硬编码耦合。

### 与现有代码的边界

| 区域 | v0.6.0 预期 |
|------|-------------|
| `pkg/mcp/cfg` + **`pkg/mcp`（拟新增）** | 已有 `MCPServerProfile` / `MCPMappedTool`；实现 **`BootstrapToolInfos`** 等 **集成契约**；集成路径 **不** 要求手写 MCP 工具表。 |
| **`loopforge/debug`** | **仅**承载 **`Dial`/`Conn`** 与 **`Ping` / `ListTools` / `CallTool`**，供联调与测试；**不得**与 `pkg/mcp` 集成 API 混在同一 import 路径，以免与集成意图混淆。 |
| `pkg/tool` | 扩展 **Invoke** 路径：根据 tool 来源分发到 **本地 Handle** 或 **MCP Client**（与现有 `ToolExecutor` 行为兼容）。 |
| `pkg/model/adapters/lark` | 继续作为默认 LLM；tool **parameters** 发往 Ark 前 **沿用 v0.5.2 schema 清理**（`arkToolParametersForAPI` 等）；MCP 映射结果 **必须经同一路径** 再请求模型。 |
| Agent / Runner | **不**要求本版本实现 **spawn** 或完整 **Memory 预算**（v0.5.0 路线图中部分项仍可按排期顺延）。 |

### MCP 工具抽象与 Lark / Ark 兼容（参考 CloudWeGo Eino）

SeRagLF 内领域编排使用 **Eino**，便于对照阅读其 **工具抽象**与 **参数描述方式** 及 loopForge 侧 **MCP → `pkg/model.ToolInfo`** 的映射关系；**loopForge 不依赖 Eino 等第三方 SDK**，**仅作思路参考**，实现上重在 **分层与归一化**（与 SeRagLF 是否使用 Eino **无模块依赖关系**）。

| 参考位置（Eino） | 要点 | 与 v0.6.0 的关联 |
|------------------|------|------------------|
| `components/tool/interface.go` | **`BaseTool`**：`Info(ctx) (*schema.ToolInfo, error)` 供模型意图识别；**`InvokableTool`**：以 **`argumentsInJSON` 字符串**执行，与「声明 / 执行」分离 | MCP 侧对应 **`tools/list` 元数据** 与 **`tools/call` 入参**；loopForge 保持 **统一 Tool 面**：暴露名 + schema → 模型；路由到 MCP 时用 **原名 + JSON 参数**。 |
| `schema/tool.go`（`ToolInfo`） | 参数通过 **`ParamsOneOf`**：**结构化 `ParameterInfo`** 或 **显式 JSON Schema**，再 **`ToJSONSchema()`** 得到发给模型的子集 | MCP `inputSchema` 往往 **关键词更全**（易触发 Ark 400）；映射时应 **解析后归一化** 为与现有 **`ToolInfo.Parameters`**（`map[string]interface{}`）兼容的形态，再进入 **`pkg/model/adapters/lark`** 已有 **strip / ensure types**（与 v0.5.2 一致），避免重复造轮子或漏清理由。 |

**结论**：Eino 的「**可控参数描述 + JSON 字符串执行**」与 loopForge 现有 **Lark 适配器清洗链** 叠加，可系统覆盖文档中的 **Lark / Ark 工具 schema 兼容**问题；实现 MCP 工具注册时优先对齐这一分层，而非把原始 MCP JSON Schema 未经处理传给 Ark。

### 模块关系（示意）

```
  loopForge (Runner + Agent loop)
        │
        ├─ ToolInfos (local, var_set, transfer, …)
        │
        └─ MCP connector manager ──tools/list / tools/call ──▶ SeRagLF (seraglf MCP Server)
```

---

## 交付清单

- [ ] **MCP Client**：`stdio` + **Streamable HTTP** 至少一种用于 SeRagLF 联调（另一种可作为同构扩展）。  
- [ ] **发现与映射**：`tools/list` → `MCPMappedTool` / `ToolInfo`，**前缀与 allowlist**（最小：可配置 `ToolPrefix` + 可选按名过滤）；对外暴露 **`BootstrapToolInfos(profile...)`**（名待定），**集成示例仅含 profile，无手动绑定**。  
- [x] **调试 API**：在 **`loopforge/debug`** 提供 **`Dial`/`Conn`** 与 **`Ping` / `ListTools` / `CallTool`**（协议 **`ping` / `tools/list` / `tools/call`**），供使用方 **不经 Agent** 联调；与 **`pkg/mcp.ConnectClientSession`** / **`BootstrapToolInfos`** 共享连接实现；异常路径 **WARN/ERROR** 日志。  
- [ ] **调用链**：模型 `tool_calls` → 路由至 MCP `tools/call` → 文本/结构化结果写入 **tool 消息** 并进入下一轮。  
- [ ] **配置与示例**：最小 **loopforged / demo** 或 **test-server** 配置片段，可一键对接 **本地 seraglf**。  
- [ ] **测试**：**集成测试** 可对 seraglf 使用 **子进程 / 录制**（无 Key 的 smoke 或 mock）；单元测试覆盖映射与路由。  
- [ ] **文档**：`doc/acceptance/v0.6.0-acceptance.md`；必要时更新 `doc/design/multi-agent-engine.md` §3.7 的实现状态指针。

---

## 风险与待决

| 项 | 说明 |
|----|------|
| SDK 选型 | **已决**：Client 与 Server 均使用官方 **`github.com/modelcontextprotocol/go-sdk`**，与 SeRagLF 同源对齐；实现时锁定 **`mcp` 包版本**与 **MCP 协议修订版**（见 SDK README 兼容表），并保证 **stdio / Streamable HTTP** 与 gateway 侧配置可联调。 |
| SeRagLF 依赖 | 完整 seraglf 可能依赖 **向量库 / MySQL / 配置**；验收可约定 **最小子集工具**（如 health / 轻量 tool）降低环境成本。 |
| 工具 schema | 不同 MCP 工具 JSON Schema 复杂度差异大；**统一收口**：MCP → `ToolInfo` 映射时做 **结构归一化**（**思路**上可对齐 Eino `schema.ToolInfo` / `ParamsOneOf`，**不引入** Eino 等第三方 SDK），再 **必走** `pkg/model/adapters/lark` 的 Ark **sanitize**（v0.5.2 规则并按需扩展）。 |
| 范围控制 | **spawn、长期 Memory 预算、多 Agent 并行** 若不纳入 v0.6.0，须在本文档与路线图中 **显式顺延**。 |

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-16 | 初稿：v0.6.0 版本计划（MCP 支持；联调与验收锚定 SeRagLF / `seraglf`）。 |
| 2026-04-17 | 补充：MCP Client 采用官方 `go-sdk/mcp`；MCP 工具分层与 schema 归一化 **思路** 可参考 Eino（**不依赖** Eino 等第三方 SDK），并与 Lark 适配器 Ark sanitize 链对齐。 |
| 2026-04-17 | 补充：**Tools** 现状与改造计划；明确 **使用方只传 MCP profile、SDK 自动 ListTools 与绑定**（`BootstrapToolInfos` 形态）。 |
| 2026-04-17 | 补充：对外 **调试 API**（`Ping` / `ListTools` / `CallTool`、`Dial`+`stop`），与 `mcp-tool-unification` §1.4 对齐。 |
| 2026-04-17 | 审阅：澄清 **集成路径** 与 **调试 `ListTools`** 关系；**`ValidateBindings` vs `ExtraTools`**；参考文档指向 §1.3 / §1.4。 |
| 2026-04-17 | 约定：**`Ping` / `ListTools` / `CallTool`** 放在 **`loopforge/debug`**，与 **`pkg/mcp`** 集成分离。 |

---

## 参考文档

- `doc/design/abstractions.md` — Tool / MCP 契约、`MCPServerProfile`  
- `doc/design/mcp-tool-unification.md` — MCP 与本地 function call **统一技术方案**（§1.3 使用方约束、§1.4 **`loopforge/debug`**、路由与 schema、Eino 概念对照）  
- `doc/design/multi-agent-engine.md` — §3.7 MCP 接入、阶段性目标 **I**  
- `doc/decision/sdk-selection.md` — 与 SeRagLF 的边界  
- `SeRagLF/doc/decision/tech-selection.md` — MCP Go SDK 版本与约束（**实现时以该仓库为准**）  
- 外部思路参考（**非 loopForge 依赖**）：`github.com/cloudwego/eino` — `components/tool/interface.go`（`BaseTool` / `InvokableTool`）、`schema/tool.go`（`ToolInfo`、`ParamsOneOf` / `ToJSONSchema`），对照 **MCP 工具声明与执行分层**、**参数 schema 归一化**（与 Lark 侧兼容策略衔接）

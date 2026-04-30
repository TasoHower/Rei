# loopForge MCP 工具集成

> 本文档描述 loopForge 如何通过 **Model Context Protocol (MCP)** 将远端服务的能力以工具形式集成到 Agent loop 中。MCP 工具与本地 Go 函数工具共享同一套 `ToolInfo` 契约，对 LLM 完全透明。
>
> **前置阅读**：[04-tools.md](04-tools.md)——Tool 系统作为通用框架，MCP 是其中一种工具来源。

---

## 1. MCP 是什么

MCP (Model Context Protocol) 是一个开放协议，定义 LLM 应用与外部服务之间的标准通信接口。MCP Server 是独立的进程或网络服务，对外暴露 tools / resources / prompts 三种能力。loopForge 作为 MCP Client，主要消费 tools 列表。

核心链路：

```
 loopForge Agent (MCP Client)
       │
       │ BootstrapToolInfos → tools/list → 获取远端工具定义
       │ tool.Invoke   → tools/call → 调用远端工具
       │
       v
 MCP Server（独立进程 / 网络服务）
    ├── stdio：子进程 stdin/stdout JSON-RPC
    └── streamable_http：HTTP SS E 长连接
```

loopForge 对 MCP Server 的实现语言/框架无感知——只要遵守 MCP 协议即可。典型场景包括：

| 场景 | MCP Server |
|------|-----------|
| 检索增强 (RAG) | SeRagLF（Self-RAG + Qdrant 向量检索） |
| 文件系统 | filesystem server |
| 数据库 | postgres server |
| 浏览器操作 | playwright server |

---

## 2. MCPServerProfile

`pkg/mcp/cfg/profile.go`：

```go
type MCPServerProfile struct {
	ID            string             // 服务唯一标识
	Transport     MCPTransportKind   // MCPTransportStdio | MCPTransportStreamableHTTP
	Command       []string           // stdio 模式：子进程命令行
	URL           string             // HTTP 模式：服务 URL
	Env           map[string]string  // 环境变量（透传给子进程或 HTTP header）
	Headers       map[string]string  // HTTP 请求头
	ToolPrefix    string             // 工具名前缀（跨 Server 防冲突，如 "seraglf__"）
	ToolAllowlist []string           // 允许暴露的工具名白名单（空=全部允许）
}
```

### 2.1 两种传输模式

| 模式 | 常量 | 适用场景 |
|------|------|---------|
| stdio | `MCPTransportStdio` | 本地子进程，`Command` 字段指定二进制路径和参数 |
| streamable_http | `MCPTransportStreamableHTTP` | 远端服务或同机不同端口，`URL` 字段指定地址 |

### 2.2 工具前缀与名称冲突

当多个 MCP Server 暴露同名工具时（如多个 Server 都有 `search` 工具），通过 `ToolPrefix` 确保 LLM 可见的工具名不冲突：

```
MCP ToolName: "search"
ToolPrefix:   "seraglf__"
ExposedName:  "seraglf__search"   ← LLM 看到这个名字
```

若 `ToolPrefix` 为空，暴露名等于 MCP 原名。为避免已有 prompt 依赖原名，当 `ToolPrefix` 非空时，`BootstrapToolInfos` 额外注册原名别名，使两种名字均可调用。

### 2.3 挂载到 Agent

```go
ag := agent.New(chat,
	agent.WithMCPServerProfiles(cfg.MCPServerProfile{
		ID:         "seraglf",                      // 服务唯一标识
		Transport:  cfg.MCPTransportStreamableHTTP,
		URL:        "http://127.0.0.1:8080/mcp",
		ToolPrefix: "seraglf__",                    // 前缀防冲突
	}),
)
```

RunLoop 在每次 `bindModel` 时自动执行 `mcp.BootstrapToolInfos`，无需手动管理连接。

---

## 3. ConnectClientSession

`pkg/mcp/connect.go`：

```go
func ConnectClientSession(ctx context.Context, profile cfg.MCPServerProfile) (*sdkmcp.ClientSession, func(), error)
```

建立与 MCP Server 的会话连接：

```
ConnectClientSession(ctx, profile)
  │
  ├── transportForProfile(profile)
  │     ├── stdio：exec.Command → subprocess stdin/stdout
  │     └── streamable_http：HTTP URL + 可选 headers
  │
  ├── sdkmcp.NewClient(impl, nil)
  │
  ├── client.Connect(ctx, transport, nil)
  │     → 握手协商 (initialize + capabilities)
  │
  └── 返回 (session, closeFunc, nil)
```

`closeFunc` 关闭 MCP 会话（stdio 模式下同时终止子进程）。loopForge 在 `RunLoop` 执行完毕后通过 `defer resetMCPSession()` 统一释放资源。

---

## 4. BootstrapToolInfos

`pkg/mcp/bootstrap.go`：

```go
func BootstrapToolInfos(ctx context.Context, profiles ...cfg.MCPServerProfile) ([]*model.ToolInfo, func(), error)
```

这是将 MCP 远端工具转换为本地 `ToolInfo` 列表的核心函数。返回值含两个部分：

- `[]*model.ToolInfo`：可供模型调用的工具列表
- `func()`：清理函数，关闭所有连接和子进程

### 4.1 内部流程

```
BootstrapToolInfos(ctx, profiles...)
  │
  ├── 对每个 profile：
  │     │
  │     ├── 1. ConnectClientSession(ctx, profile)
  │     │       建立 MCP 连接，失败 → 关闭已建立的连接 → 返回 error
  │     │
  │     ├── 2. session.ListTools()（支持分页）
  │     │       拉取远端完整工具列表，失败 → 同上
  │     │
  │     ├── 3. 对每个 MCP tool：
  │     │     ├── AllowedByAllowlist(p.ToolAllowlist, toolName)
  │     │     │       白名单过滤：空 = 全部通过，非空 = 精确匹配
  │     │     │
  │     │     ├── BuildMappedToolParts(serverID, prefix, name, desc, inputSchema)
  │     │     │       生成暴露名 = ToolPrefix + MCPToolName
  │     │     │       归一化 MCP inputSchema → JSON Schema
  │     │     │
  │     │     ├── 构造 ToolInfo：
  │     │     │       Handle = func(ctx, argsJSON) { callMCPTool(session, mcpName, argsJSON) }
  │     │     │       ← 闭包持有 session 和 mcpToolName
  │     │     │
  │     │     └── 若 ToolPrefix 非空：
  │     │           注册原名别名：dup.Name = mcpName
  │     │           （MCP 协议原名也可调用）
  │     │
  │     └── 汇总到 all ToolInfos
  │
  └── 返回 (all, stopAll, nil)
```

### 4.2 Handle 闭包内部

```go
// bootstrap.go L90-L92
handle := func(ctx context.Context, argumentsJSON string) (string, error) {
	return callMCPTool(ctx, sess, mcpName, argumentsJSON)
}
```

`callMCPTool` 将 JSON 参数字符串解析为 `map[string]any`，调用 `session.CallTool(ctx, mcpName, args)`，然后将返回结果序列化为 JSON 字符串。

MCP 工具对 `tool.Invoke` 完全透明——它只是一个普通的闭包，落在三级分派的第 2 优先（`info.Handle`）。

### 4.3 生命周期

```
RunLoop 启动
  │
  ├── bindModel(ctx, ...)
  │     ├── resetMCPSession()           // 清理上一轮
  │     ├── BootstrapToolInfos(...)      // ★ 重新连接 + ListTools
  │     └── WithTools(all)
  │
  ├── RunLoop 执行 ...
  │
  └── RunLoop 结束
        └── defer resetMCPSession()      // 关闭连接
```

每次 `RunLoop` 都会重新 Bootstrap，确保工具列表与 MCP Server 状态同步。session 在 RunLoop 结束时通过 `defer a.resetMCPSession()` 自动释放。

---

## 5. MCP 在工具体系中的位置

MCP 工具完全融入 loopForge 的通用工具体系：

```
tool.Invoke 三级分派（见 04-tools.md §5.2）：
  ① tc.Handle       ← spawn_subagent 等内置工具
  ② info.Handle     ← 本地 Go 函数 + MCP 工具的 handle 闭包 ★
  ③ ex.Execute      ← ToolExecutor 兜底
```

MCP 工具的 handle 闭包落在 ②，与本地 Go 函数完全对等——`tool.Invoke` 不知道也不关心它是本地还是远端。

---

## 6. MCP 调试包

`loopforge/debug` 提供不经 Agent 的 MCP 直调能力，用于连接验证和问题排查：

```go
import lfdebug "loopforge/debug"    // 别名 lfdebug 避免与 runtime/debug 冲突

conn, stop, err := lfdebug.Dial(ctx, cfg.MCPServerProfile{
	ID:        "probe",
	Transport: cfg.MCPTransportStreamableHTTP,
	URL:       "http://127.0.0.1:8080/mcp",
})
defer stop()

conn.Ping(ctx)                                                        // 1. 探活
tools, _ := conn.ListTools(ctx)                                       // 2. 列出 MCP Server 工具（含分页）
res, _ := conn.CallTool(ctx, "mcp_original_name", map[string]any{})   // 3. 直接用 MCP 原名调用
```

关键区别：
- `ListTools` 返回 MCP 协议端工具名（如 `retrieve`），与 `BootstrapToolInfos` 转换后的暴露名（如 `seraglf__retrieve`）不同
- `CallTool` 使用 MCP 协议端工具名，非暴露名

### 6.1 Conn 结构体

```go
type Conn struct {
	session   *sdkmcp.ClientSession
	serverID  string
	transport cfg.MCPTransportKind
}
```

### 6.2 返回值

`CallTool` 返回 `*CallToolOutcome`：
```go
type CallToolOutcome struct {
	IsError           bool   // MCP Server 报告的错误
	Text              string // text content 拼接
	StructuredContent any    // structured content（原始对象）
}
```

---

## 7. 与 Agent 的绑定关系

```
Agent.MCPServerProfiles []cfg.MCPServerProfile
  │
  │ (每次 RunLoop 时)
  v
bindModel:
  ├── mcp.BootstrapToolInfos(ctx, profiles...)
  │     └── → mcpToolInfos ([]*model.ToolInfo) + mcpStop
  │
  ├── mergedToolInfos(extraRuntime...)
  │     合并顺序：ToolInfos → mcpToolInfos → ExtraTools → extraRuntime
  │
  ├── ValidateBindings(ToolInfos + mcpToolInfos, Executor)
  │     确保所有本地和 MCP 工具都有可执行的 Handle
  │     （ExtraTools / varTools 不参与验证）
  │
  └── ChatModel.WithTools(allTools)
```

---

## 8. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/mcp/bootstrap.go` | BootstrapToolInfos（连接→发现→映射为 ToolInfo） |
| `pkg/mcp/connect.go` | ConnectClientSession（建立/关闭 MCP 会话） |
| `pkg/mcp/cfg/profile.go` | MCPServerProfile、MCPMappedTool、MCPTransportKind |
| `pkg/mcp/mapping.go` | BuildMappedToolParts、AllowedByAllowlist、ExposedToolName |
| `pkg/mcp/schema.go` | MCP inputSchema → JSON Schema 归一化 |
| `pkg/mcp/toolinfo.go` | ToToolInfo 构造 |
| `pkg/agent/helpers.go` | bindModel（内含 MCP Bootstrap 调用） |
| `pkg/agent/options.go` | WithMCPServerProfiles |
| `debug/dial.go` | 调试包：Dial → Ping / ListTools / CallTool |

---

## 9. 相关文档

| 文档 | 关系 |
|------|------|
| [04-tools.md](04-tools.md) | 通用工具系统框架——理解 ToolInfo、Invoke、executeToolCalls 后再看 MCP 集成 |
| [01-agent-core.md](01-agent-core.md) §5 | RunLoop 中 bindModel 的调用时机 |
| [00-abstractions.md](00-abstractions.md) §13 | MCPServerProfile 的代码级定义 |
| `doc/design/mcp-tool-unification.md`（待修订） | MCP 工具与本地函数工具的统一方案 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：MCP 协议概述、MCPServerProfile 两种传输模式、ConnectClientSession、BootstrapToolInfos 全流程、调试包、在工具体系中的位置。 |

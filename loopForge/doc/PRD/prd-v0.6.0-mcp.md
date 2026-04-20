# loopForge 产品需求文档 — v0.6.0 MCP 客户端与工具接入

> **版本**：v0.6.0  
> **日期**：2026-04-16  
> **里程碑**：MCP（Model Context Protocol）客户端与工具接入  
> **状态**：计划稿  
> **关联文档**：[progress-v0.6.0.md](../log/progress-v0.6.0.md)、[mcp-tool-unification.md](../design/mcp-tool-unification.md)

---

## 1. 概述

### 1.1 产品定位

**MCP 客户端与工具接入** 是 loopForge v0.6.0 的核心功能，通过在 Agent loop 内将远端 MCP tools 与本地 `ToolInfo` 统一暴露，实现**工具发现的自动化**和**执行路径的统一**。本版本以 **SeRagLF（`seraglf`）为参考 MCP Server** 进行联调与验收。

### 1.2 核心价值

1. **自动化发现** — SDK 自动从 MCP Server 发现工具，无需手写 ToolInfo
2. **统一抽象** — MCP tools 与本地 tools 使用同一套 `ToolInfo` 接口
3. **简化集成** — 使用方只提供 MCP 配置，SDK 完成所有绑定逻辑
4. **调试友好** — 独立的 debug API 用于链路排查，与集成 API 分离
5. **向后兼容** — 保持现有 `ToolInfo` + `Handle` 模式，MCP 作为新增来源

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.5.x** | Variable、Lark 适配器、Ark 工具 schema 兼容；v0.6.0 在此基础上接入 MCP |
| **v0.7.0** | Skills（技能/指令包）；v0.6.0 的 MCP tools 可作为技能的 `allowed-tools` 来源 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.6.0）

#### P0（必须实现）

1. **对外体验（硬性要求）**
   - 使用方**只提供 MCP 客户端侧配置**（如一条或多条 `MCPServerProfile`）
   - **不得**要求使用方在应用集成路径上自行调用底层 `ListTools`、手写每条 MCP 的 `ToolInfo` 列表
   - **由 loopForge SDK**（落点 `pkg/mcp`）在 `BootstrapToolInfos` 内完成：
     - 建连 → `ListTools` → 生成 `MCPMappedTool` → 归一化 schema → 生成带内部 `Handle` / 统一 `ToolExecutor` 的 `[]*model.ToolInfo`
     - 与本地工具合并并参与 `bindModel`

2. **发现与绑定（SDK 内部）**
   - 按 profile 创建 `mcp.Client` + `Connect` + `ClientSession`
   - 分页/游标若协议需要则由 SDK 封装
   - 暴露名 = `ToolPrefix` + MCP 原名（或配置别名表）

3. **Schema 归一化**
   - MCP `inputSchema` 归一化后与本地工具走同一套 Lark `arkToolParametersForAPI`
   - 对齐 `doc/design/mcp-tool-unification.md`

4. **生命周期管理**
   - SDK 返回 `close` / `Stop` 回调（或 `io.Closer`）
   - 与 session 同生命周期
   - Runner / Agent 构造路径在文档与示例中统一展示「只传 profile + defer stop」

5. **调试 API（独立 `debug` 子包）**
   - 除 `pkg/mcp.BootstrapToolInfos` 外，SDK 在 `loopforge/debug`（package `debug`，import 别名 `lfdebug`）中暴露可独立调用的 MCP 调试能力
   - 与集成 API 分 package，避免使用方分不清「接 Agent」与「纯协议排障」
   - 以会话句柄（示意名 `Conn`，由 `Dial` 返回）封装 `github.com/modelcontextprotocol/go-sdk/mcp` 的 `ClientSession`
   - 至少提供：
     - `Ping(ctx)` — 对应协议 `ping`，用于「链路是否存活 / 握手是否完成」的快速检查
     - `ListTools(ctx)` — 对应 `tools/list`，返回 MCP 原名 + 元数据（可与 `BootstrapToolInfos` 共用解析逻辑）
     - `CallTool(ctx, name, arguments)` — 对应 `tools/call`，按 MCP 工具原名（非带前缀暴露名）调用

#### P1（可选增强）

- 支持多 MCP Server 配置（优先级/故障转移）
- 工具缓存（减少重复 `ListTools`）
- 自动重连机制

### 2.2 不在本版本范围

1. **MCP Resources** — 仅支持 MCP Tools，Resources 留待后续版本
2. **MCP Prompts** — 仅支持 Tools，Prompts 与 v0.7.0 Skills 结合
3. **MCP Server 端开发** — 仅提供 Client SDK，Server 端由第三方实现
4. **工具编排** — 工具间的编排逻辑不在本版本范围

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够只配置 MCP Server 信息，SDK 自动完成工具发现和绑定  
**验收标准**：
- 创建 `MCPServerProfile` 配置（transport、URL、鉴权、`ToolPrefix`）
- 调用 `pkg/mcp.BootstrapToolInfos(ctx, profile)`
- 返回 `[]*model.ToolInfo`，可直接传入 `agent.WithTools()`
- 无需手写任何 `ListTools` 或 `ToolInfo` 构造代码

**US-2**：能够在调试时独立调用 MCP 协议 API  
**验收标准**：
- 调用 `loopforge/debug.Dial(ctx, profile)` 获取 `Conn`
- 使用 `Conn.Ping(ctx)` 检查链路
- 使用 `Conn.ListTools(ctx)` 查看 MCP 原始工具列表
- 使用 `Conn.CallTool(ctx, name, args)` 调用工具
- 调试 API 与集成 API 分离，互不干扰

**US-3**：能够将 MCP tools 与本地 tools 混合使用  
**验收标准**：
- 定义本地 `ToolInfo`（带 `Handle`）
- 调用 `BootstrapToolInfos` 获取 MCP tools
- 合并两者列表
- 调用 `agent.WithTools(append(localTools, mcpTools...))`
- 所有工具在 LLM 调用时统一暴露

**US-4**：能够控制 MCP tools 的命名和过滤  
**验收标准**：
- 配置 `ToolPrefix` 为 MCP tools 添加前缀
- 配置 allowlist 仅启用特定工具
- 配置别名表重命名工具

### 3.2 作为 Agent，我希望...

**US-5**：能够在 tool call 时自动路由到 MCP 工具  
**验收标准**：
- LLM 返回 tool call（名称匹配 MCP 工具暴露名）
- `tool.Invoke` 解析链路由到 MCP 工具的 `Handle`
- MCP SDK 执行 `CallTool` 协议
- 返回结果给 LLM

**US-6**：能够在 MCP Server 不可用时优雅降级  
**验收标准**：
- MCP 连接失败时记录错误日志
- 不影响本地工具的正常使用
- 可选择是否阻断 Agent 启动（配置项）

---

## 4. 功能需求

### 4.1 MCP Server Profile

#### 4.1.1 配置结构

```go
type MCPServerProfile struct {
    Name        string            // 配置名称（用于日志）
    Transport   string            // "stdio" 或 "sse"
    Command     string            // stdio 模式下的命令（如 "seraglf"）
    Args        []string          // stdio 模式下的参数
    URL         string            // SSE 模式下的 URL
    AuthToken   string            // 可选：鉴权 Token
    ToolPrefix  string            // 可选：工具名前缀（如 "mcp_"）
    Allowlist   []string          // 可选：允许的工具列表（为空表示全部）
    Aliases     map[string]string // 可选：工具名别名映射（MCP 原名 -> 暴露名）
}
```

#### 4.1.2 配置示例

**stdio 模式**：
```go
profile := &mcp.MCPServerProfile{
    Name:       "seraglf",
    Transport:  "stdio",
    Command:    "seraglf",
    Args:       []string{"--mode", "agent"},
    ToolPrefix: "mcp_",
}
```

**SSE 模式**：
```go
profile := &mcp.MCPServerProfile{
    Name:       "remote-mcp",
    Transport:  "sse",
    URL:        "http://localhost:8080/sse",
    AuthToken:  "Bearer xxx",
    ToolPrefix: "remote_",
}
```

### 4.2 BootstrapToolInfos

#### 4.2.1 函数签名

```go
// BootstrapToolInfos discovers MCP tools and binds them to local ToolInfos.
// Returns []*model.ToolInfo ready for agent.WithTools().
// The caller should defer stop() to cleanup the session.
func BootstrapToolInfos(ctx context.Context, profile *MCPServerProfile) (tools []*model.ToolInfo, stop func(), err error)
```

#### 4.2.2 内部流程

```
1. 创建 MCP Client（根据 Transport 选择 stdio 或 SSE）
        ▼
2. Connect 建立会话
        ▼
3. ListTools 获取工具列表
        ▼
4. 对每个 MCP Tool：
   - 应用 Allowlist 过滤
   - 应用 Aliases 重命名
   - 添加 ToolPrefix 前缀
   - 归一化 Schema（转为 OpenAI function JSON Schema）
   - 生成 MCPMappedTool（带 Handle 闭包）
        ▼
5. 返回 []*model.ToolInfo
        ▼
6. 调用方 defer stop() 清理会话
```

#### 4.2.3 Schema 归一化

```go
// MCP inputSchema 转 OpenAI function schema
func normalizeSchema(inputSchema map[string]any) map[string]any {
    // 1. 确保 type 字段存在
    // 2. 转换 properties 格式
    // 3. 处理 required 字段
    // 4. 移除 Ark 不支持的关键字
    // 5. 返回与本地工具同一套 schema
}
```

### 4.3 工具执行路径

#### 4.3.1 解析链（`pkg/tool/dispatch.go`）

```go
// tool.Invoke 解析顺序：
// 1. ToolCallPart.Handle（工具调用自带 Handle）
// 2. ToolInfo.Handle（匹配名称的 Handle）
// 3. ToolExecutor.Execute(name, argumentsJSON)（通用执行器）

func Invoke(ctx context.Context, infos []*ToolInfo, executor ToolExecutor, tc *ToolCall) (string, error) {
    // 1. 优先 ToolCallPart.Handle
    if tc.Handle != nil {
        return tc.Handle(ctx, tc.Arguments)
    }
    
    // 2. 查找 ToolInfo.Handle
    for _, info := range infos {
        if info.Name == tc.Name && info.Handle != nil {
            return info.Handle(ctx, tc.Arguments)
        }
    }
    
    // 3. 路由到 ToolExecutor（MCP 工具走这里）
    if executor != nil {
        return executor.Execute(ctx, tc.Name, tc.Arguments)
    }
    
    return "", fmt.Errorf("tool not found: %s", tc.Name)
}
```

#### 4.3.2 MCP 工具 Handle 生成

```go
// 在 BootstrapToolInfos 内部为每个 MCP 工具生成 Handle
handle := func(ctx context.Context, argumentsJSON string) (string, error) {
    // 从 context 获取 MCP ClientSession
    session := getSessionFromContext(ctx)
    
    // 调用 MCP CallTool 协议
    result, err := session.CallTool(ctx, mcpOriginalName, argumentsJSON)
    if err != nil {
        return "", err
    }
    
    // 返回结果（序列化）
    return marshalResult(result)
}
```

### 4.4 调试 API

#### 4.4.1 Conn 结构

```go
// Conn wraps an MCP ClientSession for debugging.
type Conn struct {
    session mcp.ClientSession
    // ...
}

// Dial connects to an MCP server and returns a Conn for debugging.
func Dial(ctx context.Context, profile *MCPServerProfile) (*Conn, func(), error)
```

#### 4.4.2 调试方法

```go
// Ping checks if the MCP server is alive.
func (c *Conn) Ping(ctx context.Context) error

// ListTools lists all tools from the MCP server (original names, no prefix).
func (c *Conn) ListTools(ctx context.Context) ([]mcp.ToolInfo, error)

// CallTool calls an MCP tool by its original name (no prefix).
func (c *Conn) CallTool(ctx context.Context, name string, arguments map[string]any) (mcp.ToolResult, error)
```

#### 4.4.3 使用示例

```go
import lfdebug "loopforge/debug"

// 调试模式：独立调用 MCP 协议
conn, stop, err := lfdebug.Dial(ctx, profile)
if err != nil {
    log.Fatal(err)
}
defer stop()

// Ping
if err := conn.Ping(ctx); err != nil {
    log.Printf("MCP server not alive: %v", err)
}

// List tools
tools, err := conn.ListTools(ctx)
if err != nil {
    log.Fatal(err)
}
for _, tool := range tools {
    log.Printf("Tool: %s - %s", tool.Name, tool.Description)
}

// Call tool
result, err := conn.CallTool(ctx, "search", map[string]any{"query": "hello"})
if err != nil {
    log.Fatal(err)
}
log.Printf("Result: %+v", result)
```

---

## 5. 技术需求

### 5.1 数据结构

#### 5.1.1 MCPMappedTool

```go
// MCPMappedTool wraps an MCP tool with normalized schema and Handle.
type MCPMappedTool struct {
    MCPName     string  // MCP 原名
    ExposeName  string  // 暴露名（ToolPrefix + MCPName 或 Alias）
    Description string
    Schema      map[string]any // 归一化后的 OpenAI function schema
    Handle      model.ToolCallHandler
}
```

#### 5.1.2 与 model.ToolInfo 的映射

```go
func (m *MCPMappedTool) ToToolInfo() *model.ToolInfo {
    return &model.ToolInfo{
        Name:        m.ExposeName,
        Description: m.Description,
        Parameters:  m.Schema,
        Handle:      m.Handle,
    }
}
```

### 5.2 依赖管理

#### 5.2.1 MCP SDK

```go
import (
    "github.com/modelcontextprotocol/go-sdk/mcp"
)
```

#### 5.2.2 版本兼容

- 兼容 MCP protocol version 2024-11-05
- 使用 `go-sdk` v0.1.0+

### 5.3 错误处理

#### 5.3.1 连接错误

```go
type ConnectionError struct {
    Profile string
    Err     error
}

func (e *ConnectionError) Error() string {
    return fmt.Sprintf("failed to connect to MCP server %q: %v", e.Profile, e.Err)
}
```

#### 5.3.2 工具调用错误

```go
type ToolCallError struct {
    ToolName string
    Err      error
}

func (e *ToolCallError) Error() string {
    return fmt.Sprintf("failed to call tool %q: %v", e.ToolName, e.Err)
}
```

### 5.4 观测性

#### 5.4.1 日志（slog）

```go
// 连接建立
slog.Info("MCP connected", "profile", profile.Name, "transport", profile.Transport)

// 工具发现
slog.Info("MCP tools discovered", "count", len(tools))

// 工具调用
slog.Debug("MCP tool called", "tool", toolName, "arguments", argumentsJSON)

// 错误
slog.Error("MCP tool call failed", "tool", toolName, "err", err)
```

#### 5.4.2 Trace（OpenTelemetry）

- MCP 连接 span
- ListTools span
- CallTool span（与 Agent run span 关联）

---

## 6. 验收标准

### 6.1 功能验收

详见 `../acceptance/v0.6.0-acceptance.md`

### 6.2 性能验收

- 启动时 MCP 工具发现时间 < 2s（100 个工具以内）
- 工具调用延迟 < 100ms（不含网络 RTT）
- 连接超时可控（默认 10s）

### 6.3 兼容性验收

- 与 SeRagLF MCP Server 联调通过
- 本地工具与 MCP 工具混合使用无冲突
- 调试 API 与集成 API 互不干扰

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **MCP Server 不可用** | 连接失败导致工具不可用 | 优雅降级、错误日志、配置是否阻断 |
| **Schema 不兼容** | MCP inputSchema 与 OpenAI function schema 差异 | 归一化层、兼容性测试 |
| **工具名冲突** | MCP 工具名与本地工具名重复 | ToolPrefix、别名映射、冲突检测 |
| **会话泄漏** | 未正确 cleanup 导致会话泄漏 | defer stop()、文档强调、示例代码 |
| **调试 API 滥用** | 使用方混淆调试 API 与集成 API | 分 package、文档说明、命名区分 |

---

## 8. 参考文档

- [progress-v0.6.0.md](../log/progress-v0.6.0.md) — 版本进度日志
- [mcp-tool-unification.md](../design/mcp-tool-unification.md) — MCP 工具统一抽象
- [v0.6.0-acceptance.md](../acceptance/v0.6.0-acceptance.md) — 验收标准
- [progress-v0.5.2.md](../log/progress-v0.5.2.md) — 前置版本
- [Model Context Protocol 官方文档](https://modelcontextprotocol.github.io/)

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-16 | v0.6.0 | 初始版本 |

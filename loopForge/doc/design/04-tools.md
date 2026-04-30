# loopForge 工具系统

> 本文档描述 loopForge 中**工具的注册、分发与执行机制**。Tool 是 LLM 调用外部计算能力的唯一入口——本地函数和远端 MCP 服务通过同一套 `ToolInfo` 契约进入 Agent loop。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md) §5——RunLoop 在每步 LLM 返回 tool_calls 后调用 `executeToolCalls`；[03-events.md](03-events.md) §2.6–§2.7——每次工具调用前后发射 `tool_call_start`/`tool_call_end` 事件。

---

## 0. 什么是 Tool / Function Call

### 0.1 本质：为 LLM 预留的物理机接口

LLM 是一个纯文本世界的存在——它只能接收文本，也只能输出文本。它不能直接读写文件、不能调取数据库、不能发送 HTTP 请求、不能操作本地进程。**Tool（也叫 Function Call）就是在宿主机上为 LLM 预留的一组物理机接口**。

这个接口的契约极简单：

```
LLM 输出: "我想调用 add 工具，参数 a=3, b=5"
       ↓
   物理机执行 → 3 + 5 = 8
       ↓
LLM 收到: "add 工具返回了 8"
```

整个过程有以下关键约束：

| 约束 | 说明 |
|------|------|
| **LLM 决定"何时调"** | 模型在自己的推理过程中判断"我需要外部信息/计算能力"，然后发起 tool_call |
| **LLM 决定"调什么"** | 模型从预先注册的工具列表中选择一个工具名 |
| **LLM 决定"传什么参数"** | 模型按照工具的 JSON Schema 生成参数 |
| **物理机负责"执行并返回结果"** | 代码在宿主机上运行，把返回值以字符串形式回注到 LLM 的对话历史中 |

也就是说：**LLM 拥有决策权（选哪个工具、传什么参数），但不具备执行权（它在 GPU 集群上跑，不在你的机器上）；宿主机拥有执行权（读写文件、查询数据库、发送网络请求），但由 LLM 驱动执行时机。**

### 0.2 为什么需要 Tool

| 场景 | 没有 Tool | 有 Tool |
|------|----------|--------|
| 数学计算 | LLM 凭记忆回答，容易出错 | 调用 `add/subtract/multiply/divide`，精确 |
| 检索文档 | LLM 凭训练数据回答（可能幻觉） | 调用 MCP 检索工具（如 SeRagLF），返回真实文档片段 |
| 文件操作 | 无法 | 调用 `read_file`/`write_file` 工具 |
| 代码执行 | 无法 | 调用 `execute_shell_script` 工具 |
| 变量持久化 | LLM 无状态 | 调用 `var_set`/`var_get` 工具 |

Tool 是 LLM 从"会说"到"会做"的桥梁。

### 0.3 loopForge 的工具模型

loopForge 中的工具分两类来源，对外部调用方透明：

```
                  ┌──────────────────────┐
                  │       LLM            │
                  │  (有决策权, 无执行权)   │
                  └─────────┬────────────┘
                            │ tool_calls(name + arguments JSON)
                            v
                  ┌──────────────────────┐
                  │    tool.Invoke       │
                  │   (三级分派, 见 §5.2)  │
                  └─────────┬────────────┘
                            │
            ┌───────────────┼───────────────┐
            │               │               │
            v               v               v
    ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
    │ 本地 Go 函数  │ │  MCP 远端工具 │ │  内置工具     │
    │ (同进程执行)  │ │ (独立进程/    │ │ (spawn/      │
    │              │ │  网络服务)    │ │  transfer等) │
    └──────────────┘ └──────────────┘ └──────────────┘
```

无论哪种来源，对 LLM 和 `executeToolCalls` 来说都是同一个接口：`func(ctx, argumentsJSON string) (resultJSON string, error)`。

---

## 1. 核心类型

### 1.1 ToolInfo：对 LLM 可见的工具定义

`pkg/model/types/toolinfo.go`：

```go
type ToolInfo struct {
	Name        string                 // 工具名（对 LLM 可见，调用时匹配此名）
	Description string                 // 工具描述（对 LLM 可见，影响模型调用策略）
	Parameters  map[string]interface{} // JSON Schema 参数定义
	Handle      ToolCallHandler `json:"-"` // 执行回调（Go 函数，接收 JSON 参数返回 JSON 结果）
}
```

### 1.2 ToolCallHandler：工具执行回调

```go
type ToolCallHandler func(ctx context.Context, argumentsJSON string) (string, error)
```

签名极简：接收上下文 + JSON 参数字符串，返回 JSON 结果字符串。无论是本地 Go 函数还是 MCP 远端调用，最终都通过同样的 `argumentsJSON → resultJSON` 契约执行。

### 1.3 ToolCallPart：一次具体的工具调用

`pkg/model/types/message.go`：

```go
type ToolCallPart struct {
	ID        string          // 工具调用唯一 ID（关联 ToolCallStart/ToolCallEnd 事件）
	Name      string          // 工具名
	Arguments string          // 参数字符串（JSON）
	Handle    ToolCallHandler `json:"-"` // 快捷执行回调（可在此直接调用，跳过 ToolInfo 匹配层）
}
```

`ToolCallPart` 在 LLM 返回的 `Message.ToolCalls` 中，由 `RunLoop` 取出后传给 `executeToolCalls`。

---

## 2. 工具注册体系

Agent 有三类工具来源，按注册方式区分：

| 来源 | 注册方式 | 参与 ValidateBindings | 对 LLM 可见 | 典型用途 |
|------|---------|----------------------|------------|---------|
| **本地工具** | `WithToolInfos(...)` | ✅ 是 | ✅ 是 | 自定义 Go 函数，如计算器 |
| **MCP 工具** | `WithMCPServerProfiles(...)` | ✅ 是 | ✅ 是 | 远端服务暴露的工具（SeRagLF 检索等），详见 [05-mcp-tools.md](05-mcp-tools.md) |
| **注入工具** | `WithExtraTools(...)` / 运行时 `varTools` | 🚫 否 | ✅ 是 | `transfer_to_*`（handoff）、`var_set`（变量）、`spawn_subagent` |

### 2.1 本地工具注册

调用方通过 `agent.WithToolInfos` 注册本地 Go 函数：

```go
local := []*model.ToolInfo{{
	Name:        "add",
	Description: "Add two numbers",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "number"},
			"b": map[string]interface{}{"type": "number"},
		},
		"required": []string{"a", "b"},
	},
	Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
		var args struct{ A, B float64 }
		json.Unmarshal([]byte(argumentsJSON), &args)
		return fmt.Sprintf("%f", args.A+args.B), nil
	},
}}

ag := agent.New(chat,
	agent.WithToolInfos(local),
)
```

### 2.2 MCP 工具注册

MCP（Model Context Protocol）工具通过 `agent.WithMCPServerProfiles` 挂载，RunLoop 在 `bindModel` 阶段自动连接远端服务并发现工具。详见 [05-mcp-tools.md](05-mcp-tools.md)。

### 2.3 注入工具（不参与 ValidateBindings）

Agent 在 `RunLoop` 的 `bindModel` 之前动态构造一批 `varTools`，作为 `extraRuntime` 参数传入 `mergedToolInfos`。这些工具**发给 LLM 但不参与验证**（`ValidateBindings` 只检查 `ToolInfos` + `mcpToolInfos`）：

- `var_set`：变量写入工具（`WithVariable()` 启用），详见 [06-变量.md](06-变量.md)
- `execute_shell_script`：技能脚本执行（`WithSkillShellTool` 启用）
- `load_skill`：动态加载技能（`WithLoadSkillTool` 启用）
- `spawn_subagent`：子 Agent 创建（`WithSpawn` 启用）

---

## 3. 工具合并顺序

`pkg/agent/helpers.go` 中 `mergedToolInfos` 按固定顺序合并：

```
mergedToolInfos 合并顺序：
  1. ToolInfos       （本地注册的工具）
  2. mcpToolInfos    （MCP 自动发现的工具）
  3. ExtraTools      （注入工具，如 transfer_to_*）
  4. extraRuntime    （每轮 RunLoop 动态构建的 varTools）
```

模型收到的 `WithTools` 列表按此顺序排列。执行时 `tool.Invoke` 按名称匹配，因此**同名工具以后注册者覆盖先注册者**（MCP 工具可覆盖本地工具）。

---

## 4. bindModel：工具绑定流程

每次 `RunLoop` 启动时，`bindModel` 执行以下步骤：

```
bindModel(ctx, extraRuntimeTools...)
  │
  ├── 1. resetMCPSession()           // 清理上一轮的 MCP 连接
  │
  ├── 2. 若 MCPServerProfiles 非空：
  │       mcp.BootstrapToolInfos(ctx, profiles...)
  │       → mcpToolInfos, mcpStop
  │
  ├── 3. mergedToolInfos(extraRuntimeTools...)
  │       → 按 §3 顺序合并全部工具
  │
  ├── 4. ValidateBindings(ToolInfos + mcpToolInfos, Executor)
  │       确保所有本地 + MCP 工具有 Handle 或 Executor
  │       （ExtraTools / varTools 不参与）
  │
  └── 5. ChatModel.WithTools(allTools)
          → 将工具列表注入 LLM 上下文
```

---

## 5. 工具执行

### 5.1 executeToolCalls

`pkg/agent/tools.go`：

```go
func executeToolCalls(
	ctx context.Context,
	infos []*model.ToolInfo,        // 全量工具列表（mergedToolInfos）
	executor tool.ToolExecutor,     // 备选执行器
	toolCalls []model.ToolCallPart,  // LLM 返回的 tool_calls 列表
	emit func(int, event.EventPayload), // 事件发射函数
	step int,                       // 当前步数
) ([]*model.Message, error)
```

遍历 LLM 返回的每个 `ToolCallPart`：

```
for each toolCall in toolCalls:
  1. 检查 ctx.Done() → 返回错误（支持取消）
  2. emit(tool_call_start) → 含 ToolCallID、Name、Arguments
  3. tool.Invoke(ctx, infos, executor, tc)
     ├── 成功 → 返回 resultJSON
     └── 失败 → 返回 *ToolError → emit(tool_call_end, IsError=true) → 终止
  4. emit(tool_call_end) → 含 ToolCallID、OK、Output
  5. 构造 role=tool 的消息 → 追加到结果列表

返回 tool 消息列表（追加到 messages）
```

### 5.2 tool.Invoke：三级分派

`pkg/tool/dispatch.go`：

```go
func Invoke(ctx context.Context, infos []*model.ToolInfo, ex ToolExecutor, tc model.ToolCallPart) (string, error) {
	// 第 1 优先：ToolCallPart 的快捷 Handle
	if tc.Handle != nil {
		return tc.Handle(ctx, tc.Arguments)
	}
	// 第 2 优先：在 ToolInfo 列表按 Name 匹配 Handle
	for _, info := range infos {
		if info == nil || info.Name != tc.Name { continue }
		if info.Handle != nil { return info.Handle(ctx, tc.Arguments) }
	}
	// 第 3 优先：工具执行器兜底
	if ex != nil { return ex.Execute(ctx, tc.Name, tc.Arguments) }
	// 无任何匹配 → ErrNoHandler
	return "", ErrNoHandler
}
```

| 优先级 | 来源 | 使用场景 |
|--------|------|---------|
| ① | `tc.Handle` | `spawn_subagent` 内置工具等直接在 ToolCallPart 上挂回调的场景 |
| ② | `info.Handle` | 本地工具、MCP 工具的 Handle 闭包 |
| ③ | `ToolExecutor` | 外部注册表的兜底（如 `MapToolExecutor`） |

---

## 6. 完整数据流

```
用户请求 → Agent.RunLoop
              │
              ├── bindModel(ctx, varTools...)
              │     ├── MCP Bootstrap → mcpToolInfos（详见 05-mcp-tools.md）
              │     ├── mergedToolInfos(ToolInfos + mcpToolInfos + ExtraTools + varTools)
              │     └── ChatModel.WithTools(allTools) → LLM 收到工具列表
              │
              ├── LLM 回复含 tool_calls:
              │     ├── ToolCallPart{ID, Name, Arguments}
              │     └── executeToolCalls(infos, executor, toolCalls, emit, step)
              │           │
              │           ├── emit(tool_call_start)
              │           ├── tool.Invoke(ctx, infos, executor, tc)
              │           │     ├── ① tc.Handle(ctx, argsJSON)
              │           │     ├── ② info.Handle(ctx, argsJSON)  ← MCP/本地工具在此命中
              │           │     └── ③ ex.Execute(ctx, name, argsJSON)
              │           ├── emit(tool_call_end)
              │           └── → role=tool message
              │
              └── role=tool message 追加到消息列表 → 下一轮 LLM 调用
```

---

## 7. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/model/types/toolinfo.go` | ToolInfo 结构体 |
| `pkg/model/types/message.go` | ToolCallPart 结构体、ToolCallHandler 类型 |
| `pkg/tool/dispatch.go` | Invoke（三级分派）、ValidateBindings |
| `pkg/tool/executor.go` | ToolExecutor 接口 |
| `pkg/tool/map.go` | MapToolExecutor 实现 |
| `pkg/agent/tools.go` | executeToolCalls（含事件发射） |
| `pkg/agent/helpers.go` | mergedToolInfos、bindModel |
| `pkg/agent/options.go` | WithToolInfos、WithMCPServerProfiles、WithExtraTools |

---

## 8. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) §5 | RunLoop 迭代步骤——何时调用 executeToolCalls |
| [03-events.md](03-events.md) §2.6–§2.7 | tool_call_start/tool_call_end 事件载荷 |
| [05-mcp-tools.md](05-mcp-tools.md) | MCP 工具集成详解 |
| [00-abstractions.md](00-abstractions.md) §10–§11 | ToolInfo 类型定义与 bindModel 合并逻辑 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：工具注册体系（三类来源）、bindModel 绑定流程、executeToolCalls 执行流程、Invoke 三级分派。 |
| 2026-04-30 | v0.9.5 | MCP 内容拆出至 05-mcp-tools.md，§6 缩减为引用；新增 06-变量.md 交叉引用。 |

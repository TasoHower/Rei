# Rei

> **Rei** 在本项目中与 **loopForge** 等价——它是 Multi-Agent 运行时引擎的名称
> 本项目遵循 Apache License 2.0 开源协议进行发布和维护

## 项目结构

```
Rei/
├── loopForge/      # Multi-Agent 运行时引擎（本项目核心）
└── test-server/    # Agent 调试测试服务器
```

> **SeRagLF** 曾作为 Self-RAG Demo 存在于 `SeRagLF/` 目录下，提供基于 Eino `compose.Graph` 的自反思检索增强生成 MCP Server。当前版本已彻底移除。

> **项目状态**：Rei (loopForge) 目前处于 **"Make it work"** 阶段——核心功能已可运行（Agent Loop、Tool Calling、Transfer、Spawn、MCP、Skills），但正确性契约与性能优化尚未系统展开。后续演进路线为 **Make it work → Make it right → Make it fast**。

**你能够在 loopForge/do 目录下找到本项目的所有文档**

## loopForge

Multi-Agent 运行时引擎，基于 OpenAI 兼容 API 构建，提供可控的 Agent 循环、工具调用、MCP 集成、Skills 注入和动态 spawn 能力。

**核心特性：**

- **Agent Loop**：单 Agent 迭代运行，支持 LLM 调用与工具执行的交替循环
- **Multi-Agent**：静态 Network（Parallel / Sequential / Competitive）+ 动态 spawn（受深度、并发、预算约束）
- **MCP 集成**：多 Server 管理、工具白名单与前缀隔离
- **Skills**：`SKILL.md` 解析与受信加载
- **可观测性**：OTel trace + metrics、token/cost 聚合

---

## Quick Start

### 前置条件

- Go 1.25+
- 至少一个 LLM API Key（任选一）：`OPENAI_API_KEY`、`DEEPSEEK_API_KEY`、`LARK_API_KEY`

### 启动调试服务器

```bash
# 克隆项目
git clone <repo-url>
cd rei

# 设置 LLM API Key（此处以 DeepSeek 为例）
export DEEPSEEK_API_KEY="sk-xxx"

# 启动 test-server（HTTP + SSE + Web UI）
cd test-server
./start.sh
```

启动后终端输出：
```
==> building test-server ...
==> starting on http://localhost:8191
```

浏览器会自动打开 `http://localhost:8191`，进入 Agent 调试 Web UI。

### 发送聊天请求

Web UI 中直接输入消息即可。底层等价于以下 API 调用：

```bash
curl -X POST http://localhost:8191/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "计算 123 * 456 + 789",
    "api_key": "sk-xxx",
    "base_url": "https://api.deepseek.com/",
    "model": "deepseek-chat"
  }'
```

### 可选：启动 MCP 测试服务

如需测试 MCP 工具集成：

```bash
# 启动 test-mcp（算术工具 MCP Server）
cd test-mcp
go run .

# 默认监听 :9089，test-server 自动连接（环境变量 LOOPFORGE_MCP_TEST_URL 可覆盖）
```

然后在 Web UI 的 MCP 下拉选择「env」（默认值），即可看到 `add`、`subtract`、`multiply`、`divide` 等 MCP 工具被自动注入到 Agent 的工具列表中。

### 环境变量速查

| 变量 | 用途 | 默认值 |
|------|------|--------|
| `OPENAI_API_KEY` | OpenAI 兼容 API Key（最后兜底） | — |
| `DEEPSEEK_API_KEY` | DeepSeek API Key | — |
| `LARK_API_KEY` | 火山方舟 API Key | — |
| `OPENAI_BASE_URL` | OpenAI 兼容 API 地址 | `https://api.openai.com/v1` |
| `DEEPSEEK_BASE_URL` | DeepSeek API 地址 | `https://api.deepseek.com/` |
| `OPENAI_MODEL` | OpenAI 默认模型 | `gpt-4o-mini` |
| `DEEPSEEK_MODEL` | DeepSeek 默认模型 | `deepseek-chat` |
| `ADDR` | test-server 监听地址 | `:8191` |
| `TEST_SERVER_DISABLE_MCP` | 设为 `1` 禁用 MCP 自动连接 | — |

## loopForge SDK

loopForge 的核心是 Go 语言 SDK，你可以在自己的 Go 项目中直接引用。

### 安装

```bash
go get github.com/你的路径/loopforge  # 以实际仓库路径为准
```

### 最小示例：单个 Agent 工具调用

以下示例创建了一个带四则运算工具的 Agent，通过 OpenAI 适配器调用 LLM：

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	lpagent "loopforge/pkg/agent"
	lpmodel "loopforge/pkg/model"
	openaiadapter "loopforge/pkg/model/adapters/openai"
	lprunner "loopforge/pkg/runner"
	"loopforge/pkg/runtime/event"
	"loopforge/pkg/runtime/request"
)

// mathToolInfos 注册一组可用工具（add/subtract/multiply/divide）
func mathToolInfos() []*lpmodel.ToolInfo {
	binarySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"a": map[string]interface{}{"type": "number"},
			"b": map[string]interface{}{"type": "number"},
		},
		"required": []string{"a", "b"},
	}
	handler := func(name string, op func(a, b float64) float64) lpmodel.ToolCallHandler {
		return func(_ context.Context, argsJSON string) (string, error) {
			var args struct{ A, B float64 }
			// 生产代码应使用 json.Unmarshal
			fmt.Sscanf(argsJSON, `{"a":%f,"b":%f}`, &args.A, &args.B)
			return fmt.Sprintf("%.6g", op(args.A, args.B)), nil
		}
	}
	return []*lpmodel.ToolInfo{
		{Name: "add", Description: "Return a + b", Parameters: binarySchema,
			Handle: handler("add", func(a, b float64) float64 { return a + b })},
		{Name: "multiply", Description: "Return a * b", Parameters: binarySchema,
			Handle: handler("multiply", func(a, b float64) float64 { return a * b })},
	}
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("请设置 OPENAI_API_KEY")
		os.Exit(1)
	}

	// 1. 创建 ChatModel（OpenAI SDK 兜底适配器）
	chat := openaiadapter.NewOpenAIChatModel(apiKey, "", "gpt-4o-mini")

	// 2. 创建 Agent（绑定工具和系统提示词）
	ag := lpagent.New(chat,
		lpagent.WithSystemInstructions(
			"你是一个数学助手。请使用工具计算，不要心算。",
		),
		lpagent.WithToolInfos(mathToolInfos()),
		lpagent.WithMaxSteps(12),
	)

	// 3. 创建 Runner 并执行
	runner := lprunner.NewRunner(ag)
	req := &request.RuntimeRequest{
		SessionID:   "demo",
		UserMessage: "计算 12 * 34 + 56",
	}

	ch := runner.Run(context.Background(), req)

	// 4. 消费事件流（SSE 风格）
	for ev := range ch {
		switch ev.Type {
		case event.EventAnswer:
			if p := ev.Answer(); p != nil {
				fmt.Print(p.Content)
			}
		case event.EventCallLLMStart:
			if p := ev.CallLLMStart(); p != nil {
				fmt.Printf("\n[LLM 调用] model=%s\n", p.Model)
			}
		case event.EventToolCallStart:
			if p := ev.ToolCallStart(); p != nil {
				fmt.Printf("[工具调用] %s(args=%s)\n", p.Name, p.Arguments)
			}
		case event.EventToolCallEnd:
			if p := ev.ToolCallEnd(); p != nil {
				fmt.Printf("[工具结果] %s -> %s\n", p.Name, p.Output)
			}
		case event.EventQueryEnd:
			fmt.Println("\n[运行结束]")
		case event.EventError:
			if p := ev.Error(); p != nil {
				fmt.Fprintf(os.Stderr, "[错误] %s: %s\n", p.Code, p.Message)
			}
		}
		if ev.Type == event.EventQueryEnd || ev.Type == event.EventError {
			break
		}
	}
	_ = io.EOF // 抑制未使用导入
}
```

运行：

```bash
export OPENAI_API_KEY="sk-xxx"
go run main.go
```

### 多 Agent 转移（Handoff）

```go
// 创建两个 Agent，通过 handoff 连接
mathExpert := lpagent.New(chat,
	lpagent.WithName("math_expert"),
	lpagent.WithSystemInstructions("你是数学专家。"),
	lpagent.WithToolInfos(mathToolInfos()),
)
writer := lpagent.New(chat,
	lpagent.WithName("writer"),
	lpagent.WithSystemInstructions("你是创意写手。"),
)

// triage 作为入口 Agent，根据用户意图分派
triage := lpagent.New(chat,
	lpagent.WithName("triage"),
	lpagent.WithSystemInstructions("路由到 math_expert 或 writer，不要自己回答。"),
)
triage.AddHandoff(mathExpert, writer)

// 使用 Runner 自动执行 transfer loop
runner := lprunner.NewRunner(triage, lprunner.WithMaxTransfers(10))
ch := runner.Run(ctx, &request.RuntimeRequest{UserMessage: "写一首诗"})
// ... 消费事件流同上
```

### SDK 核心包概览

| 包路径 | 职责 |
|--------|------|
| `loopforge/pkg/agent` | Agent 定义、`New()`、`RunLoop`、Option 配置、Handoff 注册 |
| `loopforge/pkg/runner` | Runner 编排器 `NewRunner()`，管理 transfer loop、spawn、budget |
| `loopforge/pkg/model` | ChatModel 接口、Message、ToolInfo、CallOption 类型 |
| `loopforge/pkg/model/adapters/openai` | OpenAI SDK 兜底适配器 `OpenAIChatModel` |
| `loopforge/pkg/model/adapters/lark` | 火山方舟/Ark 适配器 `LarkChatModel` |
| `loopforge/pkg/model/adapters/deepseek` | DeepSeek 适配器 `DeepSeekChatModel` |
| `loopforge/pkg/tool` | ToolCall 分派与执行 `Invoke()` |
| `loopforge/pkg/mcp` | MCP 客户端、BootstrapToolInfos |
| `loopforge/pkg/skill` | SKILL.md 解析与注册表 |
| `loopforge/pkg/variable` | 变量存储（var_set 工具） |
| `loopforge/pkg/runtime/event` | 运行时事件类型（EventAnswer / EventCallLLMStart 等） |
| `loopforge/pkg/runtime/request` | RuntimeRequest 定义 |
| `loopforge/pkg/errors` | sentinel 错误（ErrInvalidConfig、ErrGenerate 等） |

## test-server

基于 Hertz 的 Agent 调试服务器，提供 Web UI 和 SSE 流式接口，用于测试 loopForge 引擎的 Agent 运行效果。内置四则运算工具，LLM 侧使用 **Lark（火山 Ark）** 适配器（`volcengine-go-sdk`）。

```bash
cd test-server
./start.sh
```

## 愿景

Rei / loopForge 的目标是构建一套 **Go 原生的 Multi-Agent 运行时引擎**——从底层的 Agent 循环、工具执行，到上层的多智能体协作、spawn、MCP 集成，再到端到端的可观测与调试体验，形成闭环。

### 近期里程碑

- **Multi-Agent spawn**：父子 Run 递归调用，支持动态创建子 Agent 并回注结果，MaxDepth / 并发 / 预算硬限制
- **MCP 客户端**：stdio / Streamable HTTP transport，tools/list 自动发现与注册
- **Skill 注册表**：`SKILL.md` 指令注入 + MCP 能力发现统一注册，支持动态启停与审计
- **工具权限收敛**：per-agent / per-spawn 的 whitelist / blacklist

### 中期方向

- **Reflection / Retry**：工具调用失败后模型自反思并重试
- **Cost Tracing**：从模型 adapter 层采集 usage，沿 Run 树聚合到 RunMetrics
- **Dev UI**：进程内 HTTP Debug Server + 浏览器可视化面板，展示 Run 时间线、spawn 树、tool 调用与 token/cost 聚合

### 长期愿景

- **生产级多智能体**：静态 Network + 动态 spawn 支撑复杂业务流程的可控拆解与协作
- **即插即用的能力层**：任何领域能力（RAG、记忆、代码执行、浏览器操作等）通过 MCP 工具标准化接入，引擎与能力彻底解耦
- **全链路可观测**：OTel trace 贯穿父子 Run、跨 MCP 调用与 tool 执行，配合 Dev UI 实现从开发调试到生产监控的统一体验

## 技术栈

### 语言与 LLM

| 类别 | 技术 |
|------|------|
| 语言 | Go |
| LLM 接入 | OpenAI SDK（兜底）、火山引擎 Ark（volcengine-go-sdk）、DeepSeek（deepseek-go） |

### loopForge（Multi-Agent 运行时）

| 类别 | 技术 |
|------|------|
| ChatModel 适配器 | OpenAI SDK（兜底）、volcengine-go-sdk（Lark/Ark）、deepseek-go（DeepSeek） |
| MCP 客户端 | MCP Go SDK（`modelcontextprotocol/go-sdk`） |
| Skills | `SKILL.md` 解析、注册表与 system 注入 |
| 可观测性 | OpenTelemetry（trace / metrics） |

### test-server（调试网关）

| 类别 | 技术 |
|------|------|
| HTTP 与流式响应 | Hertz（CloudWeGo）、SSE |

## 设计文档

`doc/design/` 目录包含 loopForge 的完整设计文档体系，按序号排列：

| 文档 | 内容 |
|------|------|
| [00-abstractions.md](loopForge/doc/design/00-abstractions.md) | 核心抽象总览：Agent、Runner、Model、MCP、Spawn 等 |
| [01-agent-core.md](loopForge/doc/design/01-agent-core.md) | Agent 核心契约：Runnable、RunLoop、生命周期 |
| [02-runner-core.md](loopForge/doc/design/02-runner-core.md) | Runner 编排器：单 Agent 执行、Transfer Loop、Spawn |
| [03-events.md](loopForge/doc/design/03-events.md) | 运行时事件体系：类型定义、发射契约 |
| [04-tools.md](loopForge/doc/design/04-tools.md) | 工具系统：ToolInfo、ToolCallHandler、分派链 |
| [05-mcp-tools.md](loopForge/doc/design/05-mcp-tools.md) | MCP 工具集成：Bootstrap、前缀隔离、白名单 |
| [06-Variable.md](loopForge/doc/design/06-Variable.md) | 变量存储：VarStore、Snapshot/Materialize、LLM 写入 |
| [07-transfer.md](loopForge/doc/design/07-transfer.md) | Agent 转移：Handoff 注册、transfer 工具生成 |
| [08-skills.md](loopForge/doc/design/08-skills.md) | Skills 系统：SKILL.md 解析、注册表、注入 |
| [09-system-prompt.md](loopForge/doc/design/09-system-prompt.md) | System Prompt 构建：Builder 管线、变量替换 |
| [10-spawn.md](loopForge/doc/design/10-spawn.md) | 动态 Spawn：子 Agent 创建、深度/并发/预算控制 |
| [11-chatmodel.md](loopForge/doc/design/11-chatmodel.md) | ChatModel 接口与适配器：Lark、DeepSeek、OpenAI SDK |
| [12-budget.md](loopForge/doc/design/12-budget.md) | 预算控制：Token/Cost 额度、Tree-wide 累计 |
| [13-errors.md](loopForge/doc/design/13-errors.md) | 错误体系：sentinel 错误、错误类型 |

# loopForge 项目进度日志 — v0.1.0

> **版本**：v0.1.0  
> **日期**：2026-04-15  
> **里程碑**：可运行基座 — agent-sdk-go 接入、`pkg/model` 抽象、方舟（Doubao）流式对话 demo 跑通  
> **上一版本**：v0.0.1（设计文档基线）

---

## 本版本目标

在 **v0.0.1 文档** 与 **sdk-selection** 决策下，落地最小可运行代码：依赖 **`github.com/agentizen/agent-sdk-go`**；自建与 Eino 风格对齐的 **ChatModel 抽象**；提供 **火山方舟 OpenAI 兼容** 路径上的 **流式对话** 验证（含本地 mock 与真实 API 场景）。

---

## 交付物（代码与结构）

| 路径 | 内容 |
|------|------|
| `go.mod` / `go.sum` | Go 模块，固定 `agent-sdk-go` 等依赖 |
| `cmd/loopforged/` | Mock / Ark runner demo（`LOOPFORGE_USE_MOCK` 等） |
| `cmd/doubaodemo/` | 方舟流式：`pkg/model` 上 `Stream` + `Recv` |
| `pkg/model/` | `Message`、`ToolInfo`、`CallOption`、`BaseChatModel` / `ToolCallingChatModel`、`MessageStreamReader` |
| `pkg/model/adapters/agentsdk/` | 基于 agent-sdk-go `Provider`/`Model` 的 `ChatModel` 实现（含增量流式转发） |
| `pkg/model/adapters/doubao/` | 方舟 `ArkChatModel`；`agentsdk.SDKChatModel` 仅作模型 HTTP 能力 |
| `pkg/model/types`、`pkg/model/interface` | 数据类型与 `BaseChatModel` / `ToolCallingChatModel` 契约，根包 `pkg/model` 做别名导出 |
| `pkg/runtime/*` | 原 `pkg/rt`：请求 / 结果 / 事件 / 交换 / 工具契约；`DonePayload` 携带完整 `RuntimeOutcome` |
| `pkg/agent` | `Agent`、`Planner`、`RunnerAgent`（`pkg/model` 工具循环，不依赖 SDK `Agent`/`Runner`） |
| `internal/engine` | `Engine` 嵌入 `agent.Agent`；`AgentRunner.RunLoop` 返回 `<-chan event.RuntimeEvent` 流式输出 |
| `cmd/doubaodemo`、`cmd/loopforged`、`cmd/agentdemo` | 方舟流式 demo、runner mock/Ark demo、`RunnerAgent`+豆包联调 |
| `start.sh` | 本地启动示例（**不含密钥**，需事先 export API key） |

---

## 未包含（后续版本）

- 引擎侧 `Engine` / `RunState` / MCP 聚合等与 `doc/design` 全量对齐的实现  
- 单测与 CI  
- `AgentRunner` 的具体实现体（当前仅有接口与类型）

---

## 备注

- 进程标识默认版本号见 `internal/defaults` 中 **ServiceVersion**（本里程碑 **0.1.0**）。  
- 敏感信息（如 `ARK_API_KEY`）仅通过环境变量注入，勿写入仓库。

---

## 提交记录（开发日志）

| 日期 | 说明 |
|------|------|
| 2026-04-15 | 合并：`pkg/model` 分层、`pkg/runtime`、`pkg/agent` 与 `RunnerAgent`、demo 迁至 `cmd`、`agentdemo`、引擎流式 `RunLoop` 签名、`start.sh` 去密钥化、移除残留 `internal/demo` 与重复 `pkg/rt`。 |

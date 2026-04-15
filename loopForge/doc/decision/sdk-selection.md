# loopForge：模型内核与编排选型

> **结论**：Multi-Agent **引擎**采用 **原生 Agent loop**（agent-sdk-go 的 `runner` + `pkg/network`）。**不**采用 Eino Graph 作为本仓库的编排内核。  
> **Memory / RAG**：**不**在引擎内实现，由 **MCP**（**SeRagLF** 为其中之一）以 **tools / resources** 提供。  
> **Skills / spawn**：**Skills** 为引擎侧可装载指令包；**动态子 Agent（spawn）** 由引擎在 **同一套 runner** 上创建短时子 `Run`（Claude Code 式子任务），**不**用 Graph 编排实现。

## 1. 术语对照

| 名称 | 指向 | 说明 |
|------|------|------|
| **agent-sdk-go** | `github.com/agentizen/agent-sdk-go` | `pkg/agent`、`pkg/runner`、`pkg/tool`、`pkg/network`、`pkg/model` |
| **SeRagLF** | workspace 内项目 | MCP Server；领域内 **Self-RAG、记忆、向量检索** 在其进程内（**Eino Graph** 编排） |
| **loopForge** | 本仓库 | Multi-Agent **引擎**：loop + 静态 `pkg/network` + **spawn**；**SkillLoader**；**多 MCP**；观测与预算 |

## 2. 为何选 agent-sdk-go 做「loop 内核」

- **Agent 运行时**：`pkg/runner` 提供 **显式迭代**（推理 → 工具 → 再推理），与「原生 Agent loop」目标一致。
- **Multi-Agent**：`pkg/network`（Parallel / Sequential / Competitive + 自定义 orchestrator）。
- **工具**：`pkg/tool`；可扩展 **MCP 适配层**，把远程 tools 变成同一种调用。
- **模型目录**：`pkg/model` 便于 **Cost** 估算。
- **可观测**：与在 span 上区分 LLM / tool / **MCP** 一致。

**loopForge 自研重点**：`LoopPolicy`、`RunState`、工具注册表（**多 MCP**、**spawn 内置工具**）、`SkillLoader`、**Spawner**（深度/并发/预算）、OTel + metrics + **含子 Run 的** cost 汇总——**不含**向量库与 RAG 管线。

## 3. Eino（SeRagLF 所用）与本引擎的差异：Graph vs Agent loop

| 维度 | Eino（Graph 编排） | loopForge（本 demo） |
|------|---------------------|----------------------|
| 心智模型 | **有向图**：节点是算子，边是数据流/分支；常被形容为 **基于 Graph 的「伪 ADK」式管线** | **Runner 循环**：每步是「模型一步 + 可选工具多轮」；Multi-Agent 是 **多个 Agent 实例 + network 调度** |
| 编排单元 | Node、Branch、Graph compile | Agent、`NetworkRunner`、handoff |
| 与 SeRagLF | **内部**跑 Self-RAG 闭环 | **外部**通过 **MCP** 调用（与其它 MCP Server **并列**），不复制其图 |
| **spawn** | — | 引擎内 **动态子 Run**；与 `pkg/network` 静态 roster **互补** |

**决策**：本 demo **不以 Eino 作为 Multi-Agent 引擎核心**；若团队已在 SeRagLF 里用 Eino，那是 **MCP 服务内部** 的实现细节，与 loopForge 的 loop 正交。

## 4. Memory / RAG 为何不放进引擎

- **单一职责**：引擎负责 **调度、循环、spawn、skill 注入**；检索、反思、长期记忆属于 **领域服务**（典型由 SeRagLF 等 MCP 提供）。
- **边界清晰**：通过 **MCP** 接入；同一引擎可同时挂 **多个** Server（SeRagLF + 其它），由 **connector manager** 管理连接与工具前缀。

## 5. 备选：完全自研 HTTP 调用 LLM

仅在无法接受 agent-sdk-go 依赖时考虑；代价是重复实现 runner、tool、network 与观测钩子，一般不推荐。

## 6. 最终决策（loopForge）

系统拓扑与模块划分见 **`doc/design/architecture.md`**；**RuntimeAction / Tool / MCP** 及 **§2 MVP 最小内核**见 **`doc/design/abstractions.md`**；与现网 runner **`EventMessage` / `EventMessageType` 类型语义对齐**见 **`doc/design/data-fusion.md`**（不含 Push/落库）。交付节奏建议与 **`doc/design/multi-agent-engine.md` §1.4** 一致：**Make it work → Make it right → Make it fast**（先跑通，再正确性/契约，再性能）。

1. **默认模型与 loop 内核**：`github.com/agentizen/agent-sdk-go`（版本在 `go.mod` 固定）。
2. **领域能力**：**MCP Servers**（含 SeRagLF）→ tools/resources → **Agent loop** 内调用。
3. **Skills**：受信路径加载 `SKILL.md`（或等价清单），注入到指定 Agent；可与子 spawn 绑定。
4. **spawn**：内置受控工具（或等价 API）创建子 `Agent` + 子 `Run`，强制 **深度/并发/预算** 上限。
5. **编排栈**：agent-sdk-go 的 **loop + `pkg/network`（静态）** + **spawn（动态）**；**不**引入 Eino 作为本仓库编排依赖。

## 7. 可调试 UI（与 Eino 的对标）

Eino 使用开源 **Eino Dev**（IDE 插件）+ **`github.com/cloudwego/eino-ext/devops`**（`devops.Init()` 在进程内起 HTTP，默认 **52538**，插件连 **IP:Port** 调试 Graph/Chain）。loopForge 不是 Graph，不能直接复用该插件协议，但采用 **同类模式**：**本机 Dev HTTP + 浏览器 Web UI**（可选 OTel → Jaeger）。详见 `doc/design/multi-agent-engine.md` 第 8 节。

## 8. 许可证与维护

依赖许可证审计以各模块 `LICENSE` 为准（agent-sdk-go 一般为 MIT；以仓库文件为准）。

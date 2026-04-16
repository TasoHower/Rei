# Rei

Rei 是一个由多个子项目组成的 AI Agent 基础设施，包含 Multi-Agent 运行时引擎、Self-RAG MCP 服务以及调试用测试服务器。所有组件均使用 Go 语言实现。

## 项目结构

```
Rei/
├── loopForge/      # Multi-Agent 运行时引擎
├── SeRagLF/        # Self-RAG MCP Server
└── test-server/    # Agent 调试测试服务器
```

## loopForge

Multi-Agent 运行时引擎，基于 `agent-sdk-go` 构建，提供可控的 Agent 循环、工具调用、MCP 集成、Skills 注入和动态 spawn 能力。

**核心特性：**

- **Agent Loop**：单 Agent 迭代运行，支持 LLM 调用与工具执行的交替循环
- **Multi-Agent**：静态 Network（Parallel / Sequential / Competitive）+ 动态 spawn（受深度、并发、预算约束）
- **MCP 集成**：多 Server 管理、工具白名单与前缀隔离
- **Skills**：`SKILL.md` 解析与受信加载
- **可观测性**：OTel trace + metrics、token/cost 聚合

```bash
cd loopForge
go run cmd/loopforged/main.go
```

## SeRagLF

生产级 Self-RAG MCP Server，将自反思检索增强生成能力封装为标准 MCP 服务。基于 Eino `compose.Graph` 实现有向循环图编排，支持检索→评估→生成→幻觉检测→质量评估的完整反思闭环。

**核心特性：**

- **Self-RAG 引擎**：6 节点 + 3 条件边的有向循环图，支持自动查询重写与多轮重试
- **双层记忆系统**：短期蒸馏上下文（Redis）+ 长期用户画像（MySQL + Qdrant）
- **12 个 MCP Tools**：6 个 RAG 工具 + 6 个 Memory 工具
- **文档摄入**：支持 txt/md/html 解析与 Unicode 级文本分块

**依赖服务：** Qdrant、MySQL 8.4、Redis 7

```bash
cd SeRagLF
docker compose up mysql qdrant redis -d
make run
```

## test-server

基于 Hertz 的 Agent 调试服务器，提供 Web UI 和 SSE 流式接口，用于测试 loopForge 引擎的 Agent 运行效果。内置四则运算工具，LLM 侧使用 **Lark（火山 Ark）** 适配器（`volcengine-go-sdk`）。

```bash
cd test-server
./start.sh
```

## 愿景

Rei 的目标是构建一套**完整的 Go 原生 AI Agent 基础设施**——从底层的检索增强生成，到上层的多智能体协作，再到端到端的可观测与调试体验，形成闭环。

### 近期里程碑

**loopForge — 从可运行到可协作：**

- **Multi-Agent spawn**：父子 Run 递归调用，支持动态创建子 Agent 并回注结果，MaxDepth / 并发 / 预算硬限制
- **MCP 客户端**：stdio / Streamable HTTP transport，tools/list 自动发现与注册，实现 loopForge ↔ SeRagLF 端到端联调
- **Skill 注册表**：`SKILL.md` 指令注入 + MCP 能力发现统一注册，支持动态启停与审计
- **工具权限收敛**：per-agent / per-spawn 的 whitelist / blacklist

**SeRagLF — 从功能完备到生产就绪：**

- LLM API 实际联调（OpenAI / Lark Ark）
- Redis Embedding 缓存，减少重复向量化请求
- 集成测试（testcontainers: MySQL + Redis + Qdrant）

### 中期方向

- **Self-RAG Planner**：模型自主决策是否检索，通过 MCP retrieve tool 实现 RAG-in-the-loop
- **Reflection / Retry**：工具调用失败后模型自反思并重试
- **Cost Tracing**：从模型 adapter 层采集 usage，沿 Run 树聚合到 RunMetrics
- **Dev UI**：进程内 HTTP Debug Server + 浏览器可视化面板，展示 Run 时间线、spawn 树、tool 调用与 token/cost 聚合

### 长期愿景

- **统一编排范式**：Agent Loop（loopForge）与 Graph 编排（SeRagLF/Eino）互补共存，各司其职
- **生产级多智能体**：静态 Network + 动态 spawn 支撑复杂业务流程的可控拆解与协作
- **即插即用的能力层**：任何领域能力（RAG、记忆、代码执行、浏览器操作等）通过 MCP 工具标准化接入，引擎与能力彻底解耦
- **全链路可观测**：OTel trace 贯穿父子 Run、跨 MCP 调用与 tool 执行，配合 Dev UI 实现从开发调试到生产监控的统一体验

## 技术栈

| 组件 | 技术 |
|------|------|
| 语言 | Go |
| Agent 引擎 | agent-sdk-go |
| RAG 编排 | Eino compose.Graph (CloudWeGo) |
| MCP 协议 | MCP Go SDK |
| 向量数据库 | Qdrant (gRPC) |
| 关系数据库 | MySQL 8.4 |
| 缓存 | Redis 7 |
| HTTP 框架 | Hertz (CloudWeGo) |
| LLM Provider | OpenAI / Lark (Volcengine Ark) |

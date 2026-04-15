# SeRagLF 项目进度日志 — v0.2.0

> **版本**：v0.2.0  
> **日期**：2026-04-13  
> **里程碑**：项目脚手架搭建完成（MCP Go SDK + Python AI Engine）  
> **上一版本**：[v0.1.0](progress-v0.1.0.md)

---

## 本版本完成事项

### 1. 共享 gRPC Proto 定义

- `proto/seraglf/ai/v1/ai_engine.proto` — AIEngine 服务
  - `SelfRAGQuery` / `SelfRAGQueryStream` — Self-RAG 查询（阻塞 + 流式）
  - `Embed` / `EmbedBatch` — 向量 Embedding
  - `ParseDocument` — 文档解析
  - `HealthCheck` — 健康探针
- `proto/seraglf/retriever/v1/retriever.proto` — VectorRetriever 服务
  - `Search` / `HybridSearch` — 向量检索（供 Python 回调 Go）
- Go 和 Python 的 gRPC 代码均已生成

### 2. Go 主服务 `gateway/`（MCP Go SDK 架构）

| 模块 | 路径 | 说明 |
|------|------|------|
| 入口 | `cmd/gateway/main.go` | 支持 stdio / Streamable HTTP 两种 MCP transport |
| MCP Server | `internal/mcpserver/server.go` | 基于 `go-sdk v1.5.0` 创建 MCP Server |
| MCP Tools | `internal/mcpserver/tools.go` | 6 个 Tool：self_rag_query, ingest_documents, search_documents, list_collections, get_collection_stats, delete_collection |
| MCP Resources | `internal/mcpserver/resources.go` | 2 个 Resource：seraglf://config, seraglf://health |
| AI gRPC 客户端 | `internal/ai/client.go` | 调用 Python AIEngine 的 gRPC 客户端 |
| Retriever gRPC 服务 | `internal/ai/retriever_server.go` | Go 侧向量检索 gRPC 服务，供 Python 回调 |
| 配置 | `internal/config/config.go` | viper 管理，支持 YAML + 环境变量 |
| 健康检查 | `internal/health/checker.go` | 并发探测各组件健康状态 |

**关键架构变更**：移除 Hertz HTTP 框架，改用 MCP Go SDK 原生 transport。MCP 协议成为 Agent 与服务交互的唯一接口。

### 3. Python AI Engine `engine/`

| 模块 | 路径 | 说明 |
|------|------|------|
| gRPC 服务端 | `src/seraglf_ai/server.py` | 异步 gRPC server，实现 AIEngine 接口 |
| 配置 | `src/seraglf_ai/config.py` | pydantic-settings，环境变量前缀 `SERAGLF_` |
| LangGraph 图状态 | `src/seraglf_ai/graph/state.py` | GraphState TypedDict |
| LangGraph 节点 | `src/seraglf_ai/graph/nodes.py` | retrieve, grade_documents, generate, check_hallucination, grade_answer, transform_query |
| LangGraph 边 | `src/seraglf_ai/graph/edges.py` | 条件路由：decide_to_generate, grade_generation, grade_answer_quality |
| LangGraph 构建 | `src/seraglf_ai/graph/builder.py` | 编译完整 Self-RAG 有向循环图 |
| 相关性评分 | `src/seraglf_ai/graders/relevance.py` | Pydantic 结构化输出 + LLM Prompt |
| 幻觉检测 | `src/seraglf_ai/graders/hallucination.py` | Pydantic 结构化输出 + LLM Prompt |
| 答案评分 | `src/seraglf_ai/graders/answer.py` | Pydantic 结构化输出 + LLM Prompt |
| Embedding 服务 | `src/seraglf_ai/embedding/service.py` | 抽象后端 + OpenAI 实现 + 工厂模式 |
| 文档解析 | `src/seraglf_ai/ingest/parser.py` | 文件/目录解析（预留 unstructured 集成） |
| 文本分块 | `src/seraglf_ai/ingest/chunker.py` | 可配置 chunk_size / overlap 的分块器 |
| 日志 | `src/seraglf_ai/utils/logging.py` | structlog 结构化日志 |

### 4. 构建系统

- 根目录 `Makefile` 统一管理：
  - `make proto` — 生成 Go + Python gRPC 代码
  - `make build-go` — 编译 Go 服务
  - `make run-go` — 运行 Go MCP Server
  - `make run-engine` — 运行 Python AI Engine

---

## 项目总进度

- [x] 服务规划（v0.1.0）
- [x] 技术选型（v0.1.0）
- [x] API 设计（v0.1.0）
- [x] Proto 文件编写与代码生成（v0.2.0）
- [x] Go MCP Server 骨架实现（v0.2.0）
- [x] Python AI Engine 骨架实现（v0.2.0）
- [x] 构建系统 Makefile（v0.2.0）
- [ ] Self-RAG LangGraph 管线接入实际 LLM
- [ ] Qdrant 向量数据库集成
- [ ] Docker Compose 编排
- [ ] 单元测试
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

## 下一步计划

1. Self-RAG LangGraph 管线接入实际 LLM（OpenAI / Claude）
2. Qdrant 向量数据库集成（Go 侧 gRPC 客户端）
3. Docker Compose 编排（Qdrant + Redis + gateway + engine）
4. 单元测试 + 集成测试
5. 文档补充

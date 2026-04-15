# SeRagLF 项目进度日志 — v0.4.0

> **版本**：v0.4.0  
> **日期**：2026-04-13  
> **里程碑**：全栈迁移至纯 Go 架构（Eino + MCP Go SDK）  
> **上一版本**：[v0.3.0](progress-v0.3.0.md)

---

## 本版本完成事项

### 架构重构：Python → Go 全迁移

**删除的组件：**
- `engine/` — 整个 Python AI Engine 微服务
- `proto/` — gRPC Proto 定义（AI Engine / VectorRetriever）
- `gateway/gen/` — 生成的 gRPC Go 代码
- `gateway/internal/ai/` — gRPC 客户端和回调服务端
- `gateway/internal/health/` — 旧健康检查模块
- 服务间 gRPC 双跳通信链路

**新增的组件：**

| 模块 | 路径 | 说明 |
|------|------|------|
| **Eino Self-RAG 图** | `internal/selfrag/graph.go` | 基于 Eino `compose.Graph` 实现完整 Self-RAG 有向循环图 |
| **Self-RAG 状态** | `internal/selfrag/state.go` | 图状态结构体，含 trace 追踪 |
| **Self-RAG 节点** | `internal/selfrag/nodes.go` | 6 个节点：retrieve, grade_documents, generate, check_hallucination, grade_answer, transform_query |
| **LLM 模块** | `internal/llm/llm.go` | Eino ChatModel 封装（OpenAI），双模型：grader(gpt-4o-mini) + generator(gpt-4o) |
| **Embedding 模块** | `internal/embedding/embedding.go` | Eino Embedder 封装（OpenAI text-embedding-3-small） |
| **Qdrant 客户端** | `internal/qdrant/client.go` | 官方 Go gRPC 客户端，支持 Search/Upsert/Collection 管理 |
| **文档解析** | `internal/ingest/parser.go` | 纯 Go 实现，支持 txt/md/html |
| **文本分块** | `internal/ingest/chunker.go` | 可配置 chunk_size/overlap，基于 rune 处理 Unicode |

### 关键技术决策

1. **LangGraph → Eino Graph**：字节跳动 CloudWeGo 生态，Go 原生有向循环图编排，编译期类型安全，TikTok/豆包生产验证
2. **gRPC 双跳 → 进程内调用**：消除 Go↔Python 的 gRPC 往返，Self-RAG 全链路在同一进程内完成
3. **单一二进制部署**：整个 MCP Server 编译为一个 Go 二进制，无 Python 运行时依赖

### MCP Tools（不变，但实现已对接真实模块）

| Tool | 状态 |
|------|------|
| `self_rag_query` | ✅ 对接 Eino Self-RAG Pipeline |
| `ingest_documents` | ✅ 对接 Go 解析 + Embedding + Qdrant Upsert |
| `search_documents` | ✅ 对接 Embedding + Qdrant Search |
| `create_collection` | ✅ 对接 Qdrant Collection API |
| `list_collections` | ✅ 对接 Qdrant Collection API |
| `delete_collection` | ✅ 对接 Qdrant Collection API |

### Docker Compose 简化

从 5 个服务降至 4 个（移除 engine），gateway 不再需要 50051/50052 gRPC 端口。

---

## 项目总进度

- [x] 服务规划（v0.1.0）
- [x] 技术选型（v0.1.0）
- [x] API 设计（v0.1.0）
- [x] 项目脚手架搭建（v0.2.0）
- [x] Docker 容器化编排（v0.3.0）
- [x] 全栈迁移至纯 Go — Eino + Qdrant Go Client（v0.4.0）
- [x] Self-RAG Eino 图完整实现（v0.4.0）
- [x] LLM / Embedding / Ingest 模块实现（v0.4.0）
- [ ] 实际 LLM API 联调测试
- [ ] MySQL 持久化层实现
- [ ] Redis 缓存层实现
- [ ] 单元测试
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

## 下一步计划

1. 实际 LLM API 联调（OpenAI / 豆包 Ark）
2. MySQL 持久化层（collection 元数据、审计日志）
3. Redis Embedding 缓存
4. 单元测试 + 集成测试
5. 文档补充

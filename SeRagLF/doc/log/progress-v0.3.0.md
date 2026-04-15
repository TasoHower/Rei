# SeRagLF 项目进度日志 — v0.3.0

> **版本**：v0.3.0  
> **日期**：2026-04-13  
> **里程碑**：Docker 容器化编排  
> **上一版本**：[v0.2.0](progress-v0.2.0.md)

---

## 本版本完成事项

### 1. Docker 容器化

- `gateway/Dockerfile` — Go 主服务多阶段构建（golang:1.24-alpine → alpine:3.21），产物约 20MB
- `engine/Dockerfile` — Python AI Engine 镜像（python:3.13-slim），预装依赖后拷贝代码

### 2. Docker Compose 编排

`docker-compose.yml` 编排 4 个服务：

| 服务 | 镜像 | 端口 | 角色 |
|------|------|------|------|
| qdrant | `qdrant/qdrant:v1.14.0` | 6333 / 6334 | 向量数据库 |
| redis | `redis:7-alpine` | 6379 | Embedding 缓存 |
| engine | 本地构建 | 50051 | Python AI Engine gRPC |
| gateway | 本地构建 | 8080 / 50052 | Go MCP Server + VectorRetriever gRPC |

关键特性：
- 所有服务配置 healthcheck，gateway 通过 `depends_on` + `service_healthy` 保证启动顺序
- Qdrant / Redis 数据持久化到 named volume
- Redis 配置 AOF 持久化 + 256MB 内存上限 + LRU 淘汰
- 敏感配置（`OPENAI_API_KEY`）通过宿主机环境变量注入，不硬编码

---

## 项目总进度

- [x] 服务规划（v0.1.0）
- [x] 技术选型（v0.1.0）
- [x] API 设计（v0.1.0）
- [x] Proto 文件编写与代码生成（v0.2.0）
- [x] Go MCP Server 骨架实现（v0.2.0）
- [x] Python AI Engine 骨架实现（v0.2.0）
- [x] 构建系统 Makefile（v0.2.0）
- [x] Docker 容器化编排（v0.3.0）
- [ ] Self-RAG LangGraph 管线接入实际 LLM
- [ ] Qdrant 向量数据库集成
- [ ] 单元测试
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

## 下一步计划

1. Self-RAG LangGraph 管线接入实际 LLM（OpenAI / Claude）
2. Qdrant 向量数据库集成（Go 侧 gRPC 客户端）
3. 单元测试 + 集成测试
4. 文档补充

# SeRagLF 项目进度日志 — v0.6.0

> **版本**：v0.6.0  
> **日期**：2026-04-13  
> **里程碑**：结构化日志系统（`log/slog`）  
> **上一版本**：[v0.5.0](progress-v0.5.0.md)

---

## 本版本完成事项

### 结构化日志系统

**背景**：项目此前仅在 `main.go` 中使用标准库 `log` 包打印非结构化日志，缺乏日志级别、结构化字段和模块标识，不符合生产级可观测性要求。

**方案**：采用 Go 标准库 `log/slog`（Go 1.21+ 内置），零外部依赖，通过构造函数注入 `*slog.Logger` 实现模块级子日志。

**新增模块：**

| 模块 | 路径 | 说明 |
|------|------|------|
| **Logger** | `internal/logger/logger.go` | slog 初始化封装，支持 level（debug/info/warn/error）和 format（text/json）配置 |

**修改的文件：**

| 文件 | 变更 |
|------|------|
| `internal/config/config.go` | 新增 `LogConfig` 结构体（`level`/`format`），`Config` 顶层新增 `Log` 字段，添加默认值 |
| `cmd/gateway/main.go` | 替换全部 `log.*` 调用为 `slog.*`，初始化时打印各组件就绪状态，启动失败用 `log.Error` + `os.Exit(1)` |
| `internal/mcpserver/server.go` | `Deps` 新增 `Log *slog.Logger` 字段 |
| `internal/mcpserver/tools.go` | `self_rag_query`/`ingest_documents` handler 添加入口、出口、耗时日志 |
| `internal/mcpserver/tools_memory.go` | `memory_distill`/`memory_save`/`memory_recall` handler 添加入口、出口、耗时、错误日志 |
| `internal/memory/short_term.go` | `ShortTermStore` 注入 `*slog.Logger`，`Update`/`Get`/`BufferMessages`/`ListMessages`/`Clear` 添加 Debug/Info 日志 |
| `internal/memory/long_term.go` | `LongTermStore` 注入 `*slog.Logger`，`EnsureCollections`/`UpsertFacts`/`DeleteFact`/`SaveSummary`/`RecallFacts`/`RecallSummaries` 添加日志 |
| `internal/memory/extractor.go` | `Extractor` 注入 `*slog.Logger`，`ExtractFacts`/`Distill`/`Summarize` 添加耗时统计日志 |
| `internal/selfrag/nodes.go` | `Nodes` 注入 `*slog.Logger`，`Retrieve`/`GradeDocuments`/`CheckHallucination`/`GradeAnswer`/`TransformQuery` 添加 Debug/Info 日志 |

### 日志设计要点

1. **模块子日志**：`main.go` 中通过 `log.With("module", "xxx")` 为每个模块创建带标识的子 Logger，日志输出自动携带模块名
2. **日志级别划分**：
   - `Debug`：Redis 读写、Qdrant 搜索结果数量、Self-RAG 节点中间状态等高频操作
   - `Info`：Tool 调用入口/出口及耗时、LLM 调用耗时、数据写入成功、服务启动就绪
   - `Warn`：非致命错误（如 `EnsureCollections` 失败）
   - `Error`：Tool handler 中的操作失败
3. **耗时统计**：所有 Tool handler 和 LLM 调用均记录 `duration_ms`，便于性能分析
4. **配置化**：通过 `SERAGLF_LOG_LEVEL` / `SERAGLF_LOG_FORMAT` 环境变量或 YAML 配置控制日志级别和输出格式（text/json）

### 配置示例

```yaml
log:
  level: info     # debug | info | warn | error
  format: text    # text | json
```

```bash
export SERAGLF_LOG_LEVEL=debug
export SERAGLF_LOG_FORMAT=json
```

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
- [x] 双层记忆系统实现（v0.5.0）
- [x] MySQL 持久化层实现 — user_facts 表（v0.5.0）
- [x] Redis 缓存层实现 — 对话短期记忆（v0.5.0）
- [x] 短期记忆重构：原始消息缓冲 → LLM 上下文蒸馏（v0.5.0）
- [x] 结构化日志系统 — slog 全链路注入（v0.6.0）
- [ ] 实际 LLM API 联调测试
- [ ] Redis Embedding 缓存
- [ ] 单元测试
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

## 下一步计划

1. 实际 LLM API 联调（OpenAI / 豆包 Ark）
2. Redis Embedding 缓存（减少重复向量化请求）
3. 单元测试 + 集成测试
4. 文档补充（README、部署指南）

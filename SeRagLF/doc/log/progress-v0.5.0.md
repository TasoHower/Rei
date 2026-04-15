# SeRagLF 项目进度日志 — v0.5.0

> **版本**：v0.5.0  
> **日期**：2026-04-13  
> **里程碑**：双层记忆系统（短期对话记忆 + 长期用户记忆）  
> **上一版本**：[v0.4.0](progress-v0.4.0.md)

---

## 本版本完成事项

### 记忆系统实现

**新增模块：**

| 模块 | 路径 | 说明 |
|------|------|------|
| **类型定义** | `internal/memory/types.go` | Message、Fact、ConversationSummary 类型 |
| **短期记忆** | `internal/memory/short_term.go` | Redis List 实现，TTL 自动过期，LTRIM 容量控制 |
| **长期记忆** | `internal/memory/long_term.go` | 事实双写（MySQL CRUD + Qdrant `_user_facts` 语义索引）+ Qdrant `_memory` 摘要向量检索 |
| **事实提取器** | `internal/memory/extractor.go` | LLM 驱动的结构化事实提取 + 对话摘要生成 |
| **MySQL Store** | `internal/store/mysql.go` | 连接池管理 + user_facts 表自动 DDL 迁移 |
| **Memory Tools** | `internal/mcpserver/tools_memory.go` | 6 个 Memory MCP Tool handlers |

**修改的文件：**

| 文件 | 变更 |
|------|------|
| `internal/config/config.go` | 新增 `MemoryConfig` 配置段（TTL、最大消息数、摘要集合名、事实集合名） |
| `internal/qdrant/client.go` | 新增 `SearchWithFilter`（带 payload filter）、`DeletePoints`（按 point ID 删除）、通用 payload 解析 |
| `internal/mcpserver/server.go` | `Deps` 新增 ShortTerm/LongTerm/Extractor；版本升至 0.5.0 |
| `cmd/gateway/main.go` | 新增 MySQL、Redis、Memory 模块初始化链路 |

**新增依赖：**

| 包 | 版本 | 用途 |
|------|------|------|
| `github.com/redis/go-redis/v9` | v9.18.0 | Redis 客户端 |
| `github.com/go-sql-driver/mysql` | v1.9.3 | MySQL 驱动 |

### 新增 6 个 MCP Tools

| Tool | 类型 | 功能 |
|------|------|------|
| `memory_append` | 短期 | 追加消息到对话缓冲区 |
| `memory_list` | 短期 | 获取对话最近 N 条消息 |
| `memory_clear` | 短期 | 清除对话短期记忆 |
| `memory_save` | 长期 | LLM 提取事实 + 生成摘要，事实双写 MySQL + Qdrant `_user_facts`，摘要写入 Qdrant `_memory` |
| `memory_recall` | 长期 | 两路并行语义搜索：相关事实（`_user_facts`）+ 相关历史摘要（`_memory`），按 query 相关性排序 |
| `memory_user_profile` | 长期 | 查看/删除用户结构化事实 |

### 关键技术决策

1. **短期记忆 → Redis List**：利用已有 Redis 基础设施，RPUSH + LTRIM + EXPIRE 实现滑动窗口式对话缓冲
2. **事实双写 MySQL + Qdrant**：MySQL 保证 CRUD/去重（UNIQUE KEY + UPSERT），Qdrant `_user_facts` 提供语义检索。使用确定性 UUID v5（`user_id+category+fact_key`）保证 Qdrant 层幂等覆写
3. **事实向量化**：每条事实拼接为 `"{category}: {fact_key} — {fact_value}"` 后 embedding，使语义搜索能按 query 相关性召回事实
4. **`memory_recall` 两路语义搜索**：并行检索 `_user_facts` 和 `_memory`，替代原先全量拉取 MySQL 的方案，消除事实增长后的噪音问题
5. **`DeleteFact` 双删**：先查 MySQL 获取 key 信息，再 MySQL DELETE + Qdrant DeletePoints 同步删除
6. **并行 LLM 调用**：`memory_save` 中事实提取与摘要生成通过 goroutine 并行执行
7. **独立 MCP Tools**：记忆与 Self-RAG 解耦，Agent 自行决定何时读写记忆

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
- [ ] 实际 LLM API 联调测试
- [ ] Redis Embedding 缓存
- [ ] 单元测试
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

### 短期记忆重构：原始消息缓冲 → 上下文蒸馏

**重构原因**：原设计的短期记忆存储完整的原始聊天记录（逐条 `{role, content, timestamp}`），不符合"关键信息蒸馏"的设计意图。Agent 需要的是对话的浓缩上下文快照，而非原始消息流。

**变更内容**：

| 维度 | 旧设计 | 新设计 |
|------|--------|--------|
| 存储内容 | 逐条原始消息 | LLM 蒸馏后的浓缩上下文 (`DistilledContext`) |
| Redis 结构 | List（RPUSH 逐条追加） | String（整体 SET 覆写）+ List（内部消息缓冲） |
| 写入时机 | 每条消息即时追加 | Agent 按需调用 `memory_distill`，LLM 增量蒸馏 |
| 读取内容 | 最近 N 条原始消息 | 当前对话的关键信息快照 |
| LLM 参与 | 无 | 每次蒸馏调用一次 LLM |

**新增 `DistilledContext` 结构体**：

包含 `topics`（主题）、`decisions`（决策）、`entities`（实体）、`action_items`（待办）、`current_task`（当前任务）、`narrative`（叙事摘要）、`version`（蒸馏版本号）等字段。

**MCP Tool 变更**：

| 旧 Tool | 新 Tool | 变化 |
|---------|---------|------|
| `memory_append` | `memory_distill` | 从"追加原始消息"变为"传入新消息 → LLM 增量蒸馏 → 更新浓缩上下文" |
| `memory_list` | `memory_context` | 从"返回原始消息列表"变为"返回当前蒸馏后的上下文快照" |
| `memory_clear` | `memory_clear` | 不变，清除蒸馏上下文和消息缓冲 |

**方案 A 内部消息缓冲**：`memory_distill` 在蒸馏的同时将原始消息追加到 `conv:{id}:messages`（保留 Redis List 作为内部缓冲），确保 `memory_save` 仍能读取原始对话进行事实提取和摘要生成。

**修改的文件**：

| 文件 | 变更 |
|------|------|
| `internal/memory/types.go` | 新增 `DistilledContext` 结构体 |
| `internal/memory/short_term.go` | 完全重写：`Update`/`Get`/`Clear`/`BufferMessages`/`ListMessages` |
| `internal/memory/extractor.go` | 新增 `Distill` 方法（增量蒸馏 prompt + JSON 解析）+ `extractJSONObject` 辅助函数 |
| `internal/mcpserver/tools_memory.go` | `MemoryAppendArgs`→`MemoryDistillArgs`，`MemoryListArgs`→`MemoryContextArgs`；handler 全部重写 |
| `doc/design/architecture.md` | 更新记忆系统相关章节 |
| `doc/design/api-design.md` | 更新 memory_distill/memory_context 工具定义 |

---

## 下一步计划

1. 实际 LLM API 联调（OpenAI / 豆包 Ark）
2. Redis Embedding 缓存（减少重复向量化请求）
3. 单元测试 + 集成测试
4. 文档补充（README、部署指南）

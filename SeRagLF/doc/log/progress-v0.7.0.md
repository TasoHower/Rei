# SeRagLF 项目进度日志 — v0.7.0

> **版本**：v0.7.0  
> **日期**：2026-04-14  
> **里程碑**：单元测试  
> **上一版本**：[v0.6.0](progress-v0.6.0.md)

---

## 本版本目标

为所有现有 Go 模块补充单元测试，提升代码质量与可维护性。当前项目 **零测试文件**，本版本以 **可单测的纯逻辑优先、需 mock 的模块其次** 的策略逐步覆盖。

---

## 前置工作：接口抽取（可测性重构）

当前多个模块直接依赖具体实现（`*qdrant.Client`、`*embedding.Service`、`*redis.Client`），导致无法在不启动外部服务的情况下进行单元测试。需先抽取以下接口：

| 接口 | 所在包 | 方法 | 被依赖方 |
|------|--------|------|----------|
| `Embedder` | `embedding` | `Embed`, `EmbedQuery`, `Dimension` | `selfrag.Nodes`, `memory.LongTermStore` |
| `VectorStore` | `qdrant` | `Search`, `SearchWithFilter`, `Upsert`, `DeletePoints`, `CreateCollection`, `ListCollections` | `selfrag.Nodes`, `memory.LongTermStore` |

抽出接口后，`Nodes` / `LongTermStore` 的字段类型从具体 struct 改为接口，即可用 mock 实现替代。

---

## 测试计划（按优先级排列）

### P0 — 纯函数 / 零外部依赖（直接可测）

#### 1. `internal/ingest` — chunker_test.go

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestChunkText_Basic` | 正常分块 | 短文本、超长文本、恰好整除、中文 Unicode |
| `TestChunkText_EdgeCases` | 边界条件 | 空字符串 → nil, chunkSize=0 → nil, overlap >= chunkSize 修正 |
| `TestChunkText_Overlap` | 重叠验证 | 验证相邻 chunk 内容有重叠区域 |
| `TestChunkText_Metadata` | 元数据透传 | metadata 正确复制到每个 Chunk |

#### 2. `internal/ingest` — parser_test.go

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestParseFile_TextFile` | .txt 解析 | t.TempDir 创建临时 .txt 文件 |
| `TestParseFile_MarkdownFile` | .md 解析 | 验证内容完整读取、metadata 包含 source 和 format |
| `TestParseFile_HTMLFile` | .html 解析 + 标签剥离 | 含 `<p>`/`<div>` 标签的 HTML 文件 |
| `TestParseFile_UnsupportedFormat` | 不支持格式 | .pdf / .docx → 返回 error |
| `TestParseFile_NotFound` | 文件不存在 | → 返回 error |
| `TestParseDirectory` | 目录递归 | 目录下含混合文件类型，只解析支持的 |
| `TestStripHTMLTags` | HTML 标签剥离 | 嵌套标签、自闭合标签、无标签文本 |

#### 3. `internal/selfrag` — state_test.go

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestState_AddTrace` | TraceStep 追加 | 多次 addTrace，验证顺序和内容 |

#### 4. `internal/memory` — helpers_test.go

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestFormatConversation` | 对话格式化 | 多条消息拼接、空消息切片 |
| `TestExtractJSONObject` | JSON 提取 | 包含前后垃圾文本的 JSON、无 JSON、嵌套括号 |
| `TestExtractJSONArray` | JSON 数组提取 | 同上，针对 `[...]` |
| `TestTruncate` | 字符串截断 | 短于 maxLen、等于 maxLen、超过 maxLen |

---

### P1 — 需 mock 外部依赖

#### 5. `internal/config` — config_test.go

**测试依赖**：`t.TempDir` + 临时 YAML 文件、`t.Setenv`

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestLoad_DefaultValues` | 无配置文件时默认值 | 不提供配置文件，验证所有默认值 |
| `TestLoad_YAMLFile` | YAML 解析 | 临时 YAML 文件，验证值覆盖默认 |
| `TestLoad_EnvOverride` | 环境变量覆盖 | `t.Setenv("SERAGLF_LLM_API_KEY", ...)` |
| `TestLoad_InvalidYAML` | 非法 YAML | → 返回 error |
| `TestLoad_PartialConfig` | 部分配置 | 只设置部分字段，其余取默认 |

#### 6. `internal/logger` — logger_test.go

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestSetup_Levels` | 日志级别映射 | debug/info/warn/error/未知值 → 返回非 nil Logger |
| `TestSetup_Formats` | 输出格式选择 | json / text / 未知值 |

#### 7. `internal/selfrag` — nodes_test.go

**测试依赖**：mock `model.BaseChatModel`（实现 `Generate` 方法）、mock `Embedder`、mock `VectorStore`

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestRetrieve_Success` | 正常检索 | mock embedder 返回向量 → mock vectorDB 返回结果 → 验证 Documents |
| `TestRetrieve_EmbedError` | 向量化失败 | embedder 返回 error → 不 panic、Documents 为空、trace 记录 |
| `TestRetrieve_SearchError` | 检索失败 | vectorDB 返回 error → 同上 |
| `TestGradeDocuments_AllRelevant` | 全部相关 | grader 返回 `{"relevant": true}` → 全部保留 |
| `TestGradeDocuments_NoneRelevant` | 全部无关 | grader 返回 `{"relevant": false}` → Documents 清空 |
| `TestGradeDocuments_GraderError` | 打分失败 | grader 返回 error → 文档以 0.5 分保留 |
| `TestGenerate_Success` | 正常生成 | generator 返回内容 → State.Generation 有值 |
| `TestGenerate_Error` | 生成失败 | generator 返回 error → Generation 为空、trace 记录 |
| `TestCheckHallucination_Grounded` | 生成有据 | grader 返回 `{"grounded": true}` → Grounded=true |
| `TestCheckHallucination_NotGrounded` | 生成无据 | grader 返回 `{"grounded": false}` → Grounded=false |
| `TestCheckHallucination_Error` | 检查失败 | grader error → 默认 Grounded=true |
| `TestGradeAnswer_Useful` | 回答有用 | grader 返回 `{"useful": true}` → AnswerUseful=true |
| `TestGradeAnswer_NotUseful` | 回答无用 | grader 返回 `{"useful": false}` → AnswerUseful=false |
| `TestTransformQuery` | 查询重写 | grader 返回新问题 → Question 更新、Retries++ |
| `TestExtractJSON` | JSON 抽取辅助函数 | 带前后文本的 JSON、无 JSON |

#### 8. `internal/memory` — short_term_test.go

**测试依赖**：[miniredis](https://github.com/alicebob/miniredis) 内存 Redis

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestShortTerm_UpdateAndGet` | 上下文存取 | Update → Get 取回，验证内容一致 |
| `TestShortTerm_GetNonExistent` | 不存在的会话 | → 返回 nil, nil |
| `TestShortTerm_BufferMessages` | 消息缓冲 | 缓冲多条消息 → ListMessages 取回、验证顺序 |
| `TestShortTerm_BufferMessages_MaxLimit` | 消息数上限 | 超过 MaxConversationMessages → 旧消息被截断 |
| `TestShortTerm_Clear` | 清除会话 | Clear → Get 返回 nil、ListMessages 返回空 |
| `TestShortTerm_TTL` | TTL 设置 | 验证 key 设置了正确的过期时间 |

#### 9. `internal/memory` — extractor_test.go

**测试依赖**：mock `model.BaseChatModel`

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestExtractFacts_Success` | 正常提取 | LLM 返回合法 JSON 数组 → 解析出 Facts |
| `TestExtractFacts_EmptyArray` | 无事实 | LLM 返回 `[]` → 空切片 |
| `TestExtractFacts_LLMError` | LLM 调用失败 | → 返回 error |
| `TestExtractFacts_InvalidJSON` | 返回非法 JSON | → 返回 error |
| `TestExtractFacts_SkipEmptyKeys` | 过滤空字段 | FactKey 或 FactValue 为空 → 跳过 |
| `TestDistill_FirstMessage` | 首轮蒸馏 | existing=nil → Version=1 |
| `TestDistill_Incremental` | 增量蒸馏 | existing.Version=2 → 结果 Version=3 |
| `TestDistill_LLMError` | LLM 调用失败 | → 返回 error |
| `TestSummarize_Success` | 正常摘要 | LLM 返回文本 → 非空字符串 |
| `TestSummarize_LLMError` | LLM 调用失败 | → 返回 error |

#### 10. `internal/memory` — long_term_test.go

**测试依赖**：`sqlmock` + mock `Embedder` + mock `VectorStore`

| 测试函数 | 覆盖目标 | 用例 |
|----------|----------|------|
| `TestEnsureCollections_AlreadyExist` | 集合已存在 | → 不创建 |
| `TestEnsureCollections_Create` | 集合不存在 | → 调用 CreateCollection |
| `TestUpsertFacts_Success` | 事实写入 | MySQL 写入 + Qdrant upsert 均成功 |
| `TestUpsertFacts_Empty` | 空 facts | → 直接返回 0, nil |
| `TestUpsertFacts_MySQLError` | MySQL 失败 | → 返回 error |
| `TestGetFacts` | 事实查询 | mock rows → 解析出 Fact 切片 |
| `TestDeleteFact_Success` | 事实删除 | MySQL + Qdrant 均成功 |
| `TestDeleteFact_NotFound` | 不存在的 ID | → 返回 error |
| `TestSaveSummary` | 摘要保存 | embed + Qdrant upsert 成功 |
| `TestRecallFacts` | 事实召回 | embed query + SearchWithFilter → Fact 列表 |
| `TestRecallSummaries` | 摘要召回 | embed query + SearchWithFilter → Summary 列表 |
| `TestFactPointID` | 确定性 UUID | 相同输入 → 相同输出 |
| `TestFactEmbedText` | 嵌入文本格式 | 验证拼接格式正确 |

---

### P2 — 集成性较强（低优先级 / 可推迟）

#### 11. `internal/qdrant` — client_test.go

**说明**：Client 方法全部是薄 gRPC 封装，不含业务逻辑。可通过 `bufconn` + fake gRPC server 测试，或推迟到集成测试。

| 测试函数 | 覆盖目标 |
|----------|----------|
| `TestParseSearchResults` | `parseSearchResults` 纯函数（需通过包内测试访问） |
| `TestCreateCollection_DistanceMapping` | distance 字符串 → protobuf 枚举映射 |

#### 12. `internal/store` — mysql_test.go

**说明**：`NewMySQL` 包含 Ping + DDL，适合集成测试（testcontainers）。可推迟至 v0.8.0 集成测试阶段。

#### 13. `internal/mcpserver` — tools_test.go

**说明**：Tool handler 为 MCP SDK 注册的匿名闭包，当前不可直接调用。建议后续提取 handler 逻辑为独立函数再补测，或通过 MCP SDK 测试工具调用。

---

## 测试工具 & 依赖

| 工具 | 用途 | 引入方式 |
|------|------|----------|
| Go 标准 `testing` | 测试框架 | 内置 |
| `testify/assert` | 断言简化 | `go get github.com/stretchr/testify` |
| `miniredis/v2` | 内存 Redis mock | `go get github.com/alicebob/miniredis/v2` |
| `go-sqlmock` | SQL mock | `go get github.com/DATA-DOG/go-sqlmock` |
| 手写 mock struct | LLM / Embedder / VectorStore mock | 项目内 `internal/testutil/` |

---

## 预期文件结构

```
gateway/
  internal/
    testutil/
      mock_llm.go         # mock BaseChatModel
      mock_embedder.go     # mock Embedder interface
      mock_vectorstore.go  # mock VectorStore interface
    config/
      config_test.go
    logger/
      logger_test.go
    ingest/
      chunker_test.go
      parser_test.go
    selfrag/
      nodes_test.go
      state_test.go
    memory/
      helpers_test.go      # formatConversation, extractJSON*, truncate
      short_term_test.go
      long_term_test.go
      extractor_test.go
    embedding/
      embedding_test.go    # (接口抽取后)
    qdrant/
      client_test.go       # parseSearchResults, distance mapping
```

---

## 实施步骤

1. **接口抽取** — 新增 `Embedder` / `VectorStore` 接口，修改 `Nodes` / `LongTermStore` 字段类型
2. **创建 testutil** — 手写 mock 实现
3. **P0 测试** — `ingest`（纯函数）、`state`、`memory helpers`
4. **P1 测试** — `config` → `logger` → `selfrag/nodes` → `memory/short_term` → `memory/extractor` → `memory/long_term`
5. **P2 测试** — `qdrant/client`（可选）
6. **CI 集成** — Makefile 添加 `test` target，确保 `go test ./...` 通过

---

## 覆盖率目标

| 包 | 目标覆盖率 |
|----|-----------|
| `ingest` | ≥ 90% |
| `config` | ≥ 80% |
| `logger` | ≥ 80% |
| `selfrag` | ≥ 75% |
| `memory` | ≥ 75% |
| 整体 | ≥ 70% |

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
- [x] **单元测试（v0.7.0）**
- [ ] 实际 LLM API 联调测试
- [ ] Redis Embedding 缓存
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

## 里程碑提交记录（2026-04-14）

本提交作为 v0.7.0 阶段收尾，在已有单元测试覆盖的基础上，完成运行与工具链侧的调整，并避免将覆盖率原始文件纳入版本库。

| 类别 | 内容 |
|------|------|
| 仓库 | `.gitignore` 忽略根目录及各包生成的 `coverage.out` |
| 编排 | `docker-compose`：Qdrant 健康检查改为不依赖镜像内 `wget` 的 TCP 探测；分区注释改为英文 |
| 构建 | Gateway 镜像构建阶段使用 Go 1.25；运行镜像补充 `wget` 以满足运维/探活需求 |
| MCP | `tools.go` / `tools_memory.go` 工具参数 struct 仅保留 `json` 标签，移除 `jsonschema` 描述字段（由工具注册层统一描述或由宿主生成 schema） |

覆盖率报告仅在本地或 CI 生成，不提交二进制/大段文本产物。

---

## 下一步计划

1. 实际 LLM API 联调（OpenAI / 豆包 Ark）
2. Redis Embedding 缓存（减少重复向量化请求）
3. 集成测试（testcontainers：MySQL + Redis + Qdrant）
4. 文档补充（README、部署指南）

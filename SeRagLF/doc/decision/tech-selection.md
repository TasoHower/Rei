# SeRagLF 技术选型文档

> 各组件技术选型、对比分析与决策理由

## 1. 整体架构决策：为什么是纯 Go

### 1.1 三种架构方案对比

| 维度 | 纯 Python | Go + Python 微服务 | **纯 Go** |
|------|----------|-------------------|----------|
| MCP 协议 | SDK 成熟 | Go SDK ✓ | Go SDK v1.5.0 ✓ |
| AI/ML 编排 | LangGraph ✓ | LangGraph (Python) | **Eino Graph** (Go 原生) ✓ |
| LLM 调用 | LangChain ✓ | LangChain (Python) | **Eino ChatModel** (OpenAI/Claude/Ark) ✓ |
| 并发性能 | asyncio/GIL 限制 | 各取所长 | goroutine 原生高并发 ✓ |
| 部署复杂度 | 低（1 服务） | 高（2 服务 + gRPC） | **低（1 二进制）** ✓ |
| 运行时内存 | 500MB+ | 700MB+ (Go+Python) | **~50MB** ✓ |
| 镜像大小 | ~500MB | ~550MB | **~30MB** (alpine) ✓ |
| 类型安全 | 运行时 | 部分编译期 | **全编译期** ✓ |
| 调试复杂度 | 低 | 高（跨进程/语言） | **低（单进程/单语言）** ✓ |

### 1.2 决策

**选择纯 Go**。关键转折点：

1. **Eino 的出现**：字节跳动 CloudWeGo 开源的 Eino 框架（10k+ Stars）提供了 Go 原生的有向循环图编排能力，完全对标 LangGraph，消除了"Go 无法编排 LLM 管线"的瓶颈
2. **Qdrant Go Client**：官方 gRPC 客户端成熟（v1.17），不再需要 Python 做中间层
3. **LLM 调用本质是 HTTP API**：三个 Grader + 一个 Generator 都是 HTTP JSON 调用，Go 完全胜任
4. **消除 gRPC 双跳**：原 Go → Python → Go 的回调链路被消除，Self-RAG 延迟降低 2-5ms

---

## 2. Self-RAG 编排框架

### 2.1 候选对比

| 框架 | 语言 | 循环图 | 条件边 | 状态管理 | 编译检查 | 生产验证 |
|------|------|--------|--------|---------|---------|---------|
| LangGraph | Python | ✓ | ✓ | ✓ | ✗ (运行时) | ✓ |
| **Eino Graph** | **Go** | **✓** | **✓** | **✓** | **✓ (泛型)** | **✓ (TikTok/豆包)** |
| LangChainGo | Go | ✗ | 有限 | ✗ | ✗ | ✗ (社区抱怨多) |
| 手写状态机 | Go | ✓ | ✓ | 手写 | ✓ | - |

### 2.2 决策：Eino Graph

**选用理由**：

1. **有向循环图原生支持**：`compose.NewGraph` + `AddEdge` + `AddBranch(NewGraphBranch(...))`，与 LangGraph API 1:1 映射
2. **编译期类型安全**：Go 泛型约束 `Graph[*State, *State]`，LangGraph 的 TypedDict 仅运行时检查
3. **生产级验证**：字节内部 TikTok、豆包、Coze 等产品线使用
4. **组件生态完整**：`eino-ext` 提供 OpenAI/Claude/Gemini/Ollama/Ark 官方集成
5. **可视化调试**：VSCode/GoLand 插件支持图可视化
6. **CloudWeGo 生态**：与项目已有的 Go 技术栈（Hertz 同源团队）一脉相承

**Eino 与 LangGraph 的 API 对应关系**：

| LangGraph (Python) | Eino (Go) |
|---------------------|-----------|
| `StateGraph(GraphState)` | `compose.NewGraph[*State, *State]()` |
| `workflow.add_node("name", fn)` | `graph.AddLambdaNode("name", compose.InvokableLambda(fn))` |
| `workflow.add_edge("a", "b")` | `graph.AddEdge("a", "b")` |
| `workflow.add_conditional_edges("node", fn, mapping)` | `graph.AddBranch("node", compose.NewGraphBranch(fn, mapping))` |
| `workflow.compile()` | `graph.Compile(ctx)` → `Runnable[I,O]` |
| `app.invoke(state)` | `runnable.Invoke(ctx, state)` |

---

## 3. MCP SDK

### 3.1 选型：MCP Go SDK v1.5.0

`github.com/modelcontextprotocol/go-sdk/mcp`

| 候选 | 维护者 | 版本 | 决策 |
|------|--------|------|------|
| **modelcontextprotocol/go-sdk** | 官方 + Google | v1.5.0 | **选用** |
| mark3labs/mcp-go | 社区 | v0.x | 不选（API 不稳定） |

**理由**：
- 官方 SDK，Google 协维护
- 同时支持 stdio + Streamable HTTP
- `mcp.AddTool` 泛型 API 自动推导 JSON Schema
- 完整支持 Tool / Resource / Prompt

---

## 4. 向量数据库

### 4.1 候选对比

| 指标 | **Qdrant** | Milvus | Weaviate | pgvector |
|------|-----------|--------|----------|----------|
| p50 延迟 | **4ms** | 6ms | 12ms | 18ms |
| p99 延迟 | **25ms** | 35ms | 65ms | 90ms |
| 内存/1M向量 | **3-4 GB** | 4.5-6.2 GB | 5 GB | - |
| Go gRPC 客户端 | **官方 ✓** | 官方 ✓ | 官方 ✓ | SQL driver |
| 混合搜索 | **原生** | 原生 | 原生 | 无 |
| 自托管 | **Docker 一键** | 复杂 | Docker | 依赖 PG |

### 4.2 决策：Qdrant

- **延迟最低**：p50 4ms，Self-RAG 单次查询可能触发 2-3 次检索，低延迟至关重要
- **Go 客户端成熟**：`github.com/qdrant/go-client` v1.17，gRPC 直连
- **内存效率最优**：Rust 编写，无 GC 抖动

---

## 5. LLM 策略

### 5.1 双模型分层

Self-RAG 管线中有 4+ 次 LLM 调用（3 个 Grader + 1 个 Generator + 可选 query rewrite）。Grading 占总延迟 60-70%，因此：

| 用途 | 模型 | 理由 |
|------|------|------|
| **Grader（评估）** | gpt-4o-mini | 二元 yes/no 判断，轻量模型即可；延迟降至 1/3 |
| **Generator（生成）** | gpt-4o | 需要强推理能力 |

通过 Eino 的 `eino-ext/components/model/openai` 封装，配置切换即可更换 Provider（OpenAI / Claude / 豆包 Ark / Ollama）。

### 5.2 结构化输出

所有 Grader 要求 LLM 返回 JSON：

```json
{"relevant": true}    // grade_documents
{"grounded": true}    // check_hallucination
{"useful": true}      // grade_answer
```

Go 侧通过 `json.Unmarshal` 解析，兜底逻辑处理非标准输出。

---

## 6. Embedding

### 6.1 选型

| 模型 | 维度 | 方式 | 适用场景 |
|------|------|------|---------|
| **OpenAI text-embedding-3-small** | 1536 | API | 开发/快速启动 |
| Harrier-oss-v1-0.6b | 1024 | 自托管 | 生产/数据安全 |
| BGE-M3 | 1024 | 自托管 | 多语言场景 |

通过 Eino 的 `eino-ext/components/embedding/openai` 封装。自托管模型可通过 OpenAI 兼容 API 接入。

---

## 7. 持久化与缓存

### 7.1 MySQL 8.4

用途：collection 元数据、文档摄入记录、审计日志。

DSN 格式：`root:password@tcp(host:3306)/seraglf?charset=utf8mb4&parseTime=True&loc=Local`

### 7.2 Redis 7 (纯缓存)

用途：embedding 向量缓存。**不做持久化**（关闭 RDB/AOF），设置 maxmemory + LRU 淘汰。

缓存 Key：`embed:{model}:{sha256(text)[:16]}` → 向量 bytes

TTL：24 小时

---

## 8. 配置管理

`github.com/spf13/viper`，加载优先级：默认值 → config.yaml → 环境变量（`SERAGLF_` 前缀） → CLI flag。

---

## 9. 依赖版本汇总

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/modelcontextprotocol/go-sdk` | v1.5.0 | MCP Server SDK |
| `github.com/cloudwego/eino` | v0.8.8 | LLM 编排框架（Graph/ChatModel/Embedder） |
| `github.com/cloudwego/eino-ext/components/model/openai` | v0.1.12 | OpenAI ChatModel 实现 |
| `github.com/cloudwego/eino-ext/components/embedding/openai` | latest | OpenAI Embedder 实现 |
| `github.com/qdrant/go-client` | v1.17.1 | Qdrant 向量库客户端 |
| `github.com/spf13/viper` | v1.21.0 | 配置管理 |
| `github.com/google/uuid` | v1.6.0 | UUID 生成 |
| `github.com/redis/go-redis/v9` | v9.18.0 | Redis 客户端（短期记忆存储） |
| `github.com/go-sql-driver/mysql` | v1.9.3 | MySQL 驱动（长期记忆 — 用户事实存储） |
| `google.golang.org/grpc` | v1.80.0 | gRPC（Qdrant 客户端依赖） |

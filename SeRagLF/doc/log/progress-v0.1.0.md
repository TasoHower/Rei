# SeRagLF 项目进度日志

## 2026-04-13 — 项目启动：服务规划与技术选型

### 完成事项

1. **项目定位确认**
   - 确定项目目标：构建生产级 Self-RAG MCP Server
   - Self-RAG 核心价值："System 2" 级自反思 RAG，具备文档相关性评估、幻觉检测、答案质量评估能力

2. **架构决策**
   - 确定 Go + Python 微服务架构（Go 主服务 + Python AI Engine）
   - Go 负责 MCP 协议、向量检索、缓存、健康检查
   - Python 负责 LangGraph Self-RAG 推理管线、Embedding 推理、文档解析
   - 服务间通过 gRPC (Protocol Buffers) 通信
   - 向量检索由 Go 侧统一执行，Python 通过 gRPC 回调获取检索结果

3. **技术选型完成**
   - MCP SDK：Go 官方 SDK v1.5.0（Google 协维护）
   - 向量数据库：Qdrant（p50 4ms，内存效率最优，原生混合搜索）
   - Self-RAG 编排：LangGraph 0.2.x（有向循环图，天然适配反思循环）
   - Embedding：分层策略（OpenAI API / Harrier-oss-v1 / BGE-M3）
   - LLM：Grader 用轻量模型、Generator 用强模型
   - 缓存：Redis（Embedding 向量缓存）
   - 可观测性：zap + structlog + Prometheus + OpenTelemetry

4. **规划文档输出**
   - `doc/design/architecture.md` — 完整架构规划（系统拓扑、服务拆分、数据流时序图、LangGraph 状态图设计、gRPC 双向通信设计、目录结构、部署架构）
   - `doc/decision/tech-selection.md` — 技术选型对比分析（语言职责划分、MCP SDK / 向量库 / Embedding / LLM / 编排框架 / 通信协议 / 缓存 / 可观测性，每项含候选对比表和决策理由）
   - `doc/design/api-design.md` — API 设计文档（7 个 MCP Tool 定义含参数 Schema 和 Go 结构体、3 个 MCP Resource、完整 gRPC Proto 定义、错误处理规范、配置文件示例、Makefile 目标）

### 当前状态

- [x] 服务规划
- [x] 技术选型
- [x] API 设计
- [ ] 项目脚手架搭建（Go module + Python project）
- [ ] Proto 文件编写与代码生成
- [ ] Go MCP Server 骨架实现
- [ ] Python AI Engine 骨架实现
- [ ] Self-RAG LangGraph 管线实现
- [ ] Qdrant 集成
- [ ] Docker Compose 编排
- [ ] 单元测试
- [ ] 集成测试
- [ ] 文档补充（README、部署指南）

### 下一步计划

1. 搭建项目脚手架（go.mod、pyproject.toml、Makefile、目录结构）
2. 编写 Proto 文件并生成 Go/Python 代码
3. 实现 Go MCP Server 骨架（MCP 工具注册 + gRPC 客户端）
4. 实现 Python AI Engine 骨架（gRPC 服务端 + LangGraph 空管线）
5. Docker Compose 编排基础设施（Qdrant + Redis）

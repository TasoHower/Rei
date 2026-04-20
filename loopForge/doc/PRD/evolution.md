# loopForge 版本演进流程

> **文档类型**：演进历程  
> **创建日期**：2026-04-17  
> **最后更新**：2026-04-17  
> **关联文档**：[PRD 目录](./PRD/README.md)、[进度日志目录](./log/README.md)

---

## 演进总览

loopForge 从 **可运行基座** 起步，历经 **流式事件驱动**、**Multi-Agent 协作**、**架构统一**、**代码重构**、**状态共享**、**MCP 接入**，最终演进为支持 **Skills 技能包** 的完整 Multi-Agent SDK。

```
v0.1.0 (2026-04-15) — 可运行基座
    │
    ├─ 核心能力：ChatModel 抽象、ToolInfo、流式对话
    │
    ↓
v0.2.0 (2026-04-15) — 流式事件驱动
    │
    ├─ 核心能力：事件流、类型安全 Payload、多工具链式调用
    │
    ↓
v0.3.0 (2026-04-15) — Agent Transfer
    │
    ├─ 核心能力：多 Agent 注册、平级移交、Orchestrator 编排
    │
    ↓
v0.4.0 (2026-04-16) — Agent 架构统一
    │
    ├─ 核心能力：简化 Agent 定义、修正事件报文、并发隔离
    │
    ↓
v0.4.1 (2026-04-16) — 包拆分 + 命名统一 + 结构化日志
    │
    ├─ 核心能力：包职责分离、Go 风格命名、开箱可观测
    │
    ↓
v0.5.0 (2026-04-16) — Variable 共享变量
    │
    ├─ 核心能力：Agent 间状态共享、VarStore、Manifest 自动装配
    │
    ↓
v0.6.0 (2026-04-17) — MCP 客户端与工具接入
    │
    ├─ 核心能力：MCP 工具自动发现、统一抽象、调试 API
    │
    ↓
v0.7.0 (2026-04-17) — Skills 技能包
    │
    ├─ 核心能力：可发现技能、两阶段加载、统一 shell 执行
    │
    ↓
未来版本 — 规划中
```

---

## 详细演进流程

### v0.1.0 — 可运行基座（2026-04-15）

**里程碑**：从设计文档到可运行代码

#### 核心问题
- 如何将设计文档落地为可运行代码？
- 如何选择并接入合适的 SDK？
- 如何抽象模型层以支持多种后端？

#### 解决方案
1. **依赖 `agent-sdk-go`** — 基于成熟 SDK 构建，避免重复造轮子
2. **`pkg/model` 抽象** — ChatModel 抽象，支持 Mock/Ark/Doubao 多种后端
3. **流式对话** — 支持流式输出，为后续事件驱动奠定基础
4. **工具支持** — ToolInfo 抽象，支持 function call

#### 关键交付
- ✅ `pkg/model` — ChatModel、ToolInfo、Message
- ✅ `pkg/model/adapters/` — agentsdk、doubao 适配器
- ✅ `pkg/agent` — Agent、Planner、RunnerAgent
- ✅ `pkg/runtime` — 请求/结果/事件/工具契约
- ✅ Demo — doubaodemo、loopforged、agentdemo

#### 架构特点
- **单 Agent 模式** — 仅支持单个 Agent 运行
- **同步请求 - 响应** — `Agent.Run()` 返回 `(*RuntimeOutcome, error)`
- **工具循环** — RunnerAgent 内部实现 tool call 循环

#### 局限性
- 无多 Agent 协作能力
- 同步阻塞，无法观测中间过程
- 无状态传递机制

**📄 详细文档**：[prd-v0.1.0-baseline.md](./PRD/prd-v0.1.0-baseline.md)

---

### v0.2.0 — 流式事件驱动（2026-04-15）

**里程碑**：从同步请求 - 响应到流式事件驱动

#### 核心问题
- 如何实时观测 Agent 执行过程？
- 如何支持细粒度的过程控制？
- 如何实现类型安全的事件 Payload？

#### 解决方案
1. **`Agent.Run` 流式化** — 返回 `<-chan *RuntimeEvent`，所有过程通过 channel 发射
2. **`EventPayload` sealed interface** — 消除 `Payload any`，编译期类型安全
3. **类型安全访问器** — `.Token()` / `.ToolStart()` / `.ToolEnd()` / `.Done()`
4. **多工具链式调用** — 四个算术工具 demo，覆盖复杂场景

#### 关键交付
- ✅ 流式 API — `Run()` 返回 channel
- ✅ 类型安全 Payload — sealed interface
- ✅ Emit 构造器 — 自动推导 Type
- ✅ 多工具 demo — add/subtract/multiply/divide
- ✅ `ModelName` 配置 — 支持模型标识

#### 架构升级
- **事件驱动** — 所有中间过程（token、tool、step、done）实时输出
- **细粒度观测** — 可观测每个 step、每次 tool 调用的详细过程
- **链式调用** — 支持多工具链式调用，覆盖复杂场景

#### 局限性
- 仍为单 Agent 模式
- 无 Agent 间状态传递
- 事件流消费方需自行处理错误事件

**📄 详细文档**：[prd-v0.2.0-streaming.md](./PRD/prd-v0.2.0-streaming.md)

---

### v0.3.0 — Agent Transfer（2026-04-15）

**里程碑**：从 React Agent SDK 升级为 Multi-Agent SDK

#### 核心问题
- 如何支持多个 Agent 平级协作？
- 如何实现 Agent 间的控制权移交？
- 如何保持对话历史连续性？

#### 解决方案
1. **`pkg/transfer` 包** — Transfer 能力独立，与 `pkg/agent` 解耦
2. **`Registry` 注册表** — 具名 Agent 配置注册，支持查找和校验
3. **Transfer 工具生成** — 自动生成 `transfer_to_{name}` 工具
4. **`Orchestrator` 编排器** — 实现 `agent.Agent` 接口，管理 transfer 循环
5. **对话历史传递** — Transfer 时传递 `msgs` 给目标 Agent

#### 关键交付
- ✅ `transfer.Registry` — Agent 配置注册表
- ✅ `transfer.Orchestrator` — 编排器实现
- ✅ Transfer 工具 — `transfer_to_{name}`
- ✅ 事件扩展 — `AgentTransferPayload`、`TransferChain`
- ✅ Demo — 三 Agent 场景（triage/math_expert/writer）

#### 架构升级
- **Multi-Agent** — 支持多个 Agent 平级移交
- **透明连续** — Transfer 时继承对话历史，事件流对消费方透明
- **类型安全** — Transfer 工具自动生成，参数和返回值类型安全
- **可观测** — `AgentTransfer` 事件、`TransferChain` 完整记录移交链

#### 局限性
- `AgentConfig` / `Registry` 冗余（v0.4.0 简化）
- Transfer 事件报文混乱（v0.4.0 修正）
- 并发隔离缺失（v0.4.0 解决）

**📄 详细文档**：[prd-v0.3.0-transfer.md](./PRD/prd-v0.3.0-transfer.md)

---

### v0.4.0 — Agent 架构统一（2026-04-16）

**里程碑**：简化架构、修正事件、并发安全

#### 核心问题
- Agent 定义冗余（`AgentConfig` 与 `RunnerAgent` 字段高度重叠）
- 事件报文混乱（Transfer 拦截时发射 `ToolCallStart` 但无 `ToolCallEnd`）
- 并发隔离缺失（多 segment 共享同一 Agent Graph 产生数据竞争）

#### 解决方案
1. **简化 Agent 定义** — 去掉 `AgentConfig` 和 `Registry`，直接在 `RunnerAgent` 上支持 Transfer
2. **修正事件报文** — Transfer 不产生 `ToolCall` 事件，仅发射 `AgentTransfer` 事件
3. **并发隔离** — per-run 克隆 agent template，在副本上注入运行时字段

#### 关键交付
- ✅ `RunnerAgent` 新增 `Description` 和 `handoffs`
- ✅ `AddHandoff` 方法 — 直接注册 Transfer 目标
- ✅ 事件修正 — Transfer 仅发射 `AgentTransfer`
- ✅ Per-Run Clone — 并发安全
- ✅ 删除 `AgentConfig` / `Registry` — 简化设计

#### 架构升级
- **简化设计** — 去掉冗余的 `AgentConfig` 和 `Registry`
- **事件清晰** — Transfer 不产生 `ToolCall` 事件
- **并发安全** — per-run 克隆，避免数据竞争
- **职责分离** — 编排层与单 Agent 运行循环职责分离

#### 局限性
- 包职责仍模糊（`pkg/agent` 单包过载）
- 命名仍混淆（`RunnerAgent` 与 `pkg/runner`）
- 日志缺失（Runner 编排过程无可观测输出）

**📄 详细文档**：[prd-v0.4.0-agent-unify.md](./PRD/prd-v0.4.0-agent-unify.md)

---

### v0.4.1 — 包拆分 + 命名统一 + 结构化日志（2026-04-16）

**里程碑**：代码质量与工程化改进

#### 核心问题
- 单包过载（`pkg/agent/` 既定义接口又承载实现，15 个文件）
- 命名混淆（`RunnerAgent` 与 `pkg/runner` 包名极易混淆）
- 日志缺失（Runner 编排过程无任何可观测输出）

#### 解决方案
1. **包拆分** — Agent 与 Runner 分离，`pkg/runner` 独立
2. **命名统一** — 接口 `Agent` → `Runnable`，结构体 `RunnerAgent` → `Agent`
3. **结构化日志** — `pkg/log` 独立包，默认 `slog.Default()`

#### 关键交付
- ✅ `pkg/agent` — 接口 + 实现 + transfer（8 个文件）
- ✅ `pkg/runner` — 编排器（2 个文件）
- ✅ 命名统一 — `Runnable`、`Agent`、`New()`、`Option`
- ✅ 结构化日志 — `Logger` 接口、默认 logger
- ✅ Transfer 函数导出 — 跨包调用

#### 架构升级
- **职责分离** — Agent 定义与 Runner 编排器分离
- **命名清晰** — 符合 Go 命名惯例（`-able` 接口、具体结构体名）
- **开箱可观测** — 结构化日志默认开启
- **Go 风格** — 遵循 Go 编程惯例

#### 成果
- 每个包文件数 < 10 个
- 无循环 import
- 所有 Transfer 函数可跨包调用
- 默认输出到 `slog.Default()`

**📄 详细文档**：[prd-v0.4.1-refactor.md](./PRD/prd-v0.4.1-refactor.md)

---

### v0.5.0 — Variable 共享变量（2026-04-16）

**里程碑**：Agent 间状态共享与参数上下文恢复

#### 核心问题
- Agent 间无状态传递（Transfer 只转发对话历史）
- Tool 无法积累上下文（每次调用都是无状态的）
- 跨 Agent 协作缺少共享空间（决策结果只能编码在自然语言里）

#### 解决方案
1. **`pkg/variable` 独立包** — Variable Store 独立，不耦合 agent 或 runner
2. **`VarStore` 线程安全 map** — `sync.RWMutex` 并发安全
3. **visitable 变量** — 自动注入 prompt，LLM 直接看到
4. **`var_set` 工具** — Agent 可修改变量，自动拦截 `const_` 前缀
5. **Manifest + Materialize** — SDK 自动装配变量

#### 关键交付
- ✅ `VarStore` — 线程安全 map
- ✅ `Variable` — key/value/visitable/description
- ✅ Context 集成 — `WithVarStore()`、`FromContext()`
- ✅ `var_set` 工具 — 自动拦截 `const_`
- ✅ Manifest — 静态变量清单
- ✅ Materialize — 自动装配

#### 架构升级
- **有状态传递** — Transfer 不仅转发对话历史，还传递结构化状态
- **Tool 上下文积累** — 工具执行结果可跨 step 保留
- **类型安全的共享空间** — 前序 Agent 的决策结果可结构化传递
- **零侵入访问** — Tool 通过 `context.Context` 访问 Store

#### 成果
- 多 goroutine 并发读写无 race condition
- Transfer 时 Store 正确传递到下游 Agent
- 访问延迟 < 1μs
- 内存占用 < 1KB/变量

**📄 详细文档**：[prd-v0.5.0-variable.md](./PRD/prd-v0.5.0-variable.md)

---

### v0.6.0 — MCP 客户端与工具接入（2026-04-17）

**里程碑**：工具发现自动化与统一抽象

#### 核心问题
- 工具发现需手写 ToolInfo，工作量大
- MCP tools 与本地 tools 使用不同抽象
- 调试 MCP 链路困难（无独立调试 API）

#### 解决方案
1. **`BootstrapToolInfos`** — SDK 自动从 MCP Server 发现工具并绑定
2. **统一 `ToolInfo` 抽象** — MCP tools 与本地 tools 使用同一套接口
3. **独立 debug API** — `loopforge/debug` 子包，与集成 API 分离
4. **Schema 归一化** — MCP inputSchema 转为 OpenAI function schema

#### 关键交付
- ✅ `MCPServerProfile` — MCP 配置（stdio/SSE）
- ✅ `BootstrapToolInfos` — 自动发现与绑定
- ✅ `MCPMappedTool` — 归一化 schema + Handle
- ✅ `loopforge/debug` — 独立调试 API（Ping/ListTools/CallTool）
- ✅ Schema 归一化 — 与本地工具同一套 schema

#### 架构升级
- **自动化发现** — SDK 自动从 MCP Server 发现工具
- **统一抽象** — MCP tools 与本地 tools 使用同一套 `ToolInfo`
- **简化集成** — 使用方只提供 MCP 配置，SDK 完成所有绑定逻辑
- **调试友好** — 独立的 debug API 用于链路排查

#### 成果
- 启动时 MCP 工具发现时间 < 2s
- 工具调用延迟 < 100ms
- 与 SeRagLF MCP Server 联调通过
- 本地工具与 MCP 工具混合使用无冲突

**📄 详细文档**：[prd-v0.6.0-mcp.md](./PRD/prd-v0.6.0-mcp.md)

---

### v0.7.0 — Skills 技能包（2026-04-17）

**里程碑**：可发现、可版本化、可审计的技能装载与注入

#### 核心问题
- 如何支持可发现的技能包？
- 如何版本化管理技能？
- 如何审计技能加载和执行？
- 如何统一技能脚本执行？

#### 解决方案
1. **`SKILL.md` 载体** — front matter 含元数据，body 含使用说明
2. **两阶段加载** — 阶段 A 解析 frontmatter，阶段 B 按需加载 body
3. **统一 shell 执行** — 所有脚本通过统一 shell tool 执行
4. **完整性校验** — 结构、路径、哈希、一致性校验
5. **安全策略** — allowlist、timeout、sandbox

#### 关键交付
- ✅ `SKILL.md` — 技能包标准格式
- ✅ `SkillSpec` / `SkillMeta` — 内部数据结构
- ✅ `SkillRegistry` — 技能注册与发现
- ✅ `SkillJob` / `SkillJobRunner` — 可执行作业
- ✅ 完整性校验 — 6 项强制校验
- ✅ 审计日志 — 记录加载和执行事件

#### 架构升级
- **可发现性** — 启动时自动扫描配置目录
- **版本化管理** — 每个技能携带版本信息
- **审计与合规** — 所有技能来自受信根目录，加载过程完整记录
- **灵活注入** — 支持 system/developer 消息片段注入
- **受控执行** — 脚本执行受策略约束

#### 成果
- 启动时技能扫描时间 < 1s（100 个技能以内）
- 按需加载时间 < 100ms
- 脚本执行超时可控（默认 30s）
- 无法从非受信目录加载技能

**📄 详细文档**：[prd-v0.7.0-skills.md](./PRD/prd-v0.7.0-skills.md)

---

## 演进趋势总结

### 架构演进

| 版本 | 架构特点 | 关键变更 |
|------|----------|----------|
| v0.1.0 | 单 Agent、同步请求 - 响应 | 基座 |
| v0.2.0 | 事件驱动、流式输出 | 流式化 |
| v0.3.0 | Multi-Agent、Transfer | 多 Agent |
| v0.4.0 | 架构简化、并发安全 | 简化 |
| v0.4.1 | 包拆分、命名统一 | 工程化 |
| v0.5.0 | 状态共享、VarStore | 状态管理 |
| v0.6.0 | MCP 接入、工具发现 | 生态集成 |
| v0.7.0 | Skills、技能包 | 可扩展 |

### 核心能力累积

```
v0.1.0 — ChatModel 抽象、ToolInfo
    +
v0.2.0 — 事件流、类型安全
    +
v0.3.0 — Transfer、Multi-Agent
    +
v0.4.0 — 架构简化、并发安全
    +
v0.4.1 — 包拆分、结构化日志
    +
v0.5.0 — VarStore、状态共享
    +
v0.6.0 — MCP、工具自动发现
    +
v0.7.0 — Skills、技能包
    =
    完整的 Multi-Agent SDK
```

### 设计原则演进

1. **v0.1.0-v0.2.0** — 可运行 → 可观测
2. **v0.2.0-v0.3.0** — 单 Agent → Multi-Agent
3. **v0.3.0-v0.4.0** — 复杂 → 简化
4. **v0.4.0-v0.4.1** — 功能 → 工程质量
5. **v0.4.1-v0.5.0** — 无状态 → 有状态
6. **v0.5.0-v0.6.0** — 封闭 → 开放生态（MCP）
7. **v0.6.0-v0.7.0** — 工具 → 技能包

### 未来方向（规划中）

- **v0.8.0** — Memory（长短期记忆）
- **v0.9.0** — Spawn（父子递归子任务）
- **v1.0.0** — 稳定版、完整文档、生产就绪

---

## 相关文档索引

### PRD 文档
- [v0.1.0](./PRD/prd-v0.1.0-baseline.md) — 可运行基座
- [v0.2.0](./PRD/prd-v0.2.0-streaming.md) — 流式事件驱动
- [v0.3.0](./PRD/prd-v0.3.0-transfer.md) — Agent Transfer
- [v0.4.0](./PRD/prd-v0.4.0-agent-unify.md) — Agent 架构统一
- [v0.4.1](./PRD/prd-v0.4.1-refactor.md) — 包拆分 + 命名统一
- [v0.5.0](./PRD/prd-v0.5.0-variable.md) — Variable 共享变量
- [v0.6.0](./PRD/prd-v0.6.0-mcp.md) — MCP 客户端与工具接入
- [v0.7.0](./PRD/prd-v0.7.0-skills.md) — Skills 技能包

### 进度日志
- [v0.1.0](./log/progress-v0.1.0.md)
- [v0.2.0](./log/progress-v0.2.0.md)
- [v0.3.0](./log/progress-v0.3.0.md)
- [v0.4.0](./log/progress-v0.4.0.md)
- [v0.4.1](./log/progress-v0.4.1.md)
- [v0.5.0](./log/progress-v0.5.0.md)
- [v0.5.1](./log/progress-v0.5.1.md)
- [v0.5.2](./log/progress-v0.5.2.md)
- [v0.6.0](./log/progress-v0.6.0.md)
- [v0.7.0](./log/progress-v0.7.0.md)

### 设计文档
- [architecture.md](./design/architecture.md) — 架构设计
- [multi-agent-engine.md](./design/multi-agent-engine.md) — Multi-Agent 引擎
- [mcp-tool-unification.md](./design/mcp-tool-unification.md) — MCP 工具统一

### 决策文档
- [sdk-selection.md](./decision/sdk-selection.md) — SDK 选型决策

---

**最后更新**：2026-04-17

# loopForge 项目进度日志 — v0.9.5

> **版本**：v0.9.5  
> **日期**：2026-04-29（文档初始化）  
> **里程碑**：全面修订 `doc/decision/` + `doc/design/` 技术文档，对齐代码事实  
> **上一版本**：[v0.9.4](progress-v0.9.4.md)（Spawn 子 Agent start/end 消息恢复）  
> **配套实施计划**：`doc/plan/plan-v0.9.5.md`

---

## 本版本目标

1. **修订 `sdk-selection.md`**：更新术语表、架构描述、循环内核说明，对齐 v0.8.0~v0.9.4 的代码事实。
2. **新增 ADR 文档**：为 v0.8.0~v0.9.4 期间形成但未落盘的关键决策创建独立 ADR 文档。
3. **修订 `doc/design/` 全部 6 份文档**：标记愿景 v.s. 实现状态，修正事实错误，补齐交叉引用。
4. **文档目录规范化**：为 `doc/decision/` 创建 README 索引，更新 `doc/PRD/README.md` 引用。

---

## 关键变更清单

### `doc/decision/`（1 修订 + 3 新增 + 1 README）

| 文件 | 类型 | 说明 |
|------|------|------|
| `sdk-selection.md` | **修订** | 移除 pkg/network，agent-sdk-go 角色修正为"模型适配层" |
| `event-semantics.md` | **新增** | ADR：agent_transfer 用于 spawn 生命周期 |
| `spawn-isolation.md` | **新增** | ADR：本地 channel → OutputCh 非阻塞转发 |
| `engine-layering.md` | **新增** | ADR：pkg/agent → internal/engine → pkg/runner 三层 |
| `README.md` | **新增** | 决策文档目录索引 |

### `doc/design/`（6 份修订）

| 文件 | 修订要点 |
|------|----------|
| `architecture.md` | 分层表未实现模块加"（规划）"，§3.2 修正 pkg/network 为 agent_transfer |
| `abstractions.md` | §0 架构图补 engine 层，§8 SpawnSpec 补 OutputCh/Lifecycle/MemoryDigest/AllowChildSpawn |
| `multi-agent-engine.md` | 头部加实现状态块，全文标记愿景 vs 实现，pkg/network→handoff/agent_transfer |
| `data-fusion.md` | §3.1 补充 spawn 事件映射说明，加 ADR 交叉引用 |
| `mcp-tool-unification.md` | §1.4 修正 debug import 路径 |
| `spawn-runtime-rules.md` | 尾部加 ADR 交叉引用指针 |

### `doc/PRD/README.md`

更新决策文档引用表，新增 3 份 ADR 文档的条目。

---

## 执行流程

⚠ 本版本采用**逐个文档确认制**。详见 `doc/plan/plan-v0.9.5.md` §3。执行顺序：

```
作业 #1  event-semantics.md (ADR)  → 你确认 → 我实施
作业 #2  spawn-isolation.md   (ADR)  → 你确认 → 我实施
作业 #3  engine-layering.md   (ADR)  → 你确认 → 我实施
作业 #4  sdk-selection.md 修订       → 你确认 → 我实施
作业 #5  architecture.md 修订        → 你确认 → 我实施
作业 #6  abstractions.md 修订        → 你确认 → 我实施
作业 #7  multi-agent-engine.md 修订   → 你确认 → 我实施
作业 #8  data-fusion.md 修订         → 你确认 → 我实施
作业 #9  mcp-tool-unification.md 修订 → 你确认 → 我实施
作业 #10 spawn-runtime-rules.md 修订  → 你确认 → 我实施
作业 #11 README + PRD/README.md       → 你确认 → 我实施
作业 #12 最终验证                    → 你确认 → 我实施
```

## ADR 文档规范

每份新增 ADR（event-semantics.md、spawn-isolation.md、engine-layering.md）必须包含：

1. **问题背景** — 为什么需要做这个决策
2. **候选方案** — 至少两种可比方案（含被否决方案）
3. **决策理由** — 为什么选最终方案
4. **后果** — 这个决策带来的影响
5. **关联文档** — 代码文件路径、设计文档引用
6. **核心代码片段** — 体现决策的关键 Go 代码
7. **ASCII 逻辑图** — 体现数据流/架构的字符图

---

## 当前代码状态

| 层次 | 当前状态 |
|------|----------|
| **sdk-selection.md 修订** | 待确认后实施 |
| **ADR × 3 新增** | 待确认后实施（每份含代码片段 + 逻辑图） |
| **design/ 修订 × 6** | 待确认后实施 |
| **README + 交叉引用** | 待确认后实施 |
| **验证** | 待确认后实施 |

---

## 验证状态

- **文档盘点**：已完成（调研阶段 A）
- **plan 定稿**：已完成（逐个文档确认制，ADR 含代码片段+逻辑图）
- **逐文档修改**：等待用户从作业 #1 开始确认

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-29 | 新增 v0.9.5 进度文档，初始化阶段覆盖 doc/decision/ + doc/design/ 修订计划。 |

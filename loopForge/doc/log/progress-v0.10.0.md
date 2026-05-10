# loopForge 项目进度日志 — v0.10.0

> **版本**：v0.10.0  
> **日期**：2026-05-10（文档撰写）  
> **里程碑**：Plan 模式——Agent 驱动的结构化计划与执行  
> **上一版本**：[v0.9.8](progress-v0.9.8.md)（transfer 语义拆分——EventSpawnStart/End 独立）  
> **配套实施计划**：`doc/plan/plan-v0.10.0.md`

---

## 本版本目标

1. **Plan 数据模型**（pkg/plan）：Plan / PlanStep / PlanStepStatus 类型定义，PlanState（线程安全，Runner 持有）
2. **Plan 工具**：`plan_generate`（Agent 生成 Plan，Runner 拦截解析）、`plan_update`（Agent 更新步骤状态）
3. **Runner runPlanLoop**：3 阶段编排——规划 → 执行 → 汇总；Plan 跨 transfer 传递
4. **RunModePlan** 枚举值 + Agent LoopState.Plan 字段
5. **Plan 事件**：`EventPlanGenerated` / `EventPlanStepStart` / `EventPlanStepEnd`
6. **test-server 集成**：plan 模式入口 agent、SSE 事件序列化、UI 按钮
7. **Plan 决策规则文档化**：6 条"需要 Plan"条件 + 5 条"不需要 Plan"条件 + 边界情况
8. **测试覆盖**：单元测试（模型/状态/工具参数）+ 集成测试（入口/拦截/事件/transfer 传递）+ 回归测试

---

## 关键变更清单

### Go 代码新增

| 路径 | 说明 |
|------|------|
| `pkg/plan/plan.go` | **新建** — Plan、PlanStep、PlanStepStatus 数据类型 |
| `pkg/plan/state.go` | **新建** — PlanState（线程安全，SetPlan/UpdateStepStatus/Snapshot） |

### Go 代码修改

| 文件 | 说明 |
|------|------|
| `pkg/runtime/request/request.go` | 新增 `RunModePlan` 枚举 |
| `pkg/runtime/event/event.go` | 新增 `EventPlanGenerated` / `EventPlanStepStart` / `EventPlanStepEnd` + 3 个 payload + accessor |
| `pkg/agent/agent.go` | LoopState 新增 `Plan *plan.Plan` 字段 |
| `pkg/runner/runner.go` | 新增 `runPlanLoop` + plan_generate 拦截逻辑 + plan_update Handle + Plan 注入 SystemInstructions + 事件发射 |
| `test-server/main.go` | 新增 `case "plan"` + `buildPlanAgent()` + SSE 序列化 plan 事件 |

### 不变范围（明确不需要修改）

| 文件 | 原因 |
|------|------|
| `pkg/agent/loop.go` | Plan 注入在 Runner 层修改 SystemInstructions（同 transfer prompt 模式），runLoopFullSystem 无需感知 Plan |
| `pkg/agent/spawn.go` | 子 Agent 不感知 Plan |
| `internal/engine/` | Plan 是 Runner 层概念，引擎层不变 |

### 文档变更

| 文件 | 说明 |
|------|------|
| `doc/PRD/prd-v0.10.0-plan.md` | **新建** — 产品需求文档（含 §3 Plan 决策规则章节） |
| `doc/design/14-plan-mode.md` | **新建** — 架构设计文档（含 §8.3 决策规则 Prompt） |
| `doc/plan/plan-v0.10.0.md` | **新建** — 实施计划（含阶段 E 决策规则 Prompt 模板） |
| `doc/log/progress-v0.10.0.md` | **本文件** — 进度日志 |

---

## 执行流程

按 plan 分 7 阶段实施（各阶段状态见下方）：

```
阶段 A   数据模型与新包（pkg/plan + RunModePlan）          📝 待编码
阶段 B   事件类型（event.go 新增 3 种事件 + payload）        📝 待编码
阶段 C   Agent 层（LoopState.Plan 字段）                    📝 待编码
阶段 D   Runner 层（runPlanLoop + 工具拦截 + Plan 注入）      📝 待编码
阶段 E   test-server 集成（入口 agent + UI）                 📝 待编码
阶段 F   测试（单元 + 集成 + 回归）                          📝 待编码
阶段 G   文档与验收                                          📝 待编码
```

## 当前代码状态

| 阶段 | 状态 |
|------|------|
| A — 数据模型与新包 | 📝 未开始 |
| B — 事件类型 | 📝 未开始 |
| C — Agent 层 | 📝 未开始 |
| D — Runner 层 | 📝 未开始 |
| E — test-server 集成 | 📝 未开始 |
| F — 测试 | 📝 未开始 |
| G — 文档与验收 | 📝 未开始 |

---

## 风险与待决

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| plan_generate 拦截与 transfer 拦截叠加导致逻辑复杂 | 拦截链长 | Phase 1 中 plan_generate 拦截优先级最高；Phase 2 中取消 plan_generate 拦截，替换为 plan_update Handle |
| plan_update 事件发射需要对比 PlanState 快照 | 两次快照比对开销 | 仅工具执行后对比，不每步扫描 |
| Agent 不按 plan_update 更新状态 | Plan 进度不准确 | 不强制 Agent 更新；Phase 3 自动补发未完成步骤的 plan_step_end |
| Agent 对"是否创建 Plan"的判断不一致 | 行为差异 | 决策规则标准化（PRD §3 + Prompt 模板） |

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-10 | 初稿：v0.10.0 进度文档，覆盖 Plan 模式的 7 阶段实施计划。 |
| 2026-05-10 | 同步设计变更：移除 pkg/agent/loop.go 改动（Plan 注入改由 Runner 层做），新增决策规则风险项。 |

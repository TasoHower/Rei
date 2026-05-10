# loopForge 实施计划 — v0.10.0（Plan 模式）

> **对应版本进度**：`doc/log/progress-v0.10.0.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的**任务拆分与执行顺序**；**先于编码**成文；与 `progress` 同步维护「未开始 / 进行中 / 已完成」状态（不重复粘贴全文，以 **progress 为版本真相源**）。  
> **主目标**：为 loopForge 增加 Plan 模式——Agent 驱动的结构化计划与执行，Runner 层级持有 Plan 并协调多 Agent 执行。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/PRD/prd-v0.10.0-plan.md` | 产品需求定义 |
| `doc/design/14-plan-mode.md` | 架构设计与详细技术方案 |
| `doc/log/progress-v0.10.0.md` | 版本目标、范围、风险、交付清单、与设计的交叉引用 |
| [`doc/design/02-runner-core.md`](../design/02-runner-core.md) | Runner 当前执行流程 |
| [`doc/design/03-events.md`](../design/03-events.md) | 事件协议 |
| [`doc/design/04-tools.md`](../design/04-tools.md) | 工具注册体系 |

---

## 2. 先决与确认（编码前完成）

- [ ] 定稿 `RunModePlan` 在 `request.go` 中的枚举值命名
- [ ] 定稿 Plan/PlanStep 数据模型字段（与 design doc 一致）
- [ ] 定稿 plan_generate / plan_update 工具的 JSON Schema
- [ ] `progress` 中「本版本目标」与本文「任务分阶段」**无**未解决冲突

---

## 3. 任务分阶段（建议顺序）

### 阶段 A — 数据模型与新包

1. **新建 `pkg/plan/` 包并创建 `plan.go`**：
   ```go
   package plan

   type Plan struct { ... }
   type PlanStep struct { ... }
   type PlanStepStatus string

   const (
       PlanStepPending    PlanStepStatus = "pending"
       PlanStepInProgress PlanStepStatus = "in_progress"
       PlanStepCompleted  PlanStepStatus = "completed"
       PlanStepFailed     PlanStepStatus = "failed"
       PlanStepSkipped    PlanStepStatus = "skipped"
   )
   ```
   包含 JSON tags、AssignedTo 字段（预留 team）、DependsOn 字段。

2. **创建 `pkg/plan/state.go`**：
   ```go
   type PlanState struct { mu sync.RWMutex; plan *Plan }
   
   func NewPlanState() *PlanState
   func (ps *PlanState) SetPlan(p *Plan) error          // 校验已存在则拒绝
   func (ps *PlanState) Plan() *Plan                     // 返回副本
   func (ps *PlanState) UpdateStepStatus(id, status, result) error
   func (ps *PlanState) Snapshot() *Plan                 // 全量快照（供比较变更）
   ```
   线程安全，SetPlan 单次写入（ErrPlanAlreadySet），自动记录 StartedAt/CompletedAt。

3. **在 `pkg/runtime/request/request.go` 新增 `RunModePlan`**：
   ```go
   const RunModePlan RunMode = "plan"
   ```

### 阶段 B — 事件类型

1. **在 `pkg/runtime/event/event.go` 新增 3 种事件类型**：
   ```go
   EventPlanGenerated EventMessageType = "plan_generated"
   EventPlanStepStart  EventMessageType = "plan_step_start"
   EventPlanStepEnd    EventMessageType = "plan_step_end"
   ```

2. **新增 payload 结构体**：
   ```go
   type PlanGeneratedPayload struct { Plan *plan.Plan `json:"plan"` }
   type PlanStepStartPayload struct {
       StepID      string `json:"step_id"`
       Description string `json:"description"`
       AssignedTo  string `json:"assigned_to"`
   }
   type PlanStepEndPayload struct {
       StepID  string            `json:"step_id"`
       Status  plan.PlanStepStatus `json:"status"`
       Result  string            `json:"result,omitempty"`
   }
   ```
   在 marker switch 中添加对应分支，添加类型安全访问器（`ev.PlanGenerated()` 等）。

### 阶段 C — Agent 层：LoopState.Plan 字段

1. **LoopState.Plan 字段**（`pkg/agent/agent.go`）：
   ```go
   type LoopState struct {
       // ...现有字段
       Plan *plan.Plan // non-nil 时表示处于 plan mode，供 transfer 传递
   }
   ```

   > **注意**：System Prompt 中的 `[Plan]` 块注入**不是**在这里完成。Plan 在 Runner 层注入 Agent.SystemInstructions，同 transfer prompt 的注入模式（`a.SystemInstructions += planFragment()`）。Agent 的 `runLoopFullSystem` 无需修改。

### 阶段 D — Runner 层：runPlanLoop

这是最核心的一环。

1. **Runner.Run() 入口判断**：
   ```go
   switch {
   case req.RunMode == request.RunModePlan:
       r.runPlanLoop(ctx, req, ch, runState, policy)
   case len(entryAgent.Handoffs()) > 0:
       r.runTransferLoop(ctx, req, ch, runState, policy)
   default:
       r.runSingleAgent(...)
   }
   ```

2. **runPlanLoop 主体**（在 `pkg/runner/runner.go`）：
   ```
   runPlanLoop:
     Phase 1: 规划
       - 创建 PlanState
       - 注入 plan_generate 工具 (ExtraTools)
       - 设置 ToolInterceptor 检测 plan_generate 调用
       - 运行 entryAgent
       - 若 PlanState 有 Plan → 进入 Phase 2
       - 若未生成 Plan → 简单 query，正常结束
     
     Phase 2: 执行
       - st.Plan = planState.Plan()
       - 注入 plan_update 工具 (ExtraTools)
       - 取消 plan_generate 拦截
       - 运行 Agent（含 transfer 检测）
       - 每步工具执行后对比 PlanState 变更，发射事件
       - 若 transfer → 继续执行下一 hop
       - 若 Agent 结束 → 进入 Phase 3
     
     Phase 3: 汇总
       - 自动补发未完成的 plan_step_end
       - 发射 query_end
   ```

3. **plan_generate 工具处理**：
   - 工具 Schema 定义（`planGenerateToolInfo()`）
   - ToolInterceptor 检测 `name == "plan_generate"`
   - Intercept 回调：JSON 反序列化 → PlanState.SetPlan() → 返回 tool result
   - 参数校验：标题非空、steps 至少 1 个

4. **plan_update 工具处理**：
   - 工具 Schema 定义（`planUpdateToolInfo()`）
   - 本地 Handle（非拦截）：解析参数 → PlanState.UpdateStepStatus() → 返回确认
   - 事件发射策略：工具执行后在 Runner 中对比 PlanState Snapshot 增量发射

5. **事件发射辅助**：
   ```go
   func (r *Runner) emitPlanEvents(ch chan<- *event.RuntimeEvent, prev, cur *plan.Plan, step int)
   ```
   对比 prev/cur 的步骤状态差异，对每个变更发射 PlanStepStart 或 PlanStepEnd。

### 阶段 E — test-server 集成

1. **`test-server/main.go`**：
   - 新增 `case "plan"` 在 handleChat 的 mode switch 中
   - 新增 `buildPlanAgent()` 函数（构造 plan 模式的 Agent）
   - 设置 `req.RunMode = request.RunModePlan`
   - SSE 序列化中添加 plan 事件的事件类型分支

2. **Plan 系统 Prompt**（含决策规则）：
   ```
   You are the loopForge test assistant in plan mode.
   
   ## Plan Decision Rules
   
   ### Call plan_generate when ANY of these is true:
   - Task needs 3+ sequential steps where later steps depend on earlier results
   - Task needs multi-agent coordination (transfer to different agents)
   - Task has independent subtasks that can run in parallel (spawn)
   - Steps require different tools or knowledge domains
   - Intermediate validation is needed before proceeding
   - Number of steps is uncertain — needs iterative execution
   
   ### Do NOT call plan_generate when ANY of these is true:
   - Single tool call will suffice
   - Direct answer without external tools or data
   - Conversational follow-up or clarification
   - Simple instruction within your own capabilities
   - User is correcting or refining within an existing plan
   
   When in doubt, prefer NOT to plan — try direct execution first.
   If execution shows the task is more complex, you can plan in the next round.
   
   ## Execution Flow
   When you DO call plan_generate:
   1. FIRST, call plan_generate with a structured plan (title + steps).
   2. THEN execute steps, using plan_update to track progress.
   3. FINALLY summarize results.
   ```

3. **`test-server/static/index.html`**：
   - 新增 plan 模式按钮（mode selector）
   - 新增触发词（示例 prompt）

### 阶段 F — 测试

1. **单元测试**：
   - Plan/PlanStep JSON 序列化/反序列化
   - PlanState 线程安全（并发 UpdateStepStatus + Plan）
   - `UpdateStepStatus` 步骤状态变更校验（不允许已终态 → pending）
   - `SetPlan` 重复写入拒绝
   - `plan_generate` 参数校验（缺 steps 拒绝）
   - `plan_update` 参数校验（非法 status 拒绝）

2. **集成测试**：
   - `RunModePlan` → `runPlanLoop` 入口识别
   - plan_generate 拦截 → PlanState 非空 → PlanGenerated 事件
   - plan_generate 未调用（简单 query）→ 正常结束
   - plan_update 更新状态 → PlanStepStart/End 事件
   - Plan 跨 transfer 传递（LoopState.Plan 非空）

3. **回归测试**：
   - 现有 `TestRunner_*` 测试不被影响（RunModeSingleAgent 路径不变）
   - spawn/transfer 模式不受影响

### 阶段 G — 文档与验收

1. 更新 `doc/design/03-events.md`：新增 plan 事件的 §2.x 章节
2. 更新 `doc/design/00-abstractions.md`：事件枚举表新增 plan 事件
3. 更新 `doc/design/02-runner-core.md`：新增 runPlanLoop 路径说明
4. `doc/acceptance/v0.10.0-acceptance.md`（若当前版本有验收流程要求）

---

## 4. 主要文件/包影响（清单，随实现打勾）

| 路径/包 | 说明 |
|---------|------|
| `pkg/plan/plan.go` | **新建** — Plan、PlanStep、PlanStepStatus 类型 |
| `pkg/plan/state.go` | **新建** — PlanState、UpdateStepStatus 方法 |
| `pkg/runtime/request/request.go` | 新增 RunModePlan 枚举 |
| `pkg/runtime/event/event.go` | 新增 3 种事件类型 + payload + accessor |
| `pkg/agent/agent.go` | LoopState.Plan 字段 |
| `pkg/runner/runner.go` | runPlanLoop + plan_generate 拦截 + plan_update Handle + Plan 注入 SystemInstructions + 事件发射 |
| `test-server/main.go` | case "plan" + buildPlanAgent + SSE 序列化 |
| `test-server/static/index.html` | Plan 模式 UI |
| `doc/design/03-events.md` | 新增 plan 事件章节 |
| `doc/design/00-abstractions.md` | 事件枚举/文件表更新 |
| `doc/design/02-runner-core.md` | 新增 runPlanLoop 路径 |

---

## 5. 与 `progress` 的同步约定

- **较大范围**或**任务顺序**变更时：**先**改 `doc/plan/plan-v0.10.0.md` **并**在 `doc/log/progress-v0.10.0.md` 对应段落**补一句**引用或**变更日志**行。
- **已处于执行中**的 `progress` 段落：遵循 `rei-doc-mandatory`「**不**改正在执行段」——仅动**未开始**部分，除非用户确认调整。

---

## 6. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-05-10 | 初稿：Plan 模式实施计划，7 阶段：数据模型 → 事件 → Agent 层 → Runner 层 → test-server → 测试 → 文档。 |

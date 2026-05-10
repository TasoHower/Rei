# loopForge 产品需求文档 — v0.10.0 Plan 模式

> **版本**：v0.10.0\
> **日期**：2026-05-10\
> **里程碑**：Plan 模式——Agent 驱动的结构化计划与执行\
> **状态**：计划稿\
> **关联文档**：[00-abstractions.md](../design/00-abstractions.md)、[02-runner-core.md](../design/02-runner-core.md)、[10-spawn.md](../design/10-spawn.md)、[plan-v0.10.0.md](../plan/plan-v0.10.0.md)

***

## 1. 概述

### 1.1 产品定位

**v0.10.0 Plan 模式**为 loopForge 增加一种新的执行模式——Agent 驱动的结构化计划与执行。入口 Agent 判断用户 query 的复杂度，复杂时调用 `plan_generate` 工具生成结构化 Plan，Runner 持有 Plan 并协调多 Agent（当前 Agent、spawn、transfer、未来 teams）共同执行。

### 1.2 核心价值

1. **结构化分解** — 复杂 Query 自动拆解为步骤，避免一次性溢出
2. **多 Agent 协调** — Plan 作为 Runner 层级的共享契约，在 transfer/spawn 之间自然传递
3. **进度可观测** — Plan 步骤状态实时可见（pending / in\_progress / completed / failed）
4. **渐进扩展** — Plan 数据模型预留 `AssignedTo` 字段，未来可路由到 agent teams
5. **不破坏现有模式** — Plan 模式是新增执行路径，single / transfer / spawn 不受影响

### 1.3 与前后版本的关系

| 版本         | 关系说明                                                                            |
| ---------- | ------------------------------------------------------------------------------- |
| **v0.9.8** | transfer 语义拆分（EventSpawnStart/End 独立）；v0.10.0 的 plan 工具拦截复用相同 ToolInterceptor 模式 |
| **v0.10.0** | Plan 模式基础实现：Plan 数据模型、plan\_generate 工具、Runner Plan 编排、事件、test-server 演示        |
| **v1.0+**  | Agent teams、Plan 自动执行（无需工具更新，Runner 自动追踪步骤完成）、Plan 图形化 UI                       |

***

## 2. 需求范围

### 2.1 需求项

| #  | 需求项                                                        | 优先级 |
| -- | ---------------------------------------------------------- | --- |
| 1  | Plan/PlanStep 数据模型（pkg/plan）                               | P0  |
| 2  | PlanState（Runner 持有，线程安全，transfer 传递）                      | P0  |
| 3  | plan\_generate 工具（Agent 调用，Runner 拦截解析）                    | P0  |
| 4  | plan\_update 工具（Agent 更新步骤状态）                              | P0  |
| 5  | Plan 注入 System Prompt（\[Plan] 块）                           | P0  |
| 6  | Runner 新增 runPlanLoop 执行路径                                 | P0  |
| 7  | RunModePlan 枚举值                                            | P0  |
| 8  | EventPlanGenerated / EventPlanStepStart / EventPlanStepEnd | P0  |
| 9  | LoopState.Plan 字段（跨 transfer 传递）                           | P0  |
| 10 | test-server 演示模式（plan mode + buildPlanAgent + UI）          | P0  |
| 11 | PlanStep.AssignedTo 预留 agent team 字段                       | P0  |
| 12 | **Plan 决策规则文档化**：Agent 判断"何时创建 Plan"的明确标准                  | P0  |

### 2.2 不在本版本范围

1. **Agent teams**（AssignedTo = "team:xxx" 的路由逻辑）
2. **自动步骤追踪**（Agent 完成工作后 Runner 自动更新 Plan，无需工具调用）
3. **Plan 图形化 UI**（当前仅 SSE 事件，UI 待后续）

***

## 3. Plan 决策规则

### 3.1 何时需要创建 Plan

Agent 按以下优先级规则判断：**满足任一条件，应当创建 Plan。**

| 条件                                          | 示例                                                       |
| ------------------------------------------- | -------------------------------------------------------- |
| **多步骤依赖**：任务需要 3 个或以上顺序步骤，且后一步依赖前一步的结果      | "先查财报数据，再用数据计算毛利率，然后和行业均值对比，最后写报告"                       |
| **多 Agent 协调**：需要转移（transfer）到不同 Agent 协作完成 | "让 A 检索数据，让 B 分析，让 C 写报告"                                |
| **并行子任务**：存在可独立拆分的子任务，适合 spawn              | "同时计算三组数据：营收、成本、利润"                                      |
| **跨领域步骤**：各步骤需要不同的工具或知识域                    | "步骤 1 用 SQL 查数据库，步骤 2 用 Python 统计分析，步骤 3 用 Markdown 写文档" |
| **有中间检查点**：需要验证中间结果后继续                      | "先计算 A 的结果，检查是否合理，再继续计算 B"                               |
| **步骤数不可预期**：无法在一个 LLM 调用中完成                 | "遍历目录下所有文件，对每个文件做处理"                                     |

### 3.2 何时不需要创建 Plan

\*\*满足任一条件，禁止创建 Plan。\*\*直接使用工具或回答即可。

| 条件                                      | 示例                                 |
| --------------------------------------- | ---------------------------------- |
| **单步工具调用**：仅需调用一个工具就能完成                 | "3 + 5 等于多少"（调用 add）               |
| **直接回答**：不依赖外部工具或数据                     | "什么是 loopForge"                    |
| **对话延续**：对前一轮结果的追问或澄清                   | "刚才的计算结果再帮我解释一下"                   |
| **简单指令**：Agent 自身能力足以完成                 | "帮我把结果格式化成表格"                      |
| **用户的否定/修正在已有 Plan 内**：已有 Plan 时不需要重新生成 | "第二步的结果不对，再算一遍"（用 plan\_update 修正） |

### 3.3 边界情况处理

| 场景                  | 处理方式                                     |
| ------------------- | ---------------------------------------- |
| Query 介于"复杂"和"简单"之间 | **偏向不生成 Plan**——先试工具回答，模型回答不完整时下一轮再 Plan |
| 已有 Plan 时用户给出新任务    | 不重新生成，在当前 Plan 中追加步骤（用 plan\_update 调整）  |
| Plan 步骤数超过 10       | 拆分 Plan——优先用 spawn 做并行子 Plan，或分阶段生成      |
| 用户明确要求"请一步步做"       | **创建 Plan**——用户指令优先                      |

***

## 4. 用户故事

### US-1：Agent 判断复杂度并生成 Plan

**作为** loopForge Agent\
**我希望** 在收到复杂 Query 时调用 `plan_generate` 生成结构化 Plan\
**以便** 将大任务拆解为可执行的步骤

**验收标准**：

- Agent 在 RunLoop 中调用 `plan_generate` 工具
- Runner（ToolInterceptor）拦截该调用，解析工具参数中的 Plan JSON
- Plan 存入 Runner 持有的 PlanState
- 后续 System Prompt 自动注入 `[Plan]` 块
- 简单 Query 不触发 plan\_generate → 正常执行

### US-2：Plan 跨 transfer 传递

**作为** loopForge Agent（transfer 中的下游 Agent）\
**我希望** 看到上游 Agent 制定的 Plan\
**以便** 了解整体目标和当前步骤

**验收标准**：

- Plan 通过 LoopState.Plan 传递到下一个 Agent
- 下游 Agent 的 System Prompt 包含 `[Plan]` 块（含所有步骤状态）
- 下游 Agent 可调用 `plan_update` 更新步骤状态

### US-3：Plan 步骤状态实时可观测

**作为** test-server 用户\
**我希望** 看到 Plan 的生成和各步骤的状态变化\
**以便** 了解执行进度

**验收标准**：

- Plan 生成时发射 `EventPlanGenerated` 事件（含完整 Plan JSON）
- 步骤状态变更为 in\_progress 时发射 `EventPlanStepStart`
- 步骤状态变更为 completed/failed 时发射 `EventPlanStepEnd`
- 前端 SSE 事件流可解析并展示

***

## 5. 功能需求

### 5.1 Plan 数据模型

```go
type Plan struct {
    ID          string        `json:"id"`
    Title       string        `json:"title"`
    Description string        `json:"description,omitempty"`
    Steps       []PlanStep    `json:"steps"`
    CreatedAt   time.Time     `json:"created_at"`
}

type PlanStep struct {
    ID          string          `json:"id"`
    Description string          `json:"description"`
    AssignedTo  string          `json:"assigned_to"` // "self" | "spawn" | "transfer:AgentName" | "team:TeamName"(预留)
    DependsOn   []string        `json:"depends_on,omitempty"`
    Status      PlanStepStatus  `json:"status"`
    Result      string          `json:"result,omitempty"`
    StartedAt   *time.Time      `json:"started_at,omitempty"`
    CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type PlanStepStatus string

const (
    PlanStepPending    PlanStepStatus = "pending"
    PlanStepInProgress PlanStepStatus = "in_progress"
    PlanStepCompleted  PlanStepStatus = "completed"
    PlanStepFailed     PlanStepStatus = "failed"
    PlanStepSkipped    PlanStepStatus = "skipped"
)
```

### 5.2 PlanState（Runner 持有）

```go
type PlanState struct {
    mu   sync.RWMutex
    Plan *Plan
}
```

方法：

- `SetPlan(plan)` / `Plan()` — 读写 Plan
- `UpdateStepStatus(stepID, status)` — 更新单步状态（线程安全）
- `SetStepResult(stepID, result)` — 设置步骤结果文本

### 5.3 plan\_generate 工具

**协议**（工具名 `plan_generate`）：

```json
{
  "name": "plan_generate",
  "description": "Generate a structured execution plan for a complex task.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "title": { "type": "string", "description": "Short plan title" },
      "description": { "type": "string", "description": "Overall plan description" },
      "steps": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "id": { "type": "string" },
            "description": { "type": "string" },
            "assigned_to": { "type": "string", "description": "self | spawn | transfer:AgentName | team:TeamName" },
            "depends_on": { "type": "array", "items": { "type": "string" } }
          },
          "required": ["id", "description"]
        }
      }
    },
    "required": ["title", "steps"]
  }
}
```

**Runner 拦截**（复用 ToolInterceptor 模式）：

1. Agent 调用 `plan_generate` → RunLoop 返回 `InterceptedCall`
2. Runner 在 runPlanLoop 中解析 `plan_generate` 的参数
3. 反序列化为 `*Plan`，存入 `PlanState`
4. 发射 `EventPlanGenerated` 事件
5. 返回 tool\_result 给 Agent（确认 Plan 已创建）

**注入 System Prompt**：

```
[Plan]
Title: 多步骤计算

| Step | Description | Assigned To | Status |
|------|-------------|-------------|--------|
| 1 | 计算 123 * 456 | self | completed |
| 2 | 将结果加 789 | self | in_progress |

Complete steps: mark as completed via plan_update.
```

### 5.4 plan\_update 工具

**协议**（工具名 `plan_update`）：

```json
{
  "name": "plan_update",
  "description": "Update the status and/or result of a plan step.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "step_id": { "type": "string", "description": "Step ID to update" },
      "status": { "type": "string", "enum": ["in_progress", "completed", "failed", "skipped"] },
      "result": { "type": "string", "description": "Optional result text for the step" }
    },
    "required": ["step_id", "status"]
  }
}
```

### 5.5 事件

```go
const EventPlanGenerated EventType = "plan_generated"
const EventPlanStepStart  EventType = "plan_step_start"
const EventPlanStepEnd    EventType = "plan_step_end"
```

### 5.6 LoopState.Plan 字段

```go
type LoopState struct {
    // ... 现有字段
    Plan *Plan // non-nil 时表示处于 plan mode
}
```

***

## 6. Runner 执行流程

### 6.1 runPlanLoop

```
Runner.Run(ctx, req) when RunMode == RunModePlan:
  │
  ├── 构造 PlanState (空)
  ├── 构造 LoopState { PlanState, VarStore, ... }
  │
  ├── Phase 1: 分析 & 规划
  │     entryAgent 运行:
  │       System Prompt 包含计划决策规则
  │       工具清单包含 plan_generate (外加其他工具)
  │       Agent 分析 query:
  │         ├── 简单 → 不调 plan_generate → 正常结束
  │         └── 复杂 → 调 plan_generate → Runner 拦截
  │
  ├── [若 PlanState.Plan == nil] → 简单 query，postRunLoop 收尾
  │
  ├── Phase 2: Plan 执行
  │     Runner 将 Plan 注入 Agent.SystemInstructions（同 transfer prompt 模式）
  │     Agent 以 plan_update 标记进度
  │     Agent 通过工具 / spawn / transfer 完成各步骤
  │     步骤状态变化 → 发射对应事件
  │
  └── Phase 3: 汇总
         Agent 总结执行结果
         自动补发 plan_step_end
         收尾
```

### 6.2 与 transfer/spawn 的交互

- **Transfer**：Plan 通过 `LoopState.Plan` 随 transfer 传递。每个 Agent 运行前 Runner 注入带当前状态的 `[Plan]` 块到 `SystemInstructions`
- **Spawn**：父 Agent 生成 Plan，子 Agent 不感知 Plan。父 Agent 在步骤中调用 spawn\_subagent，完成后 plan\_update

***

## 7. 技术需求

### 7.1 新增包

| 包路径                 | 内容                                                |
| ------------------- | ------------------------------------------------- |
| `pkg/plan/plan.go`  | Plan、PlanStep、PlanStepStatus 类型定义、JSON tags       |
| `pkg/plan/state.go` | PlanState（线程安全）+ UpdateStepStatus / SetStepResult |

### 7.2 修改文件

| 文件                               | 变更                                                      |
| -------------------------------- | ------------------------------------------------------- |
| `pkg/runtime/request/request.go` | 新增 `RunModePlan`                                        |
| `pkg/runtime/event/event.go`     | 新增 3 种事件类型 + 对应 payload                                 |
| `pkg/agent/agent.go`             | LoopState 新增 `Plan *plan.Plan` 字段                       |
| `pkg/runner/runner.go`           | 新增 `runPlanLoop`、plan 工具拦截逻辑、Plan 注入 SystemInstructions |
| `test-server/main.go`            | 新增 case "plan"、buildPlanAgent、plan 系统 prompt（含决策规则）     |
| `test-server/static/index.html`  | 新增 plan 模式按钮和示例 prompt                                  |

### 7.3 不变的范围

- `pkg/agent/loop.go` `runLoopFullSystem` — **不修改**（Plan 在 Runner 层注入 SystemInstructions，同 transfer prompt 模式）
- `pkg/agent/spawn.go` — 不修改（子 Agent 不感知 Plan）
- `internal/engine/` — 不修改（Plan 是 Runner 层概念）

***

## 8. Plan 决策规则在 System Prompt 中的表述

以下为 Agent 的 System Prompt 中用于判断"是否创建 Plan"的指令文本。任何 Plan 模式的 Agent 都应包含此指令。

```
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
```

***

## 9. 验收标准

| # | 验收项                                                | 验证方式                           |
| - | -------------------------------------------------- | ------------------------------ |
| 1 | RunModePlan 可被 Runner.Run() 识别进入 runPlanLoop       | 单元测试                           |
| 2 | Agent 调用 plan\_generate → Runner 拦截并解析为 Plan       | 集成测试 + SSE 事件                  |
| 3 | Plan 注入 System Prompt（\[Plan] 块在 CallLLMStart 中可见） | CallLLMStart 事件检查 SystemPrompt |
| 4 | Agent 调用 plan\_update → PlanState 更新 + 事件          | 单元测试                           |
| 5 | Plan 跨 transfer 传递（LoopState.Plan 非空）              | transfer 集成测试                  |
| 6 | plan\_generate 参数校验（缺 steps 拒绝）                    | 单元测试                           |
| 7 | 决策规则文档化：PRD §3 + 设计 doc + 系统 prompt 均包含            | 文档审查                           |
| 8 | test-server plan 模式可正常交互                           | 端到端测试                          |

***

## 10. 风险与待决

| 风险                                       | 影响            | 缓解                                  |
| ---------------------------------------- | ------------- | ----------------------------------- |
| Agent 不自觉调用 plan\_generate（简单 query 也调用） | 不必要的 overhead | System Prompt 写入决策规则 + "有疑问时不 Plan" |
| Plan 步骤数与 LLM 输出不一致                      | Plan 不完整      | 不强制覆盖率，Agent 可在执行中调整                |
| 边界情况判断不一致                                | 不同 Agent 行为差异 | 决策规则标准化，见 §3                        |

***

## 11. 参考文档

- [00-abstractions.md](../design/00-abstractions.md)
- [02-runner-core.md](../design/02-runner-core.md)
- [10-spawn.md](../design/10-spawn.md)
- [07-transfer.md](../design/07-transfer.md)
- [03-events.md](../design/03-events.md)
- [04-tools.md](../design/04-tools.md)

***

## 变更日志

| 日期         | 说明                                                               |
| ---------- | ---------------------------------------------------------------- |
| 2026-05-10 | 初稿：v0.10.0 Plan 模式 PRD。                                           |
| 2026-05-10 | 新增 §3 Plan 决策规则：明确的创建/不创建条件 + 边界处理。新增 §8 System Prompt 中的决策规则表述。 |


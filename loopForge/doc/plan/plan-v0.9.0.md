# loopForge 实施计划 — v0.9.0（核心约束）

> **对应版本进度**：`doc/log/progress-v0.9.0.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的**任务拆分与执行顺序**；**先于编码**成文；与 `progress` 同步维护「未开始 / 进行中 / 已完成」状态（不重复粘贴全文，以 **progress 为版本真相源**）。  
> **主目标**：补全 spawn 运行时核心约束——子 Agent 非流式、父取消级联子、全树预算共享、MaxConcurrentSpawns、internal/engine 实装对接。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.0.md` | 版本目标、范围、交付清单 |
| `doc/design/spawn-runtime-rules.md` §5 / §2 / §3.8 | 非流式、级联取消、预算硬限制 |
| `doc/design/multi-agent-engine.md` §3.8 | spawn 产品语义与硬限制 |
| `doc/decision/sdk-selection.md` | spawn 在引擎中的职责边界 |
| `doc/log/progress-v0.8.0.md` | Slice1 现状与后续 TODO |

---

## 2. 先决与确认（编码前完成）

- [ ] `progress` 中「本版本目标」与本文「任务分阶段」**无**未解决冲突。
- [ ] 确认 `internal/engine` 与 `pkg/agent` 之间的适配方案：薄适配层转调 vs 重写 engine 版本。

---

## 3. 任务分阶段（建议顺序）

### 阶段 A — 非流式包装

1. **实现 `wrapNonStream`**：在 `pkg/model`（或子 ChatModel 包装层）新增强制非流式适配点；确保子路径独立于根路径流式配置；trace/日志字段 `llm_mode=non_stream`。
2. **接入 spawn 路径**：在 `defaultSpawner.runChild` 中调用 `wrapNonStream(agent.ChatModel())`；引擎忽略 `SpawnSpec.AllowStream` 字段（仅保留，不读取）。
3. **写测**：子 ChatModel 调用时 `stream=false`；`AllowStream=true` 仍被引擎忽略。

### 阶段 B — 级联取消

1. **实现 `RunState.Terminate(reason)`**：父 Run 进入终态时调用所有 `SpawnHandle.Close()`；`Children` map 清空；`ActiveChildRuns = 0`。
2. **接入父终态路径**：在 `Runner` 或引擎层，父 Run 完成/取消/错误时触发 `Terminate`。
3. **写测**：父中途取消 → 子立即退出；in-flight function-call 进入 recorder 的 `Error` 字段。

### 阶段 C — 全树预算共享 + MaxConcurrentSpawns

1. **实现 `BudgetCounter`**：线程安全；`TokensUsed` / `CostUSD` / `TokensMax` / `CostUSDMax`；`Allow()` 方法检查是否超限。
2. **根 Run 持有 BudgetCounter**：子树共享同一实例；`LoopPolicy.AllowStep` / `AllowSpawn` 每步前后累加并检查。
3. **实现 `MaxConcurrentSpawns` 校验**：`LoopPolicy.AllowSpawn` 中检查 `ActiveChildRuns < MaxConcurrentSpawns`。
4. **写测**：全树预算超额 → `SpawnError{Code: "budget_exceeded"}`；并发子数超限拒绝。

### 阶段 D — internal/engine 实装对接

1. **实装 `internal/engine.Spawner`**：实现 `Spawn(ctx, parent, spec) (*SpawnHandle, error)` 接口；整合阶段 A/B/C 所有约束。
2. **实装 `internal/engine.LoopPolicy`**：默认实现含 `MaxDepth=2`、`MaxConcurrentSpawns`、`BudgetCounter`。
3. **与 `pkg/runner` 衔接**：Runner 注入 `Spawner` 依赖；`RunLoop` 工具分发路径统一处理 spawn。
4. **写测**：engine 层面 policy 拒绝、深度、并发、ctx 取消。

---

## 4. 主要文件/包影响

| 路径/包 | 说明 |
|---------|------|
| `pkg/model/chatmodel.go` 或子模块 | `wrapNonStream` 强制非流式适配点 |
| `internal/engine/spawn.go` | `Spawner` 完整实装；`runChild` 调用 `wrapNonStream` |
| `internal/engine/runstate.go` | `Terminate(reason)` + `Children` map 管理 |
| `internal/engine/policy.go` | `LoopPolicy` 默认实现（含 `BudgetCounter`、`MaxConcurrentSpawns`） |
| `internal/defaults/loop.go` | `BudgetTokensTreeDefault` / `BudgetUSDTreeDefault` 默认值；`MaxConcurrentSpawnsDefault` |
| `pkg/runner/runner.go` | 注入 `Spawner`；父终态触发 `Terminate` |
| `pkg/runtime/exchange` | `BudgetCounter` 结构体（若放 exchange） |

---

## 5. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：从原 v0.9.0 PRD 拆分出的核心约束子版本实施计划 |

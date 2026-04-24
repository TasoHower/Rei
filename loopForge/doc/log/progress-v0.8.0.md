# loopForge 项目进度日志 — v0.8.0

> **版本**：v0.8.0  
> **日期**：2026-04-24（计划稿）  
> **里程碑**：**动态子 Agent（spawn）** — 在运行中通过受控内置工具（默认 `spawn_subagent`）创建子 `Run`，完成 **Spawner 实装**、**策略硬限制**、**结果回注**、**观测与成本聚合**；与 v0.7.0 **Skills** 的 `skill_ids` 子任务参数正式接线。  
> **设计依据**：`doc/design/multi-agent-engine.md` §3.8、`**doc/design/spawn-runtime-rules.md`**（运行时五条逻辑规则）、`doc/decision/sdk-selection.md`。  
> **配套实施计划**：`doc/plan/plan-v0.8.0.md`（任务分阶段；先于编码）。  
> **上一版本**：[v0.7.0](progress-v0.7.0.md)（Skills 装载与注入）。

---

## 运行时逻辑规则（必读）

**所有** v0.8.0 的设计与实施决策**须** 遵从 `**doc/design/spawn-runtime-rules.md`**：


| 编号  | 规则摘要                                                                   |
| --- | ---------------------------------------------------------------------- |
| §1  | **父子上下文默认隔离**；父级可向子级下传**经精简的短期记忆**，但**不得**自动复制父全量 transcript。          |
| §2  | **父 Run 终态** 时，**所有** 子 Agent（含等待唤醒中的）**必须** 退出。                       |
| §3  | 子单次执行结束后是**直接结束** 还是**进入可被父再次唤醒** 的等待态，**由父在 spawn 时决定**。              |
| §4  | 子 Run 必须在**独立 goroutine**（或等价异步）推进，**不得** 长期同步阻塞父主路径。                  |
| §5  | **嵌套 spawn 默认禁止**：子级是否能再次 spawn 由父级在 `SpawnSpec` 中**显式授权**（`AllowChildSpawn`），**逐级** 显式、**不传递不继承**，与 `MaxDepth` **取最严**。 |
| §6  | **子 Run 内** 的 LLM 调用**强制非流式**（`non-streaming`），以降低网络开销；与父/根流式配置**解耦**。 |
| §7  | 子工具集**与父同源**；父级以 `tool_white_list` + `tool_block_list` 双名单收敛子级可见工具（**白先黑后**）；子 ReAct **中间** function-call / tool_result 对父**不可见**；子级**只回最终** `SpawnResult`，**附带** `ToolInvocations` 摘要供父 Agent 外发。 |


> 本文与 `plan` / `multi-agent-engine` 的细节描述若与上述规则冲突，**以 `spawn-runtime-rules.md` 为准**。

---

## 本版本目标

1. **Spawner 落地**：实现 `internal/engine.Spawner.Spawn(ctx, parent, spec)` —— 校验 `LoopPolicy` → 分配子 `RunID` → 构造子 `Agent`（`Clone` + 子任务 system 拼接）→ 解析并注入 `SpawnSpec.SkillIDs` → 在**独立协程**中推进子 `RunLoop`（**遵守 §4**）→ 组装 `exchange.SpawnResult` 回传父级。
2. **内置工具面**：以 `model.ToolInfo` 注册 `spawn_subagent`（名称见 `internal/defaults.BuiltinSpawnToolName`，可覆写）；JSON Schema 与 `exchange.SpawnSpec` 字段对齐（`task` / `system_addendum` / `skill_ids` / `tool_allowlist` / `loop` / `model_override` 等）。
3. **硬限制（引擎强制）**：**v0.8.0 决议** —— `MaxDepth = 2`（root + 一层子 Agent），`MaxConcurrentSpawns` 取 `internal/defaults` 个位数默认；**预算（tokens / USD）全树共享**，根 Run 持有 `BudgetCounter`，子级共享同一计数器；超限返回 `SpawnRejected` + 结构化 `SpawnError`（`depth_or_concurrency` / `budget_exceeded`），**不得**静默成功。
4. **父子上下文隔离（§1）**：子 Run 默认仅注入 `task` / `system_addendum` / 引擎元数据；若启用「精简短期记忆下传」，须在 `SpawnSpec` 中**显式**字段，并在文档中写清默认策略（**默认关闭**或保守上限）。
5. **生命周期（§2 / §3）**：父 Run 终态时强制回收所有子 Agent；子单次执行结束后是**即回收**还是**进入可唤醒等待**由 spawn 参数决定，**不得**由子侧自行决定。
6. **异步执行（§4）**：子 Run 与父主路径**解耦**——父侧在子未完成期间仍能继续推理 / 工具调度 / 取消；结果通过 channel / 回调 / 工具结果回填，并保证 `ActiveChildRuns` 与 `MaxConcurrentSpawns` 一致。
7. **嵌套 spawn 授权（§5）**：`SpawnSpec` 新增 `AllowChildSpawn bool`（**默认 `false`**）；未授权时**不**为子级注册 `spawn_subagent` 工具；即便授权，也仍受 `MaxDepth` 上限约束（取最严）。授权**仅**对当前被拉起的子级生效，孙子级须由其父再次显式授权。
8. **子内非流式（§6）**：在子 `ChatModel` 或等效包装上**强制** `stream=false`；trace / 日志中可观测（如 `llm_mode=non_stream`）。**v0.8.0 决议**：`SpawnSpec.AllowStream` 字段**保留但不开放**，引擎忽略该字段，`spawn_subagent` 工具 JSON Schema **不**暴露该字段，任何「打开流式」的需求默认拒绝。
9. **子工具集策略（§7）**：子 `ToolRegistry` **复用** 父同源工具表（含 MCP 与内置）；`SpawnSpec` 新增 `ToolBlocklist`（与原 `ToolAllowlist` 配套），构造子级时按「**白先黑后**」过滤（白名单空表示不收敛，黑名单始终生效）；**v0.8.0 决议**：双名单条目**支持 glob 通配**（`*` / `?`，如 `mcp:foo/*`），按 MCP 前缀整段屏蔽，防止逐条枚举导致 `SpawnSpec` / `system_prompt` 同步爆炸。子 ReAct 期间**不**把中间 function-call / tool_result 流式回填到父，最终在 `SpawnResult.ToolInvocations` 中**结构化** 汇总（`Name` / `Arguments` / `ResultDigest` / `StartedAt` / `EndedAt` / `Error` / `MCPSource`），由父 Agent 决定是否向外部事件流转发。
10. **Skills 接线**：`SpawnSpec.SkillIDs` 经 `SkillRegistry` 解析为可注入块；合并顺序与 v0.7.0 根 Agent 一致；`skill_id` 不存在时的策略（拒绝 spawn / 区分错误码）须在「待决」表中定稿其一。
11. **观测与聚合**：slog 记录子 Run 起止与拒绝原因；子 Run 的 token / cost 汇总进父 `RunMetrics`（与 `RuntimeOutcome.ChildRunIDs` 协同）；OpenTelemetry 用 link span 或子 span 表达父子关系，携带 `parent_run_id`、`depth`、`allow_child_spawn`、`tool_filter_size`。
12. **流式事件**：`pkg/runtime/event` 增加 `spawn_start` / `spawn_end`（或等价 `EventMessageType`）+ Payload（`child_run_id` / `parent_run_id` / `depth` / `tool_invocations_count`）；子级 ReAct 中间步骤**不**进入父侧事件流（§7），需在 `Runner` / 事件桥层做过滤；与 `data-fusion.md`、历史 `prd-v0.2.0-streaming` 命名对齐时统一记录。
13. **文档与验收**：新增 `doc/acceptance/v0.8.0-acceptance.md`；更新 `multi-agent-engine.md` §3.8 实现状态指针；如引入独立 PRD 放 `doc/PRD/prd-v0.8.0-spawn.md`（可选，非阻塞）。
14. **父结束 → 子立即停止（§2 决议）**：父 Run 进入终态时**立即** 取消所有子 Agent ctx，**不**等待子 loop 当前回合跑完；子级 in-flight `function_call` 进入 recorder 的 `Error` 字段，但**不**再产出 `SpawnResult`；写测覆盖「父中途取消子立即退出」。
15. **唤醒能力保留 / 验收只覆盖 ephemeral（§3 决议）**：`SpawnHandle.inbox` + `ChildMailbox.in` 通道铺好，`SpawnSpec.Lifecycle` 字段保留，但 v0.8.0 验收**只**覆盖 `LifecycleEphemeral`；`linger` 端到端测试入 v0.9。
16. **有活跃子 Agent 时禁止 `agent_transfer`**：`RunState.ActiveChildRuns > 0` 时父 Agent **不可** transfer——root agent system prompt **显式写入**该约束，`agent_transfer` 工具 Handle 内做**硬校验**，命中返回 `Code = "transfer_blocked_active_children"`；子全部回收后方可 transfer。

---

## 现状与边界（相对 v0.7.0）


| 层次              | 现状（计划起点）                                                                                                                           |
| --------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| **契约**          | `exchange.SpawnSpec` / `SpawnResult` / `RunRef` 已定义；`SpawnStatus`、`SpawnError` 齐备。                                                 |
| **接口**          | `engine.Spawner`、`engine.LoopPolicy` 仅有接口；`RunState` 已带 `Depth` / `ParentRunID` / `ActiveChildRuns`，但主路径**尚未**在每次 spawn / 结束时完整维护。 |
| **默认值**         | `internal/defaults`：`SpawnMaxDepthDefault`、`MaxConcurrentSpawnsDefault`、`BuiltinSpawnToolName` 已就绪。                                |
| **工具 / Runner** | **无** 已接线的 `spawn_subagent` `Handle`；**无** 子 Run 结果作为 tool result 回注父 loop 的闭环路径。                                                  |
| **事件**          | `event.RuntimeEvent` 尚无 `spawn_`* 类型；与 multi-agent-server 的数据融合命名需在实现时对表。                                                          |
| **异步执行**        | 主路径默认未为子 Run 起独立 goroutine；本版本须按 §4 落地异步，并同步审视取消 / 背压。                                                                             |
| **子内非流式**       | 现 `ChatModel` 通常由 Runner 全局配置流式；子路径需要新增**强制非流式**包装或开关（§5）。                                                                         |
| **v0.7.0 顺延项**  | `skill_ids` 在 spawn 参数中此前为设计顺延，本版本纳入交付。                                                                                            |


---

## 范围取舍（MVP 内 / 可顺延）


| 项                               | v0.8.0 决策                                                                      |
| ------------------------------- | ------------------------------------------------------------------------------ |
| **单层子 Run 闭环**                  | **P0**：根 → 一级子 Run 完整闭环；**MVP `MaxDepth = 2`**，嵌套（深度 > 2）只验「拒绝」分支。                        |
| **异步子 Run**                     | **P0（按 §4 必须）**：子 `RunLoop` 在独立 goroutine 推进；父主路径不得长期同步阻塞。父结束 → 子立即取消（决议）。 |
| **子内非流式 LLM**                   | **P0（按 §6 必须）**：spawn 子路径强制 `stream=false`；`AllowStream` 字段仅保留预留、不开放。                   |
| **可唤醒子 Agent（§3 第二种语义）**        | **接口预留 + 通道铺设**：`SpawnHandle.inbox` / `ChildMailbox.in` 通道实现；v0.8.0 验收**只**覆盖 `LifecycleEphemeral`，`linger` 端到端测试入 v0.9。 |
| **精简短期记忆下传（§1）**                | **P0 接口预留 + 默认关闭**：`SpawnSpec.MemoryDigest` 字段保留；默认 `nil` = 完全隔离；MVP 不实现自动摘要器。              |
| **嵌套 spawn 授权（§5）**             | **P0**：默认禁止；父级显式 `AllowChildSpawn=true` 才允许；**未授权直接不挂 `spawn_subagent` 工具**（决议）。 |
| **子工具集 + ReAct 聚合（§7）**          | **P0**：子工具与父同源；`ToolAllowlist` / `ToolBlocklist` **支持 glob 通配**（决议）；中间步骤不回填父；最终聚合 `ToolInvocations`。 |
| **预算共享（§3.8 硬限制）**              | **P0 决议**：tokens / USD **全树共享**单一 `BudgetCounter`；超限 `Code = "budget_exceeded"`。 |
| **`agent_transfer` 与 spawn 互斥**  | **P0 决议**：父级有活跃子 Agent 时禁止 transfer（system prompt + 工具 Handle 双重保障）。            |
| **Dev UI / 调试 HTTP**            | 不强绑完整树形 UI；事件流 + 日志可验即可；`loopforged` 已有调试端点时增量暴露 spawn 元数据。                    |
| **静态 `pkg/network` 与 spawn 同屏** | **P1**：先保单根 Agent + spawn；交互复杂度高时验收单列为 beta 或 v0.8.1。                          |


---

## 改造计划（v0.8.0）

1. **设计定稿（编码前完成）**：在「待决」表落定结论 ——
  - 子 Run 的取消语义（父 `ctx` 取消是否级联、子等待唤醒态如何被打断）；
  - `skill_id` 不存在的错误策略（拒绝 spawn / 区分错误码）；
  - `tool_allowlist` 与父 MCP 工具前缀的交集算法；
  - 「精简短期记忆下传」字段的最小形态与默认值。
2. **Spawner 实装**：在 `internal/engine/`（如 `spawner_default.go`）实现，依赖 `agent.Agent` 工厂或 Clone、`ChatModel` 与 MCP 绑定、`SkillRegistry`、`LoopPolicy`、子 RunID 发号器；按 §4 起独立协程，按 §5 强制子 ChatModel 非流式。
3. **LoopPolicy 默认实现**：在 `RunState` 上实现或委托 `AllowStep` / `AllowSpawn` / `MaxSteps` / `MaxSpawnDepth`，与 `request.RuntimeRequest` 字段接线；在子 Run 创建 / 结束时**完整**维护 `ActiveChildRuns`、`Depth`、`ParentRunID`。
4. **工具层**：`pkg/tool` 或 `internal/engine/tooling` 注册 builtin `Handle`：解析 JSON → `SpawnSpec` → `Spawner.Spawn` → 序列化 `SpawnResult` 为给模型的字符串（JSON 或文本，定稿一种并写测）。
5. **Runner / Agent 集成**：`pkg/runner` 注入 / 持有 `Spawner` 依赖；保证子 Run 与父的 model / MCP 绑定与 allowlist 策略一致；spawn 工具路径在 `RunLoop` 中走「同表统一调度」或「明确分支」之一并文档化。
6. **生命周期回收（§2）**：父 Run 终态（completed / cancelled / error）时，向所有活跃子 Run 发取消信号并等待回收；等待唤醒中的子 Agent 同样退出。
7. **指标聚合**：子 Run 结束后将 `RunMetrics` merge 到父累计；若启用全局 budget，spawn 前比对子树预估与已消耗（线程安全或单写者模型，见代码计划）。
8. **测试**：
  - **单元** —— policy 拒绝、深度、并发、allowlist 交集、缺失 `skill_id`、ctx 取消、子内非流式开关；
  - **集成（mock 模型即可）** —— 一次 spawn 调用产出预期 tool result 文本；父侧不阻塞期间可继续接收事件；
  - **生命周期** —— 父结束时所有子被回收（含等待唤醒态）。
9. **文档与验收**：`doc/acceptance/v0.8.0-acceptance.md` 与 `multi-agent-engine.md` §3.8 实现状态；触及的包同步更新 `doc/packages/loopForge-*.md`（六段，参见 `rei-doc-mandatory`）。

---

## 代码改造计划（数据结构 / 接口预留）


| 位置                                                  | 计划                                                                                                                           |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `internal/engine/spawn.go` 及新文件                     | 默认 `spawner` 实现；`LoopPolicy` 实现体可拆至 `policy_*.go`。                                                                           |
| `internal/defaults/loop.go` / `pkg/runtime/request` | **决议**：`SpawnMaxDepthDefault` 收敛为 `2`（MVP）；`MaxConcurrentSpawnsDefault` 保持个位数；新增 `BudgetTokensTreeDefault` / `BudgetUSDTreeDefault`（**全树共享**预算）；其余字段完整暴露给 Runner / Engine 构造。 |
| `pkg/runtime/exchange/exchange.go`                  | 增补 `SpawnSpec` 字段（决议后）：`memory_digest`（§1，默认 nil）、`lifecycle`（§3，默认 `ephemeral`）、`allow_child_spawn`（§5，默认 `false`）、`tool_blocklist`（§7，支持 glob）、`allow_stream`（§6，**保留但不在 JSON Schema 中暴露**）。命名以 ADR 与代码评审为准。 |
| `pkg/runtime/event/event.go`                        | 新增 `EventSpawnStart` / `EventSpawnEnd`（命名最终对表 `data-fusion.md`）+ Payload（`child_run_id` / `parent_run_id` / `depth`）。        |
| `pkg/runtime/outcome/outcome.go`                    | 视需要：子树指标聚合字段或文档化 rollup 规则；与 `RuntimeOutcome.Metrics` / `ChildRunIDs` 协同。                                                    |
| `pkg/runner/runner.go` 或子模块                         | 注入 `Spawner` / 引擎句柄；发号子 `RunID`；spawn 工具路径调用 `Spawner.Spawn`；父结束时回收子。                                                        |
| `pkg/model` 或子 ChatModel 包装                         | 提供「强制非流式」适配点（§5），确保子路径独立于根路径流式配置。                                                                                            |
| `internal/engine/tooling.go`                        | builtin 与 MCP / 本地工具合并顺序；`spawn_subagent` 不被错误过滤。                                                                            |
| 观测                                                  | OTel 在 `Spawner` 与子 `RunLoop` 入口起 span（`parent_run_id`、`depth`、`llm_mode`）；`pkg/log` 审计字段。                                   |


### 示意：builtin 与 Spawner 边界（英文标识；最终以 ADR / 代码为准）

```go
// 内置 spawn 工具处理器：解析参数 -> 调 Spawner.Spawn -> 返回工具结果字符串。
// type SpawnToolHandler struct { Spawner engine.Spawner; Policy engine.LoopPolicy; ... }
// func (h *SpawnToolHandler) Handle(ctx context.Context, in tool.Input) (string, error)
```

---

## 数据结构改造细节（含父子消息同步机制）

> 本节给出**逐结构**的改造点与**父子 Agent 消息同步**的机制示意；结构名以现有源码为基线（`pkg/runtime/exchange`、`internal/engine`、`pkg/runtime/event`），新增项以「新增（v0.8.0）」标注；最终命名以代码评审 / ADR 为准。
> **注**：示意代码块中的注释为中文，便于阅读；落到实际 `.go` 源文件时，请遵守 `rei-go.mdc`「代码中禁止出现中文与中文符号」的约束，将注释翻译回英文。

### 1. 结构改造一览


| 结构                     | 现状                                                                                           | v0.8.0 改造                                                                                                                                 | 关联规则         |
| ---------------------- | -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `exchange.RunRef`      | `RunID` / `AgentRole` / `ParentRunID` / `Depth`                                              | 不破坏；额外约束：每次 spawn 必须填 `ParentRunID` 与 `Depth = parent.Depth+1`。                                                                           | §2           |
| `exchange.SpawnSpec`   | `Task` / `SystemAddendum` / `SkillIDs` / `ToolAllowlist` / `LoopOverrides` / `ModelOverride` | 新增：`MemoryDigest`（§1）、`Lifecycle`（§3：`ephemeral` / `linger`）、`AllowChildSpawn`（§5，默认 `false`）、`AllowStream`（§6，默认 `false`）、`ToolBlocklist`（§7，与原 `ToolAllowlist` 配套，**白先黑后**；`spawn_subagent` 不走双名单，由 `AllowChildSpawn` 单独决定）、`MaxIdleAfterFinish`（linger 等待上限） | §1 / §3 / §5 / §6 / §7 |
| `exchange.SpawnResult` | `ChildRunRef` / `Status` / `FinalText` / `Error` / `Metrics`                                 | 新增：`Handle SpawnHandleRef`（仅在 `Lifecycle=linger` 时非空，作为父侧再次唤醒的句柄标识，**不**直接暴露通道指针）；`ToolInvocations []ToolInvocation`（§7：本轮子级 ReAct 期间产生的 function-call 摘要按发生顺序聚合，供父 Agent 向外发送工具调用报文）。 | §3 / §7      |
| `engine.RunState`      | `RunID` / `Step` / `LastError` / `Termination` / `ParentRunID` / `Depth` / `ActiveChildRuns` | 新增：`Children map[string]*SpawnHandle`（父侧持有子句柄）、`Mailbox *ChildMailbox`（子侧持有，用于接收父唤醒）、`AllowSpawn bool`（§5：本 Run 是否被授权再次 spawn，由其父在 `SpawnSpec.AllowChildSpawn` 中决定，根 Run 默认 `true`）；父子状态互不直接共享。 | §2 / §3 / §4 / §5 |
| `engine.Spawner`       | `Spawn(ctx, parent, spec) -> *SpawnResult`                                                   | 改写为**异步**：返回 `*SpawnHandle`，`Result()` 阻塞获取最终结果；builtin tool handler 在内部 `select` 等待。                                                     | §4           |
| `event.RuntimeEvent`   | 已有 `agent_transfer` / `tool_`* 等                                                             | 新增 `EventSpawnStart` / `EventSpawnEnd` / `EventSpawnWake`（payload：`child_run_id` / `parent_run_id` / `depth` / `lifecycle` / `wake_seq`）。 | §3 / 观测      |
| `outcome.RunMetrics`   | `Model` / `*Tokens` / `*CostUSD` / `Steps`                                                   | 新增：`ChildSubtree RunMetricsAggregate`（嵌入聚合；rollup 由 Spawner 在 `Result()` 时回填）。                                                            | 指标聚合         |


### 2. 父子消息同步机制（设计）

**原则**

- **单向所有权**：父侧持有 `SpawnHandle`（拥有写权限），子侧持有 `ChildMailbox`（只读消费）；两侧通过**有界 channel** 通信，**不共享**结构体指针。
- **背压策略**：`Mailbox` 与 `Outbox` 均为有界缓冲（默认 16），满时按「待决」表落定的策略处理（`block` / `drop_oldest` / `reject`），MVP 建议 `reject` 并写日志。
- **取消语义**：父侧 ctx 取消时，关闭 `Mailbox.in`；子侧 `select` 在收到 `ctx.Done()` 或 mailbox 关闭后**立即** 退出（满足 §2、§4）。
- **生命周期开关**：
  - `Lifecycle = ephemeral`：子 Run 自然结束后**关闭** mailbox 并回收 goroutine；
  - `Lifecycle = linger`：子完成单次任务后**保留** goroutine，进入 `WaitWake` 状态；父若不再唤醒，达 `MaxIdleAfterFinish` 后自动回收。
- **再次唤醒**：父侧调用 `SpawnHandle.Wake(ctx, msg)` —— 子侧从 `Mailbox` 取出 `WakeMessage` 后**重启** 一次子 `RunLoop`（沿用同一 `Agent` 实例与 `RunID`，`Step` 继续累加）。

**通道形态（示意）**

```text
+------------------ Parent Run -------------------+
| SpawnHandle (每个子一个):                        |
|   - Ref       RunRef                            |
|   - inbox     chan<- WakeMessage   // 父 -> 子  |
|   - results   <-chan SpawnResult   // 子 -> 父  |
|   - events    <-chan *RuntimeEvent // 子 -> 父  |
|   - cancel    context.CancelFunc                |
+----+--------------------------------+----+------+
     | (Wake 唤醒)                    | (Result/Event 回传)
     v                                |
+----------------- Child Run ---------+-----------+
| ChildMailbox:                                   |
|   - in        <-chan WakeMessage                |
|   - outResult chan<- SpawnResult                |
|   - outEvent  chan<- *RuntimeEvent              |
| 主循环:                                          |
|   for {                                         |
|     select {                                    |
|       case <-ctx.Done(): 退出                   |
|       case msg, ok := <-in:                     |
|          if !ok { 退出 }                        |
|          按 msg 跑一轮子 RunLoop                |
|          if Lifecycle == ephemeral { 退出 }     |
|     }                                           |
|   }                                             |
+-------------------------------------------------+
```

### 3. 示例代码（英文标识；非最终 API）

#### 3.1 SpawnSpec / SpawnResult 扩展

```go
// pkg/runtime/exchange/exchange.go（节选）

// Lifecycle 控制子 Agent 在一次执行结束后是直接终止
// 还是保持存活等待父级再次唤醒（规则 §3）。
type Lifecycle string

const (
    LifecycleEphemeral Lifecycle = "ephemeral"
    LifecycleLinger    Lifecycle = "linger"
)

// MemoryDigest 是父侧经过精简后下传给子级的短期记忆（规则 §1）。
// 为空表示完全隔离；任何情况下都不得直接搬运父级原始 transcript。
type MemoryDigest struct {
    Summary    string            `json:"summary,omitempty"`
    RecentTurns []DigestTurn     `json:"recent_turns,omitempty"`
    Scratch    map[string]string `json:"scratch,omitempty"`
}

type DigestTurn struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type SpawnSpec struct {
    Task           string
    SystemAddendum string
    SkillIDs       []string
    ToolAllowlist  []string // §7：白名单；非空则子级仅可见列表内工具
    ToolBlocklist  []string // 新增（v0.8.0）§7：黑名单；命中即对子级不暴露，与白名单「先白后黑」叠加
    LoopOverrides  LoopOverrides
    ModelOverride  string

    MemoryDigest       *MemoryDigest // 新增（v0.8.0）§1：父侧下传的精简短期记忆
    Lifecycle          Lifecycle     // 新增（v0.8.0）§3：默认 LifecycleEphemeral
    MaxIdleAfterFinish time.Duration // 新增（v0.8.0）§3：仅对 linger 生效；0 表示使用引擎默认
    AllowChildSpawn    bool          // 新增（v0.8.0）§5：默认 false；仅在显式开启时允许该子级再次 spawn 孙子
    AllowStream        bool          // 新增（v0.8.0）§6：**保留字段，v0.8.0 不开放**；引擎实现忽略此字段并强制 stream=false；JSON Schema 中不暴露
}

// ToolInvocation 是子级 ReAct 期间一次 function-call 的结构化摘要（规则 §7）。
// 不携带原始 tool_result 全文，只放摘要 / 大小 / 必要的外链，避免父侧上下文膨胀。
type ToolInvocation struct {
    Name         string            `json:"name"`                     // 工具名（含 MCP 前缀）
    Arguments    json.RawMessage   `json:"arguments,omitempty"`      // 原始参数 JSON（小体积可全留）
    ResultDigest string            `json:"result_digest,omitempty"`  // 结果摘要 / 截断
    StartedAt    time.Time         `json:"started_at"`
    EndedAt      time.Time         `json:"ended_at"`
    Error        string            `json:"error,omitempty"`
    MCPSource    string            `json:"mcp_source,omitempty"`     // 来源 MCP server / 内置
    Attrs        map[string]string `json:"attrs,omitempty"`          // 扩展位
}

// SpawnHandleRef 是父侧再次唤醒 linger 子级时使用的不透明句柄标识。
// 真正的句柄（持有 channel）存放在 engine.RunState.Children 中，不可序列化。
type SpawnHandleRef struct {
    HandleID string
    RunID    string
}

type SpawnResult struct {
    ChildRunRef RunRef
    Status      SpawnStatus
    FinalText   string
    Error       *SpawnError
    Metrics     outcome.RunMetrics

    Handle          SpawnHandleRef   // 新增（v0.8.0）：仅当 Lifecycle == LifecycleLinger 时非零
    ToolInvocations []ToolInvocation // 新增（v0.8.0）§7：本轮子级 ReAct 期间的 function-call 摘要
                                     // 父 Agent 据此向外发送工具调用报文（事件流 / trace / 用户面）
}
```

#### 3.2 父子通道与 SpawnHandle / ChildMailbox

```go
// internal/engine/spawn_handle.go（新增 v0.8.0）

// WakeMessage 是一次「父 -> 子」的指令：要么是新的子任务（触发子级重跑一轮 loop），
// 要么是控制信号（如取消）。
type WakeMessage struct {
    Seq        uint64               // 每个句柄单调递增，用于去重 / 排序
    Task       string               // 为空表示纯控制信号
    Addendum   string               // 本轮可选的追加 system 文本
    Digest     *exchange.MemoryDigest // 可选：刷新本轮短期记忆
    Deadline   time.Time            // 零值表示沿用 handle 的 ctx 截止时间
    Control    WakeControl          // 可选控制操作
}

type WakeControl string

const (
    WakeControlNone   WakeControl = ""
    WakeControlCancel WakeControl = "cancel" // 优雅关闭子级
)

// SpawnHandle 由父侧持有，channel 均为有界（默认 16）。
type SpawnHandle struct {
    Ref       exchange.RunRef
    Lifecycle exchange.Lifecycle
    inbox     chan WakeMessage
    results   chan exchange.SpawnResult
    events    chan *event.RuntimeEvent
    cancel    context.CancelFunc
    closeOnce sync.Once
}

// Wake 向 linger 子级投递一次后续任务。子级已退出时返回 ErrChildClosed；
// 由于 inbox 有界，需通过 ctx 表达背压。
func (h *SpawnHandle) Wake(ctx context.Context, msg WakeMessage) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case h.inbox <- msg:
        return nil
    }
}

// Result 返回下一个完成的 SpawnResult；当子级彻底终止、不会再有结果到达时
// 返回 io.EOF。
func (h *SpawnHandle) Result(ctx context.Context) (exchange.SpawnResult, error) {
    select {
    case <-ctx.Done():
        return exchange.SpawnResult{}, ctx.Err()
    case r, ok := <-h.results:
        if !ok {
            return exchange.SpawnResult{}, io.EOF
        }
        return r, nil
    }
}

// Events 暴露子级发出的事件流（只读），供父侧观测 / 转发到对外事件流。
func (h *SpawnHandle) Events() <-chan *event.RuntimeEvent { return h.events }

// Close 取消子级 ctx 并等待子 loop 排空。
// 父级 Run 进入终态（completed / cancelled / error）时必须调用（规则 §2）。
func (h *SpawnHandle) Close() {
    h.closeOnce.Do(func() {
        h.cancel()
    })
}

// ChildMailbox 是子侧端点，不应暴露给用户代码。
type ChildMailbox struct {
    in        <-chan WakeMessage
    outResult chan<- exchange.SpawnResult
    outEvent  chan<- *event.RuntimeEvent
}
```

#### 3.3 Spawner 异步实装（含生命周期分支）

```go
// internal/engine/spawner_default.go（新增 v0.8.0）

func (s *defaultSpawner) Spawn(
    ctx context.Context,
    parent exchange.RunRef,
    spec *exchange.SpawnSpec,
) (*SpawnHandle, error) {
    // §5：嵌套 spawn 默认禁止；只有当父级在自己被拉起的 SpawnSpec 中
    // 显式 AllowChildSpawn=true，引擎才允许该子级再次发起 spawn。
    // 此处通过 parentState 读取「父级当初是否被授权 spawn」。
    parentState := s.parentState(parent)
    if parentState.Depth > 0 && !parentState.AllowSpawn {
        return nil, &exchange.SpawnError{Code: "child_spawn_forbidden", Message: "parent did not grant AllowChildSpawn"}
    }
    if !s.policy.AllowSpawn(parentState, parent.Depth+1) {
        return nil, &exchange.SpawnError{Code: "depth_or_concurrency", Message: "policy rejected"}
    }
    childCtx, cancel := context.WithCancel(ctx)
    ref := s.allocChildRef(parent, spec)

    h := &SpawnHandle{
        Ref:       ref,
        Lifecycle: defaultLifecycle(spec),
        inbox:     make(chan WakeMessage, 16),
        results:   make(chan exchange.SpawnResult, 4),
        events:    make(chan *event.RuntimeEvent, 64),
        cancel:    cancel,
    }
    mailbox := &ChildMailbox{in: h.inbox, outResult: h.results, outEvent: h.events}

    s.parentRegister(parent, h) // §2：登记到父侧，便于父终态时强制回收

    go s.runChild(childCtx, ref, spec, mailbox) // §4：子级在独立 goroutine 中执行

    return h, nil
}

func (s *defaultSpawner) runChild(
    ctx context.Context,
    ref exchange.RunRef,
    spec *exchange.SpawnSpec,
    mb *ChildMailbox,
) {
    defer close(mb.outResult)
    defer close(mb.outEvent)

    agent := s.buildChildAgent(spec) // §1：只注入 digest + addendum，不复制父 transcript
    // §7：子工具集与父同源；按 SpawnSpec 的白/黑名单收敛子级可见工具（白先黑后）
    // 未命中白名单或命中黑名单的工具不进入子 ToolRegistry；spawn_subagent 不走双名单。
    s.applyChildToolFilter(agent, spec.ToolAllowlist, spec.ToolBlocklist)
    // §5：未授权时不为子级注册 spawn_subagent 工具，避免「看得见调不通」的幻觉
    s.applyChildSpawnPermission(agent, spec.AllowChildSpawn)
    // §6 决议：v0.8.0 始终强制非流式；spec.AllowStream 仅保留为后续预留字段，引擎不读取
    chat := s.wrapNonStream(agent.ChatModel())

    // §7：拦截子级 ReAct 期间的 function-call / tool_result，按发生顺序聚合为 ToolInvocations，
    // 同时**不**把中间事件透传到父侧 mb.outEvent；最终在 SpawnResult 中一次性回填。
    recorder := newToolInvocationRecorder()
    agent.ToolDispatcher().Use(recorder.Middleware())

    initial := WakeMessage{Seq: 0, Task: spec.Task, Addendum: spec.SystemAddendum, Digest: spec.MemoryDigest}
    if err := s.runOneTurn(ctx, ref, agent, chat, initial, mb, recorder); err != nil {
        return
    }

    if spec.Lifecycle != exchange.LifecycleLinger {
        return
    }

    idle := time.NewTimer(idleOrDefault(spec.MaxIdleAfterFinish))
    defer idle.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-idle.C:
            return
        case msg, ok := <-mb.in:
            if !ok {
                return
            }
            if msg.Control == WakeControlCancel {
                return
            }
            recorder.Reset() // §7：每次唤醒以本轮为单位聚合 ToolInvocations
            if err := s.runOneTurn(ctx, ref, agent, chat, msg, mb, recorder); err != nil {
                return
            }
            resetTimer(idle, idleOrDefault(spec.MaxIdleAfterFinish))
        }
    }
}
```

#### 3.4 Builtin 工具：把异步 handle 折叠成 tool result（默认 `ephemeral`）+ ReAct 聚合外发（§7）

```go
// internal/engine/spawn_tool.go（新增 v0.8.0）

func (h *SpawnToolHandler) Handle(ctx context.Context, in tool.Input) (string, error) {
    spec, err := parseSpawnArgs(in.Arguments) // 包含 tool_white_list / tool_block_list / allow_child_spawn 等
    if err != nil {
        return "", err
    }
    handle, err := h.Spawner.Spawn(ctx, in.ParentRef, spec)
    if err != nil {
        return marshalRejected(err), nil
    }
    // 父 goroutine 在该工具调用上等待最终结果；其它父级工具调用 / 事件流仍可继续推进，
    // 因为子级跑在独立 goroutine（§4）。子 ReAct 中间步骤（function-call / tool_result）
    // 不流回父侧（§7），父侧只在此处拿到一次最终结果。
    res, err := handle.Result(ctx)
    if err != nil {
        handle.Close()
        return marshalErr(err), nil
    }

    // §7：把子级聚合的 ToolInvocations 通过父侧事件桥向外发送，
    // 由父 Agent 决定是否对外暴露（脱敏、截断、按 MCP 源过滤等）。
    h.ParentEventBridge.EmitChildToolInvocations(ctx, in.ParentRef, res.ChildRunRef, res.ToolInvocations)

    if spec.Lifecycle == exchange.LifecycleEphemeral {
        handle.Close()
    }
    // 返回给父级模型的 tool result 仍以 FinalText / 摘要为主；ToolInvocations
    // 不直接塞进 tool result 字符串，避免污染父级上下文与扣 token（§7）。
    return marshalResult(res), nil
}
```

#### 3.4.1 子工具过滤与 ReAct 聚合（§7）

```go
// internal/engine/child_tool_filter.go（新增 v0.8.0）

// applyChildToolFilter 在子级 ToolRegistry 上做「白先黑后」过滤：
//   1. 若 allow 非空，仅保留 name ∈ allow 的工具；allow 为空表示不主动收敛。
//   2. 不论 allow 如何，name ∈ block 的工具一律剔除。
//   3. spawn_subagent 不走双名单，由 §5 的 AllowChildSpawn 单独决定。
func (s *defaultSpawner) applyChildToolFilter(agent *Agent, allow, block []string) {
    if len(allow) == 0 && len(block) == 0 {
        return
    }
    allowSet := toSet(allow)
    blockSet := toSet(block)
    agent.ToolRegistry().Filter(func(t tool.Tool) bool {
        if t.Name() == s.spawnToolName { // 由 §5 路径处理，不在此剔除
            return true
        }
        if len(allowSet) > 0 && !allowSet[t.Name()] {
            return false
        }
        if blockSet[t.Name()] {
            return false
        }
        return true
    })
}

// internal/engine/tool_invocation_recorder.go（新增 v0.8.0）

// toolInvocationRecorder 是子级 ToolDispatcher 的中间件：
// 在每次 function-call 前后打点，结构化聚合为 []ToolInvocation。
// 同时拦截向 mb.outEvent 透传中间 tool 事件 —— 仅最终 SpawnResult 出现 ToolInvocations。
type toolInvocationRecorder struct {
    mu    sync.Mutex
    items []exchange.ToolInvocation
}

func newToolInvocationRecorder() *toolInvocationRecorder { return &toolInvocationRecorder{} }

func (r *toolInvocationRecorder) Reset() {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.items = r.items[:0]
}

func (r *toolInvocationRecorder) Snapshot() []exchange.ToolInvocation {
    r.mu.Lock()
    defer r.mu.Unlock()
    out := make([]exchange.ToolInvocation, len(r.items))
    copy(out, r.items)
    return out
}

func (r *toolInvocationRecorder) Middleware() tool.Middleware {
    return func(next tool.Handler) tool.Handler {
        return tool.HandlerFunc(func(ctx context.Context, in tool.Input) (string, error) {
            started := time.Now()
            out, err := next.Handle(ctx, in)
            ended := time.Now()

            r.mu.Lock()
            r.items = append(r.items, exchange.ToolInvocation{
                Name:         in.Name,
                Arguments:    in.RawArguments,
                ResultDigest: digestForParent(out),
                StartedAt:    started,
                EndedAt:      ended,
                Error:        errString(err),
                MCPSource:    in.MCPSource,
            })
            r.mu.Unlock()

            return out, err
        })
    }
}

// runOneTurn 在子级 RunLoop 跑完一轮后，从 recorder 取出聚合，连同 FinalText 等
// 写入一次 SpawnResult，发送到 mb.outResult；中间事件不会被透传到 mb.outEvent。
func (s *defaultSpawner) runOneTurn(
    ctx context.Context,
    ref exchange.RunRef,
    agent *Agent,
    chat model.ChatModel,
    msg WakeMessage,
    mb *ChildMailbox,
    rec *toolInvocationRecorder,
) error {
    final, metrics, err := agent.RunOnce(ctx, chat, msg.Task, msg.Addendum, msg.Digest)
    res := exchange.SpawnResult{
        ChildRunRef:     ref,
        Status:          statusOf(err),
        FinalText:       final,
        Error:           toSpawnError(err),
        Metrics:         metrics,
        ToolInvocations: rec.Snapshot(), // §7：本轮聚合
    }
    select {
    case <-ctx.Done():
        return ctx.Err()
    case mb.outResult <- res:
        return err
    }
}
```

#### 3.5 父 Run 终态强制回收（§2）

```go
// internal/engine/runstate_lifecycle.go（新增 v0.8.0）

func (r *RunState) Terminate(reason outcome.TerminationReason) {
    r.Termination = reason
    for _, h := range r.Children {
        h.Close() // 取消子级 ctx -> 子 select 返回 -> goroutine 退出
    }
    r.Children = nil
    r.ActiveChildRuns = 0
}
```

### 4. 与现有事件/指标的接入

- **事件**：子侧 `outEvent` 由 Runner 在父流中**重打 `RunID = parent.RunID`** 或 **保留 `child_run_id` 字段** 二选一（建议后者，便于客户端区分）；新增 `EventSpawnStart` / `EventSpawnEnd` / `EventSpawnWake`。  
- **指标**：`SpawnResult.Metrics` 在 `handle.Result()` 返回前由 Spawner 填充，Runner 把它累加进父 `RunMetrics.ChildSubtree`，最终在 `RuntimeOutcome` 中可见（与现有 `ChildRunIDs` 字段协同）。

---

## 交付清单

- 至少一个可跑通的 E2E 或集成样例（`cmd/` 或 `examples`）：用户消息触发模型调用 `spawn_subagent`（mock 工具调用亦可作为 CI 补充）→ 子 Run 在独立协程中执行 → 父侧 tool result 可见，且父在子执行期间未被阻塞。
- 硬限制的可观测拒绝（`SpawnRejected` + 结构化错误，**非** 200 式静默）。
- `SkillIDs` 在 spawn 上的注入与 v0.7.0 `SkillRegistry` 行为一致。
- 嵌套 spawn 授权链可观测：未授权子级调用 `spawn_subagent` 时返回 `child_spawn_forbidden`，并在 trace 中可见每级 `allow_child_spawn` 属性（§5）。
- 嵌套 spawn 授权链可观测（§5）：未授权子级调用 `spawn_subagent` 返回 `child_spawn_forbidden`；trace 中每级带 `allow_child_spawn` 属性；默认未授权时 `spawn_subagent` 不在子级 `ToolRegistry` 中。
- 子工具集 + ReAct 聚合（§7）：子 `ToolRegistry` **与父同源**且按 `tool_white_list` + `tool_block_list` 收敛；子 ReAct **中间** function-call / tool_result **不**进入父侧事件流；最终 `SpawnResult.ToolInvocations` 中按发生顺序聚合；父 Agent 通过 `EmitChildToolInvocations` 决定是否对外发送工具调用报文。
- 子内 LLM 在 trace / 日志中可见为非流式（§6）。
- 父 Run 终态时所有子（含等待唤醒态）被回收（§2），可由集成测断言。
- 子 `RunMetrics` 已 rollup 到父；OTel 父子关系（若实现）可见。
- `spawn_`* 事件可发出并被测试或 demo 消费（最小验收）。
- 文档：`doc/acceptance/v0.8.0-acceptance.md` + `multi-agent-engine.md` §3.8 实现状态指针 + 触及的 `doc/packages/*` 更新。

---

## 风险与决议

> **2026-04-24 评审定稿**：MVP 「能跑」优先；不影响接口的边角细节（如脱敏策略、命名对表）保留为「遗留待决」放在表后。

| 项 | 决议（v0.8.0 MVP） | 落点 |
| --- | --- | --- |
| **父结束 → 子立即停止（§2）** | 父级 Run 进入终态（完成 / 取消 / 错误）时，**所有** 子 Agent **立即** 取消，**不** 等待子 loop 当前回合跑完；子级 ctx 即时 done，MCP 连接 / step 计数交由 `RunState.Terminate` 统一回收。父级中途取消 → 子级所有 in-flight `function_call` 视为失败，进入 recorder 的 `Error` 字段，但**不** 再产出 `SpawnResult`（因为已无意义）。 | `RunState.Terminate`、`SpawnHandle.Close()`；§2 测试加「父中途取消子立即退出」用例 |
| **MVP 层级上限** | **`MaxDepth = 2`**（即 root + 一层子 Agent）；超过深度返回 `SpawnError{Code: "depth_or_concurrency"}`；`MaxConcurrentSpawns` 默认值另定，但不超过个位数。`AllowChildSpawn` 字段**保留**在 `SpawnSpec` 中，但 v0.8.0 实测路径不依赖嵌套层级 > 2，相关测试只验「拒绝」分支。 | `internal/defaults/loop.go`；阶段 B 任务 3 的写测断言更新为「深度=2 通过 / 深度=3 拒绝」 |
| **等待唤醒能力（§3）** | **保留**通道实现：`SpawnHandle.inbox` + `ChildMailbox.in` 的 `select` 通道铺好，`Lifecycle = linger` 字段保留；但 **v0.8.0 验收只覆盖 `LifecycleEphemeral`**——子级单次跑完即结束。`linger` 路径不写端到端测试，仅保留单元级编译通过 + `SpawnHandleRef` 字段占位。 | `SpawnSpec.Lifecycle` 默认 `ephemeral`；`linger` 入 v0.9 验收清单 |
| **精简短期记忆（§1）默认** | **默认关闭**：`SpawnSpec.MemoryDigest == nil` 表示完全隔离，不下传任何父侧历史；显式字段保留以便后续扩展。MVP 不实现「自动摘要器」。 | `SpawnSpec.MemoryDigest` 默认 nil；文档移除「最大长度 / 拼接顺序」的实现承诺 |
| **预算（§3.8 硬限制）** | **Budget 全树共享**：根 Run 持有一份 `BudgetCounter`，所有子树（spawn 一层）共享同一计数器（tokens + USD）；任何一层超额即由 `LoopPolicy` 拒绝下一次 `AllowStep` / `AllowSpawn`，并在 `SpawnError` 中标识 `Code = "budget_exceeded"`。**不** 做「每子树独立配额」。 | `LoopPolicy` / `RunState.BudgetCounter`；阶段 A 任务说明加这一点 |
| **嵌套 spawn 未授权（§5）** | **未授权直接不挂工具**：构造子级 `ToolRegistry` 时，若 `SpawnSpec.AllowChildSpawn == false` 则**不**注册 `spawn_subagent`；模型从 tool list 上根本看不到，避免幻觉。Handle 内**额外**保留 `child_spawn_forbidden` 短路作为防御性兜底。 | `applyChildSpawnPermission` 实现 + 单测「子级 ToolRegistry 不含 spawn_subagent」 |
| **`ToolBlocklist` 通配匹配（§7）** | **支持 glob 风格匹配**：`tool_block_list` / `tool_white_list` 条目按 `glob.Match` 匹配（`*` / `?`），用于按 MCP 前缀整段屏蔽（如 `mcp:foo/*`）；防止逐条枚举导致 `SpawnSpec` 与父侧 `system_prompt` 同步爆炸。同时对子级 `ToolRegistry` 做**注册前过滤**，子级模型上下文中只看到过滤后的工具描述。 | `applyChildToolFilter` 用 `path/filepath.Match` 或 `glob` 库；写测覆盖前缀通配 |
| **`allow_stream` 字段（§6）** | **字段保留但不开放**：`SpawnSpec.AllowStream` 在结构体中保留，类型 `bool`，**默认 `false`**；引擎实现**忽略**该字段（始终 `stream=false`）；`spawn_subagent` 工具的 JSON Schema **不**暴露该字段；任何要求开启的需求由维护方面对面评审，**默认拒绝**（除非提供「必须流式」的硬性理由）。 | `wrapNonStream` 不读 `AllowStream`；JSON Schema 中删除该字段；`progress` / `plan` 标注「v0.8.0 仅预留」 |
| **有活跃子 Agent 时禁止 transfer** | 父 Agent **存在任何活跃子 Agent**（`RunState.ActiveChildRuns > 0`）时，**禁止 `agent_transfer`**：在父 Agent 的 system prompt 中**显式写入** 该约束，并在 `agent_transfer` 工具 Handle 内做**硬校验**——命中时直接返回工具错误（`Code = "transfer_blocked_active_children"`），不进入 transfer 流程。等子 Agent 全部回收后方可 transfer。 | 阶段 C 新增任务：root agent prompt 模板补充该规则；`agent_transfer` handler 增加守卫 + 单测 |

### 遗留待决（不阻塞 v0.8.0 验收）

| 项 | 说明 |
| --- | --- |
| `ToolInvocations` 的 `Arguments` 默认脱敏 | token / api_key 等字段是否在落库前自动打码；MVP 由父 Agent 自行选择是否外发，引擎不主动脱敏。 |
| `EmitChildToolInvocations` 的承载通道 | 走 `RuntimeEvent` 还是仅入 OTel trace；与 `data-fusion.md` / 历史 `prd-v0.2.0-streaming` 命名表对齐时再定（不影响 v0.8.0 接口形态）。 |
| `ToolInvocations` 上限 | 默认条数 / 单条 `ResultDigest` 长度的硬上限；MVP 给出宽松默认（如 100 条 / 单条 4KB），后续按真实场景收紧。 |
| 事件命名对表 | `spawn_start` / `spawn_end` 与 `data-fusion.md`、历史 `prd-v0.2.0-streaming` 命名差异的统一，待该文档下次 review 时合并。 |


---

## Slice1 已落地（`pkg/agent` 最小实现）

- **落点**：`pkg/agent` 与 `pkg/runner`（`internal/engine` 的 `Spawner` / `RunState` 仍为接口占位，**未**在本切片接入实装）。  
- **数据**：`pkg/runtime/exchange` 中 `SpawnSpec` 已带 `MemoryDigest`、`Lifecycle`（MVP 仅 `ephemeral` 行为可验证；`linger` 在 `Spawn` 中返回 `SpawnRejected`）、`AllowChildSpawn`（默认 `false` 时在子 Agent 的 `ChildAgentBuilder` 中关闭嵌套能力）。`internal/defaults/loop.go` 中 `SpawnMaxDepthDefault = 2`。  
- **运行时**：`defaultSpawner`（`pkg/agent/spawn.go`）在**独立协程**中跑子 `RunLoop`；子事件写入内部 channel 并由同一条消费路径收齐，**不**进父的 `ch`。父侧通过 `spawn_subagent` 的 `Handle` 同步获得 JSON 化的 `SpawnResult`（`pkg/agent/spawn_tool.go`）。  
- **入口**：`WithSpawn`（`pkg/agent/options.go`）配置 `ChildAgentBuilder`；`buildSpawnSubagentVarTool` 在深度、开关不满足时**不**注册 `spawn_subagent`（不「看得见调不通」）。`pkg/runner/Runner` 为每轮 `RunLoop` 设置 `LoopState.CurrentRunRef`（`Depth: 0` 根）。  
- **测试**：`pkg/agent/spawn_test.go` 覆盖 `Spawner` 拒深度、`BuildSpawn` 门闸、全路径父侧**不出现**子级多一轮 `CallLLMStart` 的泄漏。  
- **与完整设计/决议的差距**：**未** 实现非流式子 LLM、双名单 glob、ToolInvocations 聚合、全树预算、`agent_transfer` 互斥、新 `RuntimeEvent` 类型、linger/mailbox；见下节 TODO。

## 后续迭代 TODO（相对 Slice1）

1. 子 `ChatModel` **非流式**包装（`wrapNonStream`），与 `spawn-runtime-rules` §6 对齐。  
2. `ToolAllowlist` / `ToolBlocklist`（**glob**）+ `applyChildToolFilter`；`SpawnResult.ToolInvocations` + 父侧外发。  
3. 预算 **全树共享**、父取消 **级联**子、并发子数 `MaxConcurrentSpawns`。  
4. **linger** + `SpawnHandle` + `ChildMailbox` / 唤醒。  
5. 新事件 `spawn_start` / `spawn_end`（与 `data-fusion.md` 对表后）。  
6. **`agent_transfer` 与 spawn 互斥**（transfer prompt + 条件拦截 + 有活跃子级时拒 transfer）。  
7. `internal/engine` **实装** `Spawner` / `LoopPolicy` / `RunState` 与 Runner 的衔接（或保留 `pkg/agent` 实现、由薄适配层转调）。

---

## 变更日志


| 日期         | 说明                                                                                                                                                                                                                                            |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-04-24 | 初稿：v0.8.0 版本计划（spawn 主线；衔接 v0.7.0 顺延的 `skill_ids` 与现有 `exchange` / `defaults` / `RunState`）。                                                                                                                                                  |
| 2026-04-24 | 新增配套实施计划 `doc/plan/plan-v0.8.0.md`；Cursor 规则 `rei-loopforge.mdc` / `rei-doc-mandatory` / `rei-gated-workflow` 已补充 `doc/plan/` 约定。                                                                                                             |
| 2026-04-24 | 新增 `doc/design/spawn-runtime-rules.md`：四条 loopForge 内 spawn 逻辑规则（不入 `.cursor/rules`）；`multi-agent-engine` §3.8 已摘要引用。                                                                                                                         |
| 2026-04-24 | `spawn-runtime-rules` §5：spawn 子 LLM 调用强制非流式；本文目标与 `multi-agent-engine` 摘要对齐。                                                                                                                                                                 |
| 2026-04-24 | 整体优化：修正与 §4 冲突的「同步子 Run」表述，统一目标顺序与「现状」「取舍」「风险」表，新增「运行时逻辑规则（必读）」首章作为单一指针。                                                                                                                                                                      |
| 2026-04-24 | 新增「数据结构改造细节（含父子消息同步机制）」：列出 `RunRef` / `SpawnSpec` / `SpawnResult` / `RunState` / `Spawner` / `RuntimeEvent` / `RunMetrics` 的改造点；给出 `SpawnHandle` + `ChildMailbox` 的有界 channel 同步方案与 Go 示意代码（`Lifecycle` / `WakeMessage` / 异步 Spawner / 终态回收）。 |
| 2026-04-24 | 文档内 Go 示例代码注释统一改为中文，便于阅读；同时在该节首部注明：落到实际 `.go` 源文件时仍须遵守 `rei-go.mdc`「代码中禁止中文」的约束。                                                                                                                                                              |
| 2026-04-24 | `spawn-runtime-rules` 新增 **§5 嵌套 spawn 默认禁止 + 父级显式授权**（原 §5 非流式顺延为 §6）：`SpawnSpec` 新增 `AllowChildSpawn`（默认 `false`）；`RunState` 新增 `AllowSpawn`；`Spawner.Spawn` 入口加 `child_spawn_forbidden` 短路；本文目标重新编号为 12 项；`multi-agent-engine` §3.8 摘要同步。 |
| 2026-04-24 | `spawn-runtime-rules` 新增 **§7 子 Agent 工具集与 ReAct 聚合**：子 `ToolRegistry` **与父同源**；`SpawnSpec` 新增 `ToolBlocklist`（与 `ToolAllowlist` **白先黑后** 配套）；子 ReAct 中间 function-call / tool_result **不**回填父级事件流；新增 `SpawnResult.ToolInvocations` 与 `exchange.ToolInvocation` 结构；`SpawnToolHandler` 通过 `EmitChildToolInvocations` 让父 Agent 向外发送工具调用报文；本文目标扩至 13 项；新增 §3.4.1 子工具过滤 + ReAct 聚合 Go 示意；`multi-agent-engine` §3.8 摘要同步。 |
| 2026-04-24 | **风险与待决评审定稿**：（1）父结束子立即停止；（2）MVP `MaxDepth = 2`；（3）唤醒能力保留通道、验收只覆盖 `ephemeral`；（4）`MemoryDigest` 默认关闭；（5）预算**全树共享**；（6）未授权直接不挂 `spawn_subagent` 工具；（7）`ToolBlocklist` / `ToolAllowlist` **支持 glob 通配**；（8）`AllowStream` 字段保留但**不开放、不进 JSON Schema**；（9）父级有活跃子 Agent 时禁止 `agent_transfer`（system prompt + 工具 Handle 双重保障）。本文「风险与待决」改写为「风险与决议」并附「遗留待决」子表；目标扩至 16 项；范围取舍表新增 4 行（嵌套授权 / 子工具集 / 预算共享 / transfer 互斥）；`SpawnSpec.AllowStream` 注释与 `runChild` 示例同步标注；`internal/defaults/loop.go` 影响项补充全树预算与 `MaxDepth=2` 决议。 |
| 2026-04-24 | **代码 Slice1 落地**（上节「Slice1 已落地」+「后续迭代 TODO」）：见 `pkg/agent/spawn.go`、`pkg/agent/spawn_tool.go`、`pkg/runtime/exchange` 与 `doc/plan` 变更记录。 |


---

## 参考文档

- `doc/plan/plan-v0.8.0.md` —— 实施计划（Step 2 任务分阶段、文件清单；与本文配套）
- `doc/design/spawn-runtime-rules.md` —— spawn 运行时五条逻辑规则（**单一权威**）
- `doc/design/multi-agent-engine.md` §3.8 —— 动态子 Agent 生成（spawn）能力总述
- `doc/decision/sdk-selection.md` —— spawn 与 loopForge 职责边界
- `doc/log/progress-v0.7.0.md` —— Skills 与 spawn 参数关系
- `doc/design/data-fusion.md` —— 事件与 `agent_transfer` 扩展口
- `doc/design/architecture.md` —— 能力分层总览


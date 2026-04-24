# loopForge 实施计划 — v0.8.0（spawn）

> **对应版本进度**：`doc/log/progress-v0.8.0.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的**任务拆分与执行顺序**；**先于编码**成文；与 `progress` 同步维护「未开始 / 进行中 / 已完成」状态（不重复粘贴全文，以 **progress 为版本真相源**）。  
> **主目标**：在运行中支持 **动态子 Agent（spawn）**，受控 **内置工具**（默认 `spawn_subagent`）创建子 `Run` 并回注结果。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.8.0.md` | 版本目标、范围、风险、交付清单、与设计的交叉引用 |
| `doc/design/multi-agent-engine.md` §3.8 | spawn 产品语义与硬限制 |
| `doc/decision/sdk-selection.md` | spawn 在引擎中的职责边界 |
| `doc/design/data-fusion.md` | 事件与扩展字段（含 spawn 与 `agent_transfer`） |
| `doc/design/spawn-runtime-rules.md` | **引擎内** 父子隔离（§1）、**父终态** 时子**全部退出**（§2）、**结束后是否可再唤醒** 由**父 spawn 参数** 决定（§3）、子**在独立 goroutine** 且**不** **阻塞**父**主路径**（§4）、**嵌套 spawn** 默认禁止、由**父** 在 `SpawnSpec.AllowChildSpawn` 显式授权（§5）、**子** 内 **LLM** **须** **非流式**（§6）、**子工具集与父同源** + 父级 `tool_white_list` / `tool_block_list` 双名单（**白先黑后**）+ 子 ReAct **中间** 步骤**不** 回填父、**最终** `SpawnResult.ToolInvocations` 聚合供父外发（§7）；**不** 是 Cursor 规则 |

---

## 2. 先决与确认（编码前完成）

- [x] **风险与待决评审定稿**（见 `progress` 「风险与决议」表）：
    - 父 Run 终态 → 子**立即**取消；`MaxDepth = 2`；`Lifecycle = ephemeral` 仅作 v0.8.0 验收；`MemoryDigest` 默认关闭；预算**全树共享**；嵌套未授权**不挂工具**；`ToolBlocklist` / `ToolAllowlist` **支持 glob 通配**；`AllowStream` 字段保留但**不开放、不进 JSON Schema**；父级有活跃子 Agent 时**禁止 `agent_transfer`**。
- [ ] 遗留待决（不阻塞验收）：`ToolInvocations` 上限 / 默认脱敏策略 / `EmitChildToolInvocations` 承载通道 / 事件命名对表，详见 `progress` 「遗留待决」子表。
- [ ] `progress` 中「本版本目标」与本文「任务分阶段」**无**未解决冲突。

---

## 3. 任务分阶段（建议顺序）

### 阶段 A — 策略与状态

1. 实现或衔接 **`LoopPolicy` 默认实现**：与 `RunState` / `request.RuntimeRequest` 的 `SpawnMaxDepth` 等字段**接线**；`AllowSpawn` / `AllowStep` 行为可单测。**决议**：`SpawnMaxDepthDefault` 收敛为 **`2`**（root + 一层子 Agent）；超深度返回 `SpawnError{Code: "depth_or_concurrency"}`。
2. 在 spawn 子路径**维护** `RunState.ActiveChildRuns`、`Depth`、`ParentRunID`（**创建子 Run 时 + 子 Run 结束时**），与 **MaxConcurrentSpawns** 一致。
3. **预算全树共享**（决议）：根 Run 持有 `BudgetCounter`（tokens + USD），子级共享同一计数器；`AllowStep` / `AllowSpawn` 在每步前后**累加 / 检查**；超额返回 `SpawnError{Code: "budget_exceeded"}`。**不**实现「每子树独立配额」。
4. **父结束 → 子立即取消**（§2 决议）：实现 `RunState.Terminate(reason)`：父 Run 进入终态时**立即** 调用所有 `SpawnHandle.Close()`，**不**等待子 loop 当前回合跑完；写测覆盖「父中途取消子立即退出」并验证 in-flight `function_call` 进入 recorder 的 `Error` 字段。

### 阶段 B — Spawner 与内置工具

1. 在 `internal/engine/` 实现 **`Spawner.Spawn`**：校验 policy → 子 `RunID` → 子 `Agent`（`Clone`、system 拼接、`SpawnSpec`）→ **`SkillIDs` → `SkillRegistry` 解析与注入** → 子**完整**子 **`RunLoop` 在独立** `goroutine`（或**等价异步**）中**推进**，**父** **主路径** 仅**有界**等待/信令，**禁止** 长期同步阻塞，见 **`doc/design/spawn-runtime-rules.md` §4**；结果**回填** 父**工具结果** 通道，组装 **`exchange.SpawnResult`**。
2. 注册 **builtin** `spawn_subagent`（名可经 `defaults` 覆写）：**JSON Schema** 与 `SpawnSpec` 字段对齐（含 `allow_child_spawn`，默认 `false`）；**Handle** 内 `Parse → Spawn →` **tool result 串**（格式在阶段内定稿一种并写测例断言）。**决议**：`allow_stream` 字段**不**进入 JSON Schema（v0.8.0 仅作 Go 结构体预留）；`memory_digest` 进入 Schema 但默认 `null`，文档明确「MVP 不实现自动摘要器」。
3. **嵌套 spawn 授权**（**§5**）：`Spawner.Spawn` 入口须先校验**父级 `RunState.AllowSpawn`**，未授权时返回 `SpawnError{Code: "child_spawn_forbidden"}` 并不进入子 `RunLoop`；子级 `Agent` 构造时按 `SpawnSpec.AllowChildSpawn` 决定**是否注册** `spawn_subagent` 到子 `ToolRegistry`（**未授权不挂**）；写测：父未授权 → 子无 `spawn_subagent` 工具；父授权 → 子可调用并受 `MaxDepth` 约束。
4. **子工具集 + ReAct 聚合**（**§7**）：子 `ToolRegistry` **复用** 父同源工具表（含 MCP / 内置）；`SpawnSpec` 新增 `ToolBlocklist`，`applyChildToolFilter` 按「**白先黑后**」收敛（`spawn_subagent` 不走双名单）；**决议**：`ToolAllowlist` / `ToolBlocklist` 条目**支持 glob 通配**（`*` / `?`，例如 `mcp:foo/*`），用 `path/filepath.Match` 或等价库匹配；防止逐条枚举导致 `system_prompt` 同步爆炸。子级 `ToolDispatcher` 挂 `toolInvocationRecorder` 中间件，按发生顺序聚合为 `[]ToolInvocation`；`runOneTurn` 在子 RunLoop 跑完一轮后写入 `SpawnResult.ToolInvocations`；**禁止** 把中间 function-call / tool_result 透传到 `mb.outEvent`；`SpawnToolHandler.Handle` 在拿到结果后调 `ParentEventBridge.EmitChildToolInvocations` 让父 Agent 决定外发；写测：子用了 N 次工具 → `SpawnResult.ToolInvocations` 长度为 N 且顺序一致；中间事件**不**出现在父侧事件流；通配匹配用例覆盖 `mcp:foo/*` 整段屏蔽。

### 阶段 C — Runner 与 tool 面

1. `pkg/runner` 或等价位：**发号**子 `RunID`、**注入/持有** `Spawner` 依赖；保证 **子 Run 与父** 的 **model / MCP 绑定** 与 **allowlist** 策略**一致**于设计；**子** 路径 **LLM** **须** **非流式**（`spawn-runtime-rules` **§6**，子 **ChatModel** 或等效**包装** 上**强制**）。**决议**：`wrapNonStream` **不**读取 `SpawnSpec.AllowStream`（始终 `stream=false`）；该字段保留为后续预留。
2. `internal/engine/tooling`（或 `pkg/tool`）：**builtin 与 MCP/本地工具** 合并时 **不** 误删 **spawn**；`RunLoop` 工具分发路径**统一**处理 spawn（或**明确**分支，二选一**文档化**）。
3. **`agent_transfer` 与 spawn 互斥**（决议）：在 root agent 的 system prompt 模板中**显式写入**「父 Agent 存在活跃子 Agent 时禁止 transfer」；`agent_transfer` 工具 Handle 在执行前**硬校验** `RunState.ActiveChildRuns == 0`，命中即返回工具错误（`Code = "transfer_blocked_active_children"`）并不进入 transfer 流程；写测：`spawn → 立即 transfer` 被拒绝；`spawn → 等待 SpawnResult 回收 → transfer` 通过。

### 阶段 D — 观测、指标、事件

1. 子 `Run` **结束** 后将 **RunMetrics** **rollup** 到父（或**文档化**的**树形**汇总结构）；与 `RuntimeOutcome`、`ChildRunIDs` 等字段**协同**。
2. **slog** 审计字段；**OTel** 父子 **span**（`parent_run_id`, `depth`）。
3. `pkg/runtime/event`：新增 **spawn 起止** 的 **Type + Payload**（与 `data-fusion.md` 对名或**列一份重命名表**在 PR/文档）。

### 阶段 E — 测试与样例

1. **单测**：policy 拒绝、深度（**`MaxDepth=2` 通过 / 深度=3 拒绝**）、并发、allowlist / blocklist **glob 匹配**、缺失 `skill_id`、**父 ctx 取消时子立即退出**、**全树预算超额**、**未授权时子 ToolRegistry 不含 `spawn_subagent`**、**`AllowStream=true` 也始终非流式**、**有活跃子 Agent 时 `agent_transfer` 拒绝**。
2. **集成**（**mock 模型** 可）：**一次 spawn** → **tool result** 含**子输出** 的**稳定**片段；中间 function-call **不**进入父侧事件流；最终 `SpawnResult.ToolInvocations` 顺序一致。
3. 可选：`cmd/` 或 `examples` 最小**跑通**路径（与 `progress` 交付一致）。

### 阶段 F — 文档与验收

1. 新增 `doc/acceptance/v0.8.0-acceptance.md`；更新 `doc/design/multi-agent-engine.md` §3.8 **实现状态**。
2. 变更涉及的 **`doc/packages/loopForge-*.md`** 六段更新；相关 **`README` 链** 更新。

---

## 4. 主要文件/包影响（清单，随实现打勾）

| 路径/包 | 说明 |
|---------|------|
| `internal/engine/spawn*.go` | `Spawner`、`LoopPolicy`、`SpawnHandle` / `ChildMailbox`、`applyChildToolFilter`、`toolInvocationRecorder`、`RunState.Terminate` 实装 |
| `internal/defaults/loop.go`, `pkg/runtime/request` | **决议**：`SpawnMaxDepthDefault=2`；新增 `BudgetTokensTreeDefault` / `BudgetUSDTreeDefault`（全树共享）；`AllowStream` 不进 JSON Schema |
| `internal/engine/tooling.go` | builtin 注册与合并；`agent_transfer` Handle **硬校验** `ActiveChildRuns == 0` |
| `pkg/runner/runner.go` 及相关 | 子 `RunID`、Spawner 依赖；root agent system prompt 模板补充「有活跃子 Agent 时禁止 transfer」 |
| `pkg/runtime/exchange` | `SpawnSpec` 增 `ToolBlocklist` / `MemoryDigest` / `Lifecycle` / `AllowChildSpawn` / `AllowStream`（保留）；`SpawnResult` 增 `ToolInvocations` / `Handle` |
| `pkg/runtime/event/event.go` | spawn 事件；中间子级 function-call 事件**不**对外发布 |
| `pkg/runtime/outcome` | `RunMetrics` 全树共享；`ChildSubtree` rollup |
| `pkg/skill` | 与 `SpawnSpec.SkillIDs` **解析** 边界 |

---

## 5. 与 `progress` 的同步约定

- **较大范围**或**任务顺序**变更时：**先** 改 `doc/plan/plan-v0.8.0.md` **并** 在 `doc/log/progress-v0.8.0.md` 对应段落**补一句**引用或**变更日志**行。  
- **已处于执行中** 的 `progress` 段落：遵循 `rei-doc-mandatory`「**不**改正在执行段」——仅动**未开始**部分，除非用户确认调整。

---

## 6. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：与 `progress-v0.8.0` 配套的 **Step 2** 实施计划。 |
| 2026-04-24 | 与 **`spawn-runtime-rules.md`** 对齐：子 **Run** **异步**、不阻塞父**主路径**；**删除** 与**本文** 冲突的「MVP 同步子 Run 整段」 表述。 |
| 2026-04-24 | 对齐 **`spawn-runtime-rules` §5**：阶段 **C** 子 **LLM** **非流式**；关联表与 **`multi-agent-engine`** 摘要已更新。 |
| 2026-04-24 | `spawn-runtime-rules` 新增 **§5 嵌套 spawn 授权**（原 §5 非流式顺延为 §6）：阶段 **B** 增任务 3——`Spawner.Spawn` 入口短路 `child_spawn_forbidden`，未授权时**不**为子级注册 `spawn_subagent` 工具；`SpawnSpec.allow_child_spawn` 默认 `false`；关联表与 **`multi-agent-engine`** 摘要同步。 |
| 2026-04-24 | `spawn-runtime-rules` 新增 **§7 子工具集与 ReAct 聚合**：阶段 **B** 增任务 4——`SpawnSpec` 增 `ToolBlocklist`，`applyChildToolFilter` 实现「白先黑后」过滤（`spawn_subagent` 除外）；子 `ToolDispatcher` 挂 `toolInvocationRecorder`，最终在 `SpawnResult.ToolInvocations` 中聚合；中间 function-call / tool_result **不**回填父侧事件流；`SpawnToolHandler` 通过 `EmitChildToolInvocations` 让父 Agent 向外发送工具调用报文；关联表 `pkg/skill` 行 § 序号同步至 §7；**`multi-agent-engine`** §3.8 摘要同步。 |
| 2026-04-24 | **风险评审定稿** 同步：阶段 A 增任务 3（**全树共享预算**）+ 任务 4（**父结束子立即取消**）；阶段 B 任务 2 标注「`allow_stream` 不入 Schema、`memory_digest` 默认 null」；阶段 B 任务 4 标注「双名单 **glob 通配**」；阶段 C 增任务 3（**`agent_transfer` 与 spawn 互斥**：system prompt + 工具 Handle 双重保障）；阶段 E 测试矩阵补齐（`MaxDepth=2`、glob、父 ctx 取消、全树预算、未授权不挂工具、`AllowStream=true` 仍非流式、活跃子 Agent 时 transfer 拒绝）；先决与确认表标记「评审定稿」。文件影响清单按决议刷新。 |
| 2026-04-24 | **Slice1（最小可演示）已落地**（`pkg/agent` + `pkg/runner`，**未** 实装 `internal/engine` 空壳）：`SpawnSpec` 增加 `MemoryDigest` / `Lifecycle` / `AllowChildSpawn`；`defaultSpawner` + 内置 `spawn_subagent`（`defaults.SpawnMaxDepthDefault=2`；未授权不注册子级 spawn 工具；子 `RunLoop` 事件不写入父 ch）；`LoopState.CurrentRunRef` + `WithSpawn`；`doc/log/progress` 已追加 **Slice1 与后续 TODO**。 |

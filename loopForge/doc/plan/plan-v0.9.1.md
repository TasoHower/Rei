# loopForge 实施计划 — v0.9.1（工具与事件）

> **对应版本进度**：`doc/log/progress-v0.9.1.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的**任务拆分与执行顺序**；**先于编码**成文；与 `progress` 同步维护「未开始 / 进行中 / 已完成」状态（不重复粘贴全文，以 **progress 为版本真相源**）。  
> **主目标**：实现子 Agent 工具过滤与 ReAct 聚合、spawn 事件体系、agent_transfer 与 spawn 互斥。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.1.md` | 版本目标、范围、交付清单 |
| `doc/design/spawn-runtime-rules.md` §7 | 工具集与 ReAct 聚合（白先黑后、glob 通配、中间事件隔离） |
| `doc/design/data-fusion.md` | 事件命名与扩展字段 |
| `doc/design/multi-agent-engine.md` §3.8 | spawn 产品语义 |
| `doc/log/progress-v0.9.0.md` | v0.9.0 交付基础 |

---

## 2. 先决与确认（编码前完成）

- [ ] 确认 `glob.Match` 或 `path/filepath.Match` 作为通配匹配库。
- [ ] 确认 `EmitChildToolInvocations` 的承载通道形态（`RuntimeEvent` vs OTel trace 双写）。
- [ ] 确认事件命名最终对表（与 `data-fusion.md` review）。

---

## 3. 任务分阶段（建议顺序）

### 阶段 A — 工具过滤

1. **实现 `applyChildToolFilter`**：在 `Spawner.runChild` 中按 `SpawnSpec.ToolAllowlist` / `ToolBlocklist` 做白先黑后过滤；`spawn_subagent` 不走双名单。
2. **glob 通配支持**：双名单条目用 `path/filepath.Match`（或 `glob` 库）匹配工具名（如 `mcp:foo/*`）。
3. **写测**：allow 非空仅保留匹配工具；block 命中剔除；通配前缀 `mcp:foo/*` 整段屏蔽；`spawn_subagent` 始终保留。

### 阶段 B — ReAct 聚合

1. **实现 `toolInvocationRecorder` Middleware**：挂到子 `ToolDispatcher`，拦截每次 function-call，按顺序聚合 `ToolInvocation`（Name / Arguments / ResultDigest / StartedAt / EndedAt / Error / MCPSource）。
2. **聚合回填**：`runOneTurn` 跑完后 `rec.Snapshot()` 写入 `SpawnResult.ToolInvocations`。
3. **父侧事件桥**：`SpawnToolHandler` 调 `ParentEventBridge.EmitChildToolInvocations` 让父 Agent 外发。
4. **中间事件隔离**：子级中间 function-call / tool_result **不**写入父侧 `mb.outEvent`。
5. **写测**：子 N 次工具调用 → `ToolInvocations` 长度为 N 且顺序一致；中间事件不出现于父侧。

### 阶段 C — spawn 事件

1. **新增事件类型**：`EventSpawnStart`、`EventSpawnEnd`、`EventSpawnWake` + Payload（`child_run_id` / `parent_run_id` / `depth` / `lifecycle` / `wake_seq`）。
2. **事件发出**：`Spawner.Spawn` 入口发 `EventSpawnStart`；子 Run 结束时发 `EventSpawnEnd`；`SpawnHandle.Wake` 时发 `EventSpawnWake`。
3. **写测**：spawn 起止事件可消费；payload 字段完整。

### 阶段 D — agent_transfer 与 spawn 互斥

1. **system prompt 写入**：root agent system prompt 模板中显式写入「有活跃子 Agent 时禁止 transfer」。
2. **工具 Handle 硬校验**：`agent_transfer` Handle 执行前检查 `RunState.ActiveChildRuns == 0`；命中返回 `Code = "transfer_blocked_active_children"`。
3. **写测**：spawn → 立即 transfer 被拒绝；spawn → 回收 → transfer 通过。

---

## 4. 主要文件/包影响

| 路径/包 | 说明 |
|---------|------|
| `internal/engine/child_tool_filter.go` | `applyChildToolFilter` 白先黑后 + glob 通配 |
| `internal/engine/tool_invocation_recorder.go` | `toolInvocationRecorder` Middleware + `Snapshot` |
| `internal/engine/spawn_tool.go` | `EmitChildToolInvocations` 桥接 |
| `internal/engine/tooling.go` | `agent_transfer` Handle 硬校验 |
| `pkg/runtime/event/event.go` | 新增 `EventSpawnStart` / `EventSpawnEnd` / `EventSpawnWake` |
| `pkg/runtime/exchange/exchange.go` | `ToolInvocation` 结构体完善 |
| `pkg/runner/runner.go` | root agent system prompt 模板补充 |
| `pkg/runtime/event` | 中间子级 function-call 事件过滤 |

---

## 5. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：从原 v0.9.0 PRD 拆分出的工具与事件子版本实施计划 |

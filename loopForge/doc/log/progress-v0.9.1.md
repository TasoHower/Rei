# loopForge 项目进度日志 — v0.9.1

> **版本**：v0.9.1  
> **日期**：2026-04-24（计划稿）  
> **里程碑**：**工具与事件** — 工具白/黑名单过滤、ReAct 聚合、spawn 事件、transfer 互斥  
> **上一版本**：[v0.9.0](progress-v0.9.0.md)（核心约束）  
> **下一版本**：[v0.9.2](progress-v0.9.2.md)（生命周期最终）  
> **设计依据**：`doc/design/spawn-runtime-rules.md` §7（工具集与聚合）、§5（非流式）、`doc/design/data-fusion.md`（事件）  
> **配套实施计划**：`doc/plan/plan-v0.9.1.md`

---

## 本版本目标

1. **ToolAllowlist / ToolBlocklist glob 通配**：双名单条目支持 `glob.Match`（`*` / `?`），如 `mcp:foo/*` 整段屏蔽。
2. **applyChildToolFilter**：实现白先黑后过滤逻辑；`spawn_subagent` 工具不走双名单。
3. **SpawnResult.ToolInvocations 聚合**：子 ReAct 中间 function-call / tool_result 按发生顺序聚合；中间步骤不进入父侧事件流。
4. **EmitChildToolInvocations 外发**：`ParentEventBridge` 让父 Agent 向外发送工具调用报文。
5. **spawn 事件**：新增 `EventSpawnStart` / `EventSpawnEnd` / `EventSpawnWake`；与 `data-fusion.md` 命名对齐。
6. **agent_transfer 与 spawn 互斥**：root agent system prompt 显式写入约束；`agent_transfer` 工具 Handle 硬校验 `ActiveChildRuns == 0`。

---

## 现状与边界（相对 v0.9.0）

| 层次 | 现状（计划起点） |
|------|-----------------|
| **工具过滤** | Slice1 无双名单过滤；子级工具与父同源但不经收敛 |
| **ToolInvocations** | `SpawnResult.ToolInvocations` 结构体已定义但无聚合逻辑 |
| **事件** | 无 `spawn_*` 类型事件 |
| **transfer 互斥** | transfer 不校验活跃子 Agent |

---

## 交付清单

- 双名单 glob 通配测试覆盖
- `applyChildToolFilter` 白先黑后逻辑
- `SpawnResult.ToolInvocations` 聚合 + 中间事件隔离
- `EmitChildToolInvocations` 外发通道
- `spawn_start` / `spawn_end` / `spawn_wake` 事件
- transfer 互斥拦截

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：v0.9.1 版本计划（工具与事件子版本，从原 v0.9.0 PRD 拆分） |

# loopForge 项目进度日志 — v0.9.8

> **版本**：v0.9.8  
> **日期**：2026-05-01（实施完成）  
> **里程碑**：transfer 语义拆分——将 spawn 生命周期从 `EventAgentTransfer` 中独立为 `EventSpawnStart` / `EventSpawnEnd`，消除事件语义混淆  
> **上一版本**：[v0.9.7](progress-v0.9.7.md)（tools 自动化注册——基于 struct 反射自动生成 JSON Schema）  
> **配套实施计划**：`doc/plan/plan-v0.9.8.md`

---

## 本版本目标

1. **新增 `EventSpawnStart` / `EventSpawnEnd` 事件类型**：在 `pkg/runtime/event` 中定义专用的事件消息类型和 payload 结构体，使 spawn 生命周期拥有独立事件标识。
2. **改造 `DefaultSpawner.Spawn()` 事件发射**：将子 Agent 启动/结束/取消时的 `EventAgentTransfer` 替换为 `EventSpawnStart` / `EventSpawnEnd`，清除语义混淆。
3. **清理 `AgentTransferPayload` 的 spawn 承载残留**：移除 `ChildRunID` / `Depth` / `OK` 字段，`agent_transfer` 事件恢复纯 transfer 语义。
4. **测试覆盖**：验证 spawn 事件流不再包含 `EventAgentTransfer`，transfer 事件流不再包含 `EventSpawnStart` / `EventSpawnEnd`（现有测试天然覆盖）。
5. **文档更新**：修订 `doc/design/03-events.md`，新增 spawn 事件定义并更新关联设计文档。

---

## 关键变更清单

### Go 代码修改

| 文件 | 说明 |
|------|------|
| `pkg/runtime/event/event.go` | 新增 `EventSpawnStart` / `EventSpawnEnd` 常量 + `SpawnStartPayload` / `SpawnEndPayload` 结构体 + marker 方法 + accessor；`AgentTransferPayload` 移除 `ChildRunID` / `Depth` / `OK` |
| `pkg/agent/spawn.go` | `DefaultSpawner.Spawn()` 中 3 处事件发射全部替换：`EventAgentTransfer` → `EventSpawnStart` / `EventSpawnEnd`；payload 从 `AgentTransferPayload` → `SpawnStartPayload` / `SpawnEndPayload`（含 `TaskSummary` 截断、`Metrics` 携带） |

### 文档变更

| 文件 | 说明 |
|------|------|
| `doc/design/03-events.md` | §2.8 清理为纯 transfer 语义；新增 §2.9 `spawn_start`、§2.10 `spawn_end`；§3.3 时序图更新；§4 配对联更新；§7 文件表更新；后续章节重编号 |
| `doc/design/00-abstractions.md` | 事件枚举表新增 `EventSpawnStart` / `EventSpawnEnd`；spawn 事件描述更新；决策参考更新 |
| `doc/design/10-spawn.md` | 事件标记从 `agent_transfer(phase=start/end, depth>0)` 改为 `spawn_start / spawn_end`；流程图更新 |
| `doc/design/07-transfer.md` | 对比表事件标记更新 |
| `doc/design/02-runner-core.md` | 注释补充 transfer 限定 |

---

## 执行流程

已按 plan 分 4 阶段一次性实施：

```
阶段 A  事件类型与数据模型（event.go 新增类型 + payload）     ✅
阶段 B  Spawn 生命周期事件发射改造（spawn.go 替换事件类型）    ✅
阶段 C  测试（单元 + 集成 + 回归）                             ✅ 全部通过
阶段 D  文档与验收                                              ✅
```

## 当前代码状态

| 阶段 | 状态 |
|------|------|
| A — 事件类型与数据模型 | ✅ 已完成 |
| B — Spawn 生命周期事件发射改造 | ✅ 已完成 |
| C — 测试 | ✅ 全部通过（build + vet + test） |
| D — 文档与验收 | ✅ 已完成 |

---

## 风险与待决

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| 外部消费者可能按 `EventAgentTransfer` + `ChildRunID` 解包 spawn 信息 | 删除 `AgentTransferPayload` 承载字段会破坏兼容性 | v0.9.8 已移除字段，消费者需改为 `EventSpawnStart` / `EventSpawnEnd` |

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-01 | 初稿：v0.9.8 进度文档，覆盖 transfer 语义拆分的事件模型重构。 |
| 2026-05-01 | 移除阶段 C（工具层拆分），确认本版本仅为事件语义拆分，不改工具。 |
| 2026-05-01 | 实施完成：event.go + spawn.go 代码修改 + 4 份设计文档更新 + build/vet/test 全部通过。 |

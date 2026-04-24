# loopForge 项目进度日志 — v0.9.2

> **版本**：v0.9.2  
> **日期**：2026-04-24（计划稿）  
> **里程碑**：**生命周期最终** — Linger 唤醒机制、ToolInvocations 脱敏、事件命名对表、遗留待决完成  
> **上一版本**：[v0.9.1](progress-v0.9.1.md)（工具与事件）  
> **设计依据**：`doc/design/spawn-runtime-rules.md` §3（linger）、`doc/design/data-fusion.md`（事件）  
> **配套实施计划**：`doc/plan/plan-v0.9.2.md`

---

## 本版本目标

1. **LifecycleLinger 完整实现**：子 Agent 支持 linger 模式，执行完单次任务后进入等待态。
2. **SpawnHandle / ChildMailbox 完整实现**：通道铺设完成，有界缓冲（默认 16）；inbox / outResult / outEvent 通道消费。
3. **WakeMessage 唤醒机制**：父 Agent 通过 `SpawnHandle.Wake` 投递唤醒消息，子侧 `select` 消费后重启子 `RunLoop`；`WakeControlCancel` 支持优雅关闭。
4. **ToolInvocations 脱敏与上限**：`Arguments` 默认脱敏（token/api_key 等字段）；配置条数上限与单条 `ResultDigest` 长度上限。
5. **事件命名对表**：与 `data-fusion.md` / 历史 `prd-v0.2.0-streaming` 命名差异统一。
6. **遗留待决完成**：`EmitChildToolInvocations` 承载通道确定；`ToolInvocations` 上限默认值定稿。

---

## 现状与边界（相对 v0.9.1）

| 层次 | 现状（计划起点） |
|------|-----------------|
| **linger** | `LifecycleLinger` 字段保留但验收只覆盖 `ephemeral` 路径；`SpawnHandle` / `ChildMailbox` 通道未接入子 RunLoop |
| **脱敏** | 无脱敏策略；`ToolInvocation.Arguments` 原样输出 |
| **事件命名** | 临时命名未与 `data-fusion.md` 对表 |
| **遗留待决** | 4 项未定：脱敏、承载通道、上限、命名 |

---

## 交付清单

- linger 生命周期端到端测试
- `SpawnHandle.Wake` 唤醒 + 空闲超时自动回收
- `ToolInvocation` 脱敏策略生效
- 事件命名与 `data-fusion.md` 一致

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：v0.9.2 版本计划（生命周期最终子版本，从原 v0.9.0 PRD 拆分） |
| 2026-04-24 | 修复 spawn 异步子任务在父通道关闭后回写导致的 panic（send on closed channel）；补充父 RunLoop 对异步子任务收口等待，保证子结果事件在 query_end 前可见 |

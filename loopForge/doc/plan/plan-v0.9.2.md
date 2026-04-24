# loopForge 实施计划 — v0.9.2（生命周期最终）

> **对应版本进度**：`doc/log/progress-v0.9.2.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的**任务拆分与执行顺序**；**先于编码**成文；与 `progress` 同步维护「未开始 / 进行中 / 已完成」状态（不重复粘贴全文，以 **progress 为版本真相源**）。  
> **主目标**：完整实现 linger 生命周期与唤醒机制、ToolInvocations 脱敏、事件命名对表、遗留待决完成。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.2.md` | 版本目标、范围、交付清单 |
| `doc/design/spawn-runtime-rules.md` §3 | 生命周期（ephemeral / linger） |
| `doc/design/data-fusion.md` | 事件命名与扩展字段 |
| `doc/design/multi-agent-engine.md` §3.8 | spawn 产品语义 |
| `doc/log/progress-v0.8.0.md` | 遗留待决子表（脱敏、上限、承载通道、命名） |

---

## 2. 先决与确认（编码前完成）

- [ ] 确认 `SpawnHandle.inbox` / `ChildMailbox.in` 通道的默认缓冲大小（建议 16）。
- [ ] 确认 `MaxIdleAfterFinish` 默认值（建议 30s）。
- [ ] 确认 `ToolInvocations` 默认上限（建议 100 条 / 单条 4KB）。
- [ ] 确认 `Arguments` 脱敏规则（如 `api_key` / `token` / `secret` 等字段模式匹配）。

---

## 3. 任务分阶段（建议顺序）

### 阶段 A — Linger 完整实现

1. **`SpawnHandle` / `ChildMailbox` 通道接入**：将 `inbox chan WakeMessage`、`outResult`、`outEvent` 接入子 `RunLoop` 的 `select` 多路复用。
2. **`runChild` linger 分支**：子 RunLoop 执行完初始 `WakeMessage` 后，若 `Lifecycle == linger`，进入 waiting 循环——`select { case <-ctx.Done(): / case <-idle.C: / case msg := <-mb.in: }`。
3. **`SpawnHandle.Wake` 实现**：向 `inbox` 投递 `WakeMessage`；`Control=Cancel` 时优雅关闭。
4. **空闲超时自动回收**：`MaxIdleAfterFinish` 到达后子 goroutine 退出；`close(outResult)` / `close(outEvent)`。
5. **写测**：ephemeral 即跑即走；linger 唤醒后子跑第二回合；空闲超时后子退出；`WakeControlCancel` 优雅关闭。

### 阶段 B — ToolInvocations 脱敏与上限

1. **脱敏策略**：在 `toolInvocationRecorder.Middleware` 中或 `ToolInvocation` 构造时扫描 `Arguments` 中 `api_key` / `token` / `secret` / `password` 等敏感字段，替换为 `"***"`。
2. **条数上限**：`recorder` 最多记录 N 条（默认 100），超出丢弃后续记录并有日志告警。
3. **单条长度上限**：`ResultDigest` 截断至 4KB（默认），超出部分省略加后缀 `... (truncated)`。
4. **可配置**：上限通过 `SpawnSpec` 或 `LoopPolicy` 暴露配置入口。
5. **写测**：敏感字段脱敏；超上限丢弃但不断言；超长 `ResultDigest` 截断。

### 阶段 C — 事件命名对表 + 遗留待决

1. **事件命名统一**：对照 `data-fusion.md` 和历史 `prd-v0.2.0-streaming`，确认 `spawn_start` / `spawn_end` / `spawn_wake` 最终名称；更新所有引用处。
2. **`EmitChildToolInvocations` 承载通道定稿**：走 `RuntimeEvent` 还是 OTel trace 双写；定稿后文档化并更新代码。
3. **`ToolInvocations` 默认值定稿**：默认条数上限（100）、单条 `ResultDigest` 长度（4KB）、脱敏字段列表。

---

## 4. 主要文件/包影响

| 路径/包 | 说明 |
|---------|------|
| `internal/engine/spawn_handle.go` | `SpawnHandle` / `ChildMailbox` 完整实现；`Wake` / `Result` / `Events` / `Close` |
| `internal/engine/spawner_default.go` | `runChild` linger 分支（idle loop + WakeMessage select） |
| `internal/engine/tool_invocation_recorder.go` | 脱敏策略 + 上限逻辑 |
| `pkg/runtime/event/event.go` | 事件命名最终定稿 |
| `internal/defaults/loop.go` | `MaxIdleAfterFinishDefault` / `ToolInvocationsMaxDefault` / `ResultDigestMaxLenDefault` |
| `pkg/runtime/exchange/exchange.go` | `SpawnSpec.MaxIdleAfterFinish`、脱敏配置字段 |

---

## 5. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-24 | 初稿：从原 v0.9.0 PRD 拆分出的生命周期最终子版本实施计划 |
| 2026-04-24 | 增补修复项：`spawn_subagent` 异步路径防止向已关闭父事件通道发送；父 `RunLoop` 在完成前等待异步子任务收口，避免子结果丢失与 panic |

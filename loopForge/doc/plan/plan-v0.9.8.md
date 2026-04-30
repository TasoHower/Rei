# loopForge 实施计划 — v0.9.8（transfer 语义拆分）

> **对应版本进度**：`doc/log/progress-v0.9.8.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的**任务拆分与执行顺序**；**先于编码**成文；与 `progress` 同步维护「未开始 / 进行中 / 已完成」状态（不重复粘贴全文，以 **progress 为版本真相源**）。  
> **主目标**：将当前混用 `EventAgentTransfer` 承载的 spawn 生命周期拆分为独立的 **`EventSpawnStart` / `EventSpawnEnd`** 事件类型，使 **transfer**（会话级 Agent 切换）、**spawn start**（子 Agent 启动）、**spawn end**（子 Agent 结束）在事件层面各司其职，清除语义混淆。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.8.md` | 版本目标、范围、风险、交付清单、与设计的交叉引用 |
| `doc/design/data-fusion.md` | 事件与扩展字段（需补充 `EventSpawnStart` / `EventSpawnEnd` 定义） |
| `doc/plan/plan-v0.8.0.md` | spawn 原始实施计划（含转移互斥、嵌套授权等背景） |
| `doc/design/multi-agent-engine.md` §3.8 | spawn 产品语义与生命周期 |

---

## 2. 先决与确认（编码前完成）

- [ ] 定稿 **事件类型命名**：`EventSpawnStart` / `EventSpawnEnd`（不引入 `EventSpawn` 泛型事件，保持 start/end 双事件模式对齐已有的 `EventAgentTransfer` + `TransferPhase` 惯例但用独立类型表达，便于消费者精确订阅）。
- [ ] 定稿 **SpawnStartPayload / SpawnEndPayload 字段**（见阶段 A 任务 2 字段表）。
- [ ] `progress` 中「本版本目标」与本文「任务分阶段」**无**未解决冲突。

---

## 3. 任务分阶段（建议顺序）

### 阶段 A — 事件类型与数据模型

1. **在 `pkg/runtime/event/event.go` 新增事件类型**：
    ```go
    EventSpawnStart EventMessageType = "spawn_start"
    EventSpawnEnd   EventMessageType = "spawn_end"
    ```
    在 payload marker switch 中添加对应分支。

2. **定义专用的 payload 结构体**：
    ```go
    // SpawnStartPayload 描述子 Agent 启动信息。
    type SpawnStartPayload struct {
        ChildRunID  string `json:"child_run_id"`
        ParentRunID string `json:"parent_run_id,omitempty"`
        AgentRole   string `json:"agent_role"`
        Depth       int    `json:"depth"`
        TaskSummary string `json:"task_summary,omitempty"` // spec.Task 的前 N 字符摘要
    }

    // SpawnEndPayload 描述子 Agent 结束信息。
    type SpawnEndPayload struct {
        ChildRunID  string `json:"child_run_id"`
        ParentRunID string `json:"parent_run_id,omitempty"`
        AgentRole   string `json:"agent_role"`
        Depth       int    `json:"depth"`
        OK          bool   `json:"ok"`
        Status      string `json:"status"`       // completed / failed / rejected
        ErrorCode   string `json:"error_code,omitempty"`
        FinalTextLen int   `json:"final_text_len,omitempty"`
        Metrics     *outcome.RunMetrics `json:"metrics,omitempty"`
    }
    ```
    **决议**：`SpawnEndPayload` 不含完整 `FinalText`（避免大文本驻留在事件通道缓冲），消费者可通过 `ChildRunID` 自行关联 `SpawnResult`。

3. **删除 `AgentTransferPayload` 对 spawn 的承载残留**：确认 `TransferPhase` 和 `AgentTransferPayload.ChildRunID` / `.Depth` / `.OK` 字段是否仍为 transfer 所需；若仅 spawn 使用则将其从 `AgentTransferPayload` 摘除，保持 transfer 事件干净。

### 阶段 B — Spawn 生命周期事件发射改造

1. **修改 `pkg/agent/spawn.go` `DefaultSpawner.Spawn()`**：
    - 子 Agent 启动处（当前 L144-L158）：将 `event.EventAgentTransfer` / `event.TransferStart` 替换为 `event.EventSpawnStart` + `SpawnStartPayload`。
    - 子 Agent 结束处（当前 L167-L184）：将 `event.EventAgentTransfer` / `event.TransferEnd` 替换为 `event.EventSpawnEnd` + `SpawnEndPayload`。
    - 父级取消导致子未正常结束处（当前 L213-L231）：同样替换为 `EventSpawnEnd` + `SpawnEndPayload{OK: false}`。
    - 移除 `AgentTransferPayload` 的 `FromAgent` 字段填充（spawn 场景下不需要 transfer 的"来源 Agent"语义）。

2. **确认 `pkg/runner/runner.go` `runTransferLoop` 中的事件发射**（当前 L355-L360）：
    - `EventAgentTransfer` + `TransferPhase` 在此处已经是纯粹的 transfer 语义，无需改动。
    - 确认 `AgentTransferPayload` 的 `ChildRunID` / `Depth` 字段在 transfer 场景下不再使用 → 可以考虑从 `AgentTransferPayload` 中移除，或保留为空（兼容性）。**决议**：保留 `ChildRunID` / `Depth` 在 `AgentTransferPayload` 中作为可选字段，但文档注明「仅 spawn 事件填充，transfer 事件为空」。

3. **更新事件消费者/测试辅助**：
    - 在 `runner_test.go` / `helpers_test.go` 中：确保任何断言 `event.EventAgentTransfer` 的地方，对于 spawn 场景改为断言 `event.EventSpawnStart` / `event.EventSpawnEnd`。
    - `events` 相关监听代码若按 `AgentTransferPayload` 解包 spawn 信息，需更新。

### 阶段 C — 测试

1. **单元测试**：
    - `SpawnStartPayload` / `SpawnEndPayload` 的 JSON 序列化/反序列化。
    - spawn 事件不包含 `AgentTransferPayload` 字段（如 `FromAgent` 在 spawn 端为 zero value）。
    - `AgentTransferPayload` 在 transfer 场景下 `ChildRunID` / `Depth` 为空。

2. **集成测试**：
    - 单次 spawn → 事件流包含 `EventSpawnStart` → (工具调用事件) → `EventSpawnEnd`，**不**包含 `EventAgentTransfer`。
    - 单次 transfer → 事件流包含 `EventAgentTransfer`（始末各有 TransferPhase），**不**包含 `EventSpawnStart` / `EventSpawnEnd`。
    - spawn + transfer 混合场景：spawn 进行中不允许 transfer（现有互斥逻辑，验证事件类型不受影响）。

3. **回归测试**：
    - 运行现有的 `TestRunner_*` 测试，确保 transfer 路径行为不变。
    - 运行 spawn 相关测试，确保 `SpawnResult` 内容不变。

### 阶段 D — 文档与验收

1. 更新 `doc/design/data-fusion.md`：新增 `EventSpawnStart` / `EventSpawnEnd` 事件定义，移除 `AgentTransferPayload.ChildRunID` / `.Depth` / `.OK` 的 spawn 承载说明（或注明仅向前兼容）。
2. 更新 `doc/acceptance/`：创建 `v0.9.8-acceptance.md`（若当前版本有验收流程要求）。
3. 更新 `doc/packages/loopForge-pkg-runtime-event.md` 六段（影响 `event.go` 包）和 `doc/packages/loopForge-pkg-agent.md`（影响 `spawn.go`）。

---

## 4. 主要文件/包影响（清单，随实现打勾）

| 路径/包 | 说明 |
|---------|------|
| `pkg/runtime/event/event.go` | 新增 `EventSpawnStart` / `EventSpawnEnd` + `SpawnStartPayload` / `SpawnEndPayload`；清理 `AgentTransferPayload` 中 spawn 专用字段（或注明残留兼容） |
| `pkg/agent/spawn.go` | `DefaultSpawner.Spawn()` 替换事件发射为 `EventSpawnStart` / `EventSpawnEnd`；调整 payload 字段 |
| `pkg/runner/runner.go` | 确认 `runTransferLoop` 事件发射不受影响 |
| `pkg/runner/runner_test.go` | 测试用例更新：spawn 事件断言改为 `EventSpawnStart` / `EventSpawnEnd` |
| `pkg/runner/helpers_test.go` | 测试辅助更新 |
| `doc/design/data-fusion.md` | 补充 spawn 事件定义 |

---

## 5. 与 `progress` 的同步约定

- **较大范围**或**任务顺序**变更时：**先** 改 `doc/plan/plan-v0.9.8.md` **并** 在 `doc/log/progress-v0.9.8.md` 对应段落**补一句**引用或**变更日志**行。  
- **已处于执行中** 的 `progress` 段落：遵循 `rei-doc-mandatory`「**不**改正在执行段」——仅动**未开始**部分，除非用户确认调整。

---

## 6. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-05-01 | 初稿：transfer 语义拆分 — `EventSpawnStart` / `EventSpawnEnd` 独立事件类型。 |
| 2026-05-01 | 移除阶段 C（工具层拆分），确认本版本仅为事件语义拆分，不改工具。 |

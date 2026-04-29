# loopForge 实施计划 — v0.9.3（DeepSeek + Reasoning + Spawn 收口）

> **对应版本进度**：`doc/log/progress-v0.9.3.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的任务拆分与执行顺序；用于本轮“总结现状 + 补文档”落档。  
> **主目标**：对齐当前代码事实，补齐 v0.9.3 文档基线，明确已完成能力与剩余缺口。

---

## 1. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.3.md` | v0.9.3 版本真相源（现状、范围、交付、风险） |
| `doc/log/progress-v0.9.2.md` | 上一版本基线（生命周期最终） |
| `doc/design/spawn-runtime-rules.md` | spawn 运行时规则约束 |
| `doc/design/data-fusion.md` | 事件语义与前端消费约定 |

---

## 2. 任务分阶段（文档补齐）

### 阶段 A — 代码现状盘点（已完成）

1. 盘点 `pkg/model/adapters/deepseek` 新增实现：`client.go`、`convert.go`、`stream.go`。
2. 盘点 Reasoning 字段贯通：`pkg/model/types/message.go`、Lark/DeepSeek 适配层、`pkg/agent/stream.go`。
3. 盘点 spawn 收口行为：`LoopState` 异步子任务结果收集与回注（`pkg/agent/agent.go`、`pkg/agent/loop.go`、`pkg/agent/spawn_tool.go`）。
4. 盘点前端可视化行为：`test-server/static/index.html` 中 reasoning 气泡与 `is_final` 覆盖刷新逻辑。

### 阶段 B — v0.9.3 进度文档落盘（本轮执行）

1. 新增 `doc/log/progress-v0.9.3.md`，记录：
   - 本版本目标与完成项；
   - 关键变更清单（模型层、Agent 层、事件层、test-server）；
   - 当前边界与后续待办。
2. 保持“代码事实优先”，不写与现有实现冲突的承诺。

### 阶段 C — 计划文档落盘（本轮执行）

1. 新增 `doc/plan/plan-v0.9.3.md`（本文件），将“补档任务”的步骤、范围和依赖固定下来。
2. 与 `progress-v0.9.3.md` 建立双向对应，避免后续版本追踪断层。

### 阶段 D — 验证与收尾（本轮执行）

1. 运行与本次变更相关的测试/构建命令，确认文档中“验证状态”表述准确。
2. 若测试失败，文档中显式记录失败项与影响范围，不做隐式跳过。

---

## 3. 影响清单（本轮补档）

| 路径 | 类型 | 说明 |
|------|------|------|
| `doc/plan/plan-v0.9.3.md` | 新增 | v0.9.3 补档执行计划 |
| `doc/log/progress-v0.9.3.md` | 新增 | v0.9.3 进度与现状真相源 |

---

## 4. 验收标准

1. `v0.9.3` 的计划与进度文档均已创建，且互相引用。
2. 文档中的能力描述与当前代码一致，不出现“已实现但代码不存在”或“代码已落地但文档缺失”。
3. 对 DeepSeek 接入、Reasoning 流、spawn 子结果回注、前端 reasoning 展示均有清晰记录。
4. 留有明确“后续待办”，便于下一版本继续推进。

---

## 5. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-29 | 初稿：v0.9.3 文档补齐计划，目标为“代码现状总结 + 版本文档落盘”。 |

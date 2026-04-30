# loopForge 项目进度日志 — v0.9.7

> **版本**：v0.9.7  
> **日期**：2026-04-30（文档初始化）  
> **里程碑**：tools 自动化注册——通过 struct 类型自动生成 JSON Schema，消除手写 Parameters  
> **上一版本**：[v0.9.6](progress-v0.9.6.md)（全量移除 agent-sdk-go，OpenAI SDK 填充兜底）  
> **配套实施计划**：`doc/plan/plan-v0.9.7.md`

---

## 本版本目标

1. **新增 `pkg/tool/autoreg/` 包**：基于 Go struct 反射的 JSON Schema 自动生成器（`SchemaFromStruct[T]`）和类型安全注册器（`NewToolFromStruct[T]`）。
2. **重构 4 个现有工具**：`var_set`、`execute_shell_script`、`load_skill`、`spawn_subagent` 改为使用自动化注册，消除手写 Parameters 和 Handle 内重复 JSON 解析。
3. **兼容性保障**：逐工具对比反射生成与原始手动 Parameters，确保 `required` 一致、`description` 一致、字段类型一致（含 `map[string]interface{}` 无 additionalProperties、空 required 省略键、`Lifecycle` 隐藏等 10 项精细化调整）。
4. **全面单元测试**：覆盖所有 Go 基础类型映射、可选/必需推导、嵌套结构体、empty required 省略、`map[string]interface{}` 无 additionalProperties 等场景。
5. **文档更新**：修订 `doc/design/04-tools.md`，新增自动化注册章节（§9）。

---

## 关键变更清单

### Go 代码新增

| 文件 | 说明 |
|------|------|
| `pkg/tool/autoreg/schema.go` | `SchemaFromStruct[T any]()` — 反射式 JSON Schema 生成器 |
| `pkg/tool/autoreg/handler.go` | `ToolHandlerTyped[T any]` 类型 + `NewToolFromStruct[T any]()` 类型安全注册器 + `ToolOption`/`WithParameters` 废弃兼容机制 |
| `pkg/tool/autoreg/doc.go` | 包文档 |
| `pkg/tool/autoreg/schema_test.go` | 单元测试（覆盖所有基础类型、可选/必需推导、嵌套、RawMessage、`map[string]interface{}`、empty required 省略、类型别名、WithParameters 废弃兼容） |

### Go 代码修改

| 文件 | 说明 |
|------|------|
| `pkg/variable/tools.go` | 重构 `var_set`：删除手写 Parameters（17 行）+ 删除 Handle 内 `sonic.UnmarshalString` |
| `pkg/skill/shell_tool.go` | 重构 `execute_shell_script`：删除手写 Parameters（21 行）+ 删除 Handle 内 `json.Unmarshal`；为 `shellToolArgs` 补齐 `description` tags |
| `pkg/skill/load_tool.go` | 重构 `load_skill`：删除手写 Parameters（10 行）+ 删除 Handle 内 `json.Unmarshal` |
| `pkg/agent/spawn_tool.go` | 重构 `spawn_subagent`：删除手写 Parameters（33 行）+ 删除 Handle 内 `sonic.UnmarshalString`；为 `spawnSubagentArgs` 补齐 `description` tags |

### 文档变更

| 文件 | 说明 |
|------|------|
| `doc/design/04-tools.md` | §2.1 示例代码更新为自动化注册；§7 文件表追加 `autoreg/`；新增 §9 自动化注册章节 |
| `doc/log/progress-v0.9.7.md` | 本文件 |

---

## 执行流程

⚠ 本版本采用**逐个作业确认制**。详见 `doc/plan/plan-v0.9.7.md` §3。执行顺序：

```
作业 #0  新增 pkg/tool/autoreg/ (schema.go + handler.go + doc.go)  → 你确认 → 我实施
作业 #1  重构 var_set 工具                                          → 你确认 → 我实施
作业 #2  重构 execute_shell_script 工具                              → 你确认 → 我实施
作业 #3  重构 load_skill 工具                                       → 你确认 → 我实施
作业 #4  重构 spawn_subagent 工具                                   → 你确认 → 我实施
作业 #5  修订 doc/design/04-tools.md                                → 你确认 → 我实施
作业 #6  新增 schema_test.go 单元测试                                → 你确认 → 我实施
作业 #7  创建 progress-v0.9.7.md                                    → 你确认 → 我实施
作业 #8  最终验证                                                    → 你确认 → 我实施
```

---

## 当前代码状态

| 作业 | 状态 |
|------|------|
| 新增 `pkg/tool/autoreg/` 包 (schema.go + handler.go + doc.go) | ✅ 已完成 |
| 重构 `var_set` 工具 | ✅ 已完成 |
| 重构 `execute_shell_script` 工具 | ✅ 已完成 |
| 重构 `load_skill` 工具 | ✅ 已完成 |
| 重构 `spawn_subagent` 工具 | ✅ 已完成 |
| 修订 `doc/design/04-tools.md` | ✅ 已完成 |
| 新增 `schema_test.go` 单元测试 (17 tests) | ✅ 已完成 |
| 最终验证 (build/vet/test) | ✅ 全部通过 |

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-30 | 初稿：v0.9.7 进度文档，初始化阶段覆盖 tools 自动化注册的计划。 |
| 2026-04-30 | 实施完成：核心包 autoreg (3 文件 + 测试)、4 工具重构、文档修订、build/vet/test 全部通过。 |

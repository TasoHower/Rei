# loopForge 项目进度日志 — v0.5.1

> **版本**：v0.5.1  
> **日期**：2026-04-16  
> **里程碑**：VarStore 驱动的 `{{name}}` 占位 + 可选 **SystemPromptBuilder**（每步动态组合 system，再经 VarStore 做 `{{}}` 替换）  
> **上一版本**：v0.5.0（Variable）

---

## 本版本目标

1. **双花括号占位**：在 **system** 与 **user** 文案中支持 `{{name}}`，取值来自当前 **`VarStore`** 的键（大小写不敏感匹配 key；不做驼峰/下划线归一；未命中则保留原文 `{{...}}`）。**不在 `RuntimeRequest` 上增加业务参数字段**；会话恢复通过外部 **`Materialize` / 快照 → `runner.WithVarStore(*VarStore)`** 完成。
2. **每次调用 LLM 前刷新**：合并 `SystemInstructions`、（若开启 Variable）`PromptBlock()` 后，对 **system** 与 **user** 侧模板用 **当前** `VarStore` 重算 `{{...}}`，保证变量在 tool 执行后变化能反映到下一轮模型输入。
3. **SystemPromptBuilder（system 组合回调）**：可选；若设置，则在 **每一步** 在合并 `SystemInstructions`、Variable 块之后调用，得到 **system 文案**，再对该文案做 `{{...}}` 替换；未设置则使用默认合并（仅 `SystemInstructions` + `PromptBlock()`）。回调返回 error 时 Run 报错终止（`system_prompt_builder`）。Transfer 继承历史消息时仍按 **当前 Agent** 的 `SystemInstructions` / builder 重算 system（与首轮 user 是否回调无关——user 侧始终用 `RuntimeRequest.UserMessage` 为模板并做 `{{}}` 替换，见下节）。

---

## 核心概念

### 与 Runner / VarStore 的关系

- **Runner** 仅通过已有选项 **`WithVarStore(*VarStore)`** 注入本轮 Store；**不**解析业务侧自定义结构体，也 **不**增加其它请求参数字段。
- 外部持久化：**运行结束**通过 `RuntimeOutcome.VarStore` 将引用交回调用方；**核心库不写库**，由 SDK / 应用做 `Snapshot` 持久化（与 v0.5.0 一致）。

### 模块关系（示意）

```
   SDK / 应用层                         loopForge
   ┌──────────────────┐                ┌─────────────────────────────┐
   │ 快照 JSON 等      │──Materialize──▶│ VarStore（Runner 可选注入）  │
   └──────────────────┘                │                             │
           │                             │  SystemInstructions         │
           │                             │  + PromptBlock (可选)       │
           │                             │  + SystemPromptBuilder (可选)│
           │                             │       ↓                     │
           │                             │  ReplaceDoubleBraceParams   │
           │                             │  （键值来自 VarStore）       │
           │                             │       ↓                     │
           │                             │  每步 LLM 前刷新 system/user │
           └◀──── RuntimeOutcome.VarStore ──────────────────────────┘
```

### 用户消息处理顺序（新会话，`inheritedMsgs == nil`）

1. `userTemplate = req.UserMessage`（**无**单独 UserMessage 侧 builder；动态组合在 **system** 侧由 **SystemPromptBuilder** 承担）。
2. 对 `userTemplate` 做 **`{{...}}` 替换**（VarStore 当前值），得到首轮展示与首条 user 内容。
3. **每一步 LLM 前**：用 **最新** VarStore 再次替换 **首条 user 消息**中的 `{{...}}`（与 system 同步刷新）。

### 占位符规则摘要

- 形态：`{{` + 标识符 + `}}`，内层 `TrimSpace` 后参与与 **VarStore key** 的匹配。
- **大小写不敏感**（`strings.EqualFold`）；**不**做驼峰与下划线互换。
- **未命中**：保留整段 `{{...}}`。
- 值的字符串化：`string` 原样；`nil` 为 `<unset>`；其余 `json.Marshal`（与 `[Variables]` 块中非字符串展示思路一致）。

---

## 交付清单

- [x] `pkg/agent` — `ReplaceDoubleBraceParams`、`stringParamsFromVarStore`；RunLoop 每步刷新 system 与首条 user 中的占位符。
- [x] `pkg/agent` — `SystemPromptBuildContext`、`SystemPromptBuilder`、`WithSystemPromptBuilder`；`Agent.Clone` 拷贝 `SystemPromptBuilder`。
- [x] 单元测试：`brace_params_test.go`、`user_message_test.go`（system builder + brace）、`clone_test.go` 等。
- [x] 文档：本文件 + `doc/acceptance/v0.5.1-acceptance.md`。

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-16 | 初稿与多轮修订。 |
| 2026-04-16 | 去掉「请求 Parameters」叙事；占位符统一来自 VarStore；补充 SystemPromptBuilder 与每步刷新。 |
| 2026-04-16 | 文档命名统一：原「UserMessageBuilder」口径改为 **SystemPromptBuilder**（动态组合仅在 system 侧；user 为模板 + `{{}}`）。 |

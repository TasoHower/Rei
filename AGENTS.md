# AGENTS.md（Rei.）

本文件位于**仓库根目录**，供人类与会在根目录查找 `AGENTS.md` 的工具使用。Cursor **项目规则**以 **`.cursor/rules/*.mdc`** 为准（YAML 前置元数据 + 正文）。

## 规则文件（Cursor）

| 文件 | 作用 | 应用范围 |
|------|------|----------|
| `.cursor/rules/rei-gated-workflow.mdc` | **门禁流程** — 分析 → Plan → 编码 → commit；未经确认不得写入 | `alwaysApply: true` |
| `.cursor/rules/rei-doc-mandatory.mdc` | **文档规范** — `doc/packages/` 六段、`plan` 落盘、`progress-*.md` 体例 | `alwaysApply: true` |
| `.cursor/rules/rei-go.mdc` | **Go 编码** — 风格、禁止 `iota`、错误处理、分层约定 | `globs: **/*.go` |

详细说明见 **`.cursor/rules/README.md`**。

## 仓库结构（实际）

- 各**顶层模块**（例如 `loopForge/`）自带 **`doc/`**（例如 `loopForge/doc/log/progress-v*.md`）。
- **没有**覆盖全仓库的单一根目录 `doc/`；以各模块 `doc/` 为准。

## 快速参考

### 标准工作流程

1. **Step 1** — 需求分析 + 版本草案（只读）
2. **Step 2** — 实施 Plan（只读，`plan` 落盘）
3. **Step 3** — 编码实施（允许写入）
4. **Step 4** — Commit 文案（最后确认）

**未经用户明确确认，不得进入下一步。**

### 文档要求

- **包文档**：`doc/packages/{module}-{path}.md`，必须包含六段结构
- **版本计划**：`doc/log/progress-v*.md`，对齐 v0.7.0 体例
- **Plan 落盘**：任何 Plan 必须写入文件，不得仅在聊天中

### Go 编码规范

- **禁止** `iota`（使用显式常量）
- **禁止** 吞掉错误
- **必须** 使用 `gofmt`
- **优先** 具体类型而非 `any`

## Git 策略

未经用户明确要求，Agent **不得**：
- 创建/合并分支
- **push** 远端
- 修改 Git 历史

本地改动遵循 **rei-gated-workflow.mdc** 与用户确认。

## 相关文档

- `.cursor/rules/README.md` — 规则目录说明
- `loopForge/doc/log/progress-v0.7.0.md` — 版本计划参考体例

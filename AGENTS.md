# AGENTS.md（Rei.）

本文件位于**仓库根目录**，供人类与会在根目录查找 `AGENTS.md` 的工具使用。Cursor **项目规则**以 **`.cursor/rules/*.mdc`** 为准（YAML 前置元数据 + 正文）。

## 规则文件（Cursor）

| 文件 | 作用 | 应用范围 |
|------|------|----------|
| `.cursor/rules/rei-gated-workflow.mdc` | **门禁流程** — 分析 → Plan → 编码 → commit；未经确认不得写入 | `alwaysApply: true` |
| `.cursor/rules/rei-doc-mandatory.mdc` | **文档规范** — `doc/plan/plan-v*.md` 与 `doc/log` 配套、`doc/packages/` 六段、`progress-*.md` 体例 | `alwaysApply: true` |
| `.cursor/rules/rei-loopforge.mdc` | **loopForge** — v0.8.0 **spawn** 与 `doc/plan` 执行顺序 | `globs: loopForge/**` |
| `.cursor/rules/rei-go.mdc` | **Go 编码** — 风格、禁止 `iota`、错误处理、分层约定 | `globs: **/*.go` |

详细说明见 **`.cursor/rules/README.md`**。

## 仓库结构（实际）

- 各**顶层模块**（例如 `loopForge/`）自带 **`doc/`**（例如 `loopForge/doc/plan/plan-v*.md`、`loopForge/doc/log/progress-v*.md`）。
- **没有**覆盖全仓库的单一根目录 `doc/`；以各模块 `doc/` 为准。

## 核心原则
- **DRY** 避免重复代码
- **KISS** 简单、直接、粗暴（`KISS`）
- **YAGNI** 不会使用到的代码，不要实现
- **LOD** 低耦合、高内聚（`LOD`）
- **注释** 本项目的注释可以使用中文
- **代码文件约束** 超过 500 行的文件，必须分层。
- **逻辑核心** 关键逻辑核心代码，必须进行详细注释。
- **关键变量** 关键变量必须进行注释，说明值的含义。
- **落档留痕** 任何 coding 必须同步更新版本日志以及模块文档
- **事实依据** 不要猜测，如果有疑问，可以补充日志后要求再次执行

## 快速参考

### 标准工作流程

1. **Step 1** — 需求分析 + 写入 `doc/PRD/`
2. **Step 2** — 生成计划：各顶模块 `doc/plan/plan-v*.md`（**实施拆分**）并与 `doc/log/progress-v*.md` 配套，**先 plan 后编码**
3. **Step 3** — 实施代码并更新版本日志（`doc/log/progress-v*.md`）
4. **Step 4** — 验收代码（`doc/acceptance/v0.7.0-acceptance.md`）
5. **Step 5** — 提交代码（`git commit`）

**未经用户明确确认，不得进入下一步。**

### 文档要求

- **包文档**：`doc/packages/{module}-{path}.md`，必须包含六段结构
- **实施计划**：`doc/plan/plan-v*.md`（任务分阶段、**先于编码**），与 `doc/log/progress-v*.md` 配套
- **版本计划**：`doc/log/progress-v*.md`，对齐 v0.7.0 体例
- **Plan 落盘**：任何 Plan 必须写入文件，不得仅在聊天中传递

### Go 编码规范

- **禁止** `iota`（使用显式常量）
- **禁止** 吞掉错误
- **必须** 使用 `gofmt`
- **优先** 具体类型而非 `any`
- **禁止** 必须在写入端关闭 channel, 严格禁止在读取端关闭

## Git 策略

未经用户明确要求，Agent **不得**：
- 创建/合并分支
- **push** 远端
- 修改 Git 历史

本地改动遵循 **rei-gated-workflow.mdc** 与用户确认。

## 相关文档

- `.cursor/rules/README.md` — 规则目录说明
- `loopForge/doc/plan/plan-v0.8.0.md` — v0.8.0 实施计划（spawn）
- `loopForge/doc/log/progress-v0.7.0.md` — 版本计划参考体例

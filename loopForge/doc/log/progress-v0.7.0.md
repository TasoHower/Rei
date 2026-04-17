# loopForge 项目进度日志 — v0.7.0

> **版本**：v0.7.0  
> **日期**：2026-04-17（计划稿）  
> **里程碑**：**Skills（技能 / 指令包）** — 在 Agent loop 中支持可发现、可版本化、可审计的 **Skill** 装载与注入（对齐 `doc/design/multi-agent-engine.md` §**3.6** 与 Cursor **Agent Skills** 一类体验）  
> **上一版本**：[v0.6.0](progress-v0.6.0.md)（MCP 客户端与工具接入；联调锚点 SeRagLF / `seraglf`）

---

## 本版本目标

1. **载体与元数据**：约定 **Skill** 的磁盘形态（优先 `**SKILL.md`**：front matter 含 `name`、`version`、`description` 等）或等价 **JSON 清单**；引擎解析为内部 `**SkillSpec`**（名称唯一、版本可比较、正文可注入）。
2. **发现**：启动时扫描可配置的 `**SKILL_PATH`**（可多目录、顺序有意义）；可选 **运行时**通过白名单工具 `**load_skill(name)`** 再装载（若开放，则必须配合 **allowlist** 与审计）。
3. **注入**：将 skill 正文以 **system** 或 **developer** 消息片段挂到 **指定 Agent**（根 Agent / 后续 spawn 子 Agent）；与 **MCP Prompts** 的合并策略在引擎层显式定义（例如 **后加载覆盖** 或 **priority** 字段）。
4. **与子 Agent 的衔接（最小）**：为后续 **spawn** 预留参数形状（如 `**skill_ids`**），本版本至少保证 **静态 Network / 单 Agent** 路径上 skill 列表可配置；若 **spawn** 尚未落地，则在文档中标注 **顺延** 项。
5. **安全与观测**：Skill 仅允许来自 **受信根目录** 或 **签名清单**；禁止未校验的任意路径；**slog** 记录「谁、何时、加载哪一版 skill」；OpenTelemetry 侧为 **Skill 装载** 增加 span（与 §3.5 表格一致）。
6. **文档与验收**：新增 `**doc/acceptance/v0.7.0-acceptance.md`**；更新 `**doc/design/multi-agent-engine.md**` §3.6 的实现状态指针与示例配置片段。

---

## 现状与边界（相对 v0.6.x）


| 层次        | 现状（计划起点）                                                                                                                     |
| --------- | ---------------------------------------------------------------------------------------------------------------------------- |
| **设计**    | `multi-agent-engine.md` §3.6 已描述 Skills 的产品语义与与 MCP Prompts 的关系。                                                             |
| **代码**    | 主路径以 `**Agent` + `ToolInfo` + MCP `BootstrapToolInfos`** 为主；**尚无** 独立 `pkg/skill`（或等价模块）与 `**SKILL_PATH` 扫描器**。              |
| **与 MCP** | v0.6.0 解决 **tools** 统一面；v0.7.0 **不重复** MCP 协议工作，仅在「提示来源管线」上与 `**prompts/list` / `get`** **可组合**（优先级与合并规则在本版本写清 **最小可行** 策略）。 |


---

## 「可执行」Skill 与脚本：计划支持几种、为什么

### 术语

- **「在引擎内执行脚本」**：由 loopForge **直接解释或调用**某种语言运行时（如 **shell / Python / JavaScript**），在 **Agent 进程或子进程**里产生副作用。  
- **「可执行」的日常说法**：有时指「skill 里写了步骤，模型会照着做」——那是 **LLM 执行自然语言程序**，**不是**引擎执行脚本。下文分开写清。

### v0.7.0 范围：**frontmatter 先行 + 按需加载 + 受控脚本执行**


| #     | 形态                                                | 是否「引擎执行代码」                                           | v0.7.0                  |
| ----- | ------------------------------------------------- | ---------------------------------------------------- | ----------------------- |
| **A** | `**SKILL.md` frontmatter**（name/description/allowed-tools/model/license） | 是；用于 **首阶段筛选与路由** | **支持**（首阶段必做） |
| **B** | **按需加载正文/参考/资产**（body + `references/` + `assets/`） | 是；按选中 skill 再加载，避免全量装载 | **支持** |
| **C** | **执行 scripts**（`scripts/`） | 是；**默认**走 **统一 shell tool**；可选走 **`ToolCallHandler` + `Executor`** 多运行时（见 **§1.2.1**），受策略与完整性校验约束 | **支持（受控开启）** |


**结论**：v0.7.0 采用 **两阶段加载 + 受控执行**。Agent 先读 frontmatter 决策，再按需加载其余内容，最终在策略允许时执行 `scripts/`。**默认**由 Agent 按 prompt 调用 **统一 shell tool**；若产品需要 **Python / Node / HTTP** 等直连执行，可在集成层为 bundle 内脚本 **注册带 `Handle` 的 `ToolInfo`**，由 **`Executor`** 按 **`Runtime` 或 `detectRuntime(entry)`** 分发（见 **§1.2.1**）。

### 为什么这样定

1. **安全与合规**：任意「skill 里嵌脚本」会把 **提示注入面** 扩成 **任意代码执行面**，审计与沙箱成本陡增；与 **「受信根目录下的文本技能包」** 产品定义冲突。
2. **职责分离**：**确定性副作用、对外 I/O** 已统一落在 **Tool / MCP**（v0.6.0）；Skill 负责 **策略与行文**，避免在引擎里再维护 **第二套工具运行时**。
3. **简化设计**：所有 skill 脚本统一使用 **shell** 执行，执行逻辑由 skill.md 的 prompt 部分告诉 Agent，Agent 调用统一的 shell 执行 tool。
4. **与 `multi-agent-engine.md` §3.6 一致**：**禁止**默认把 `SKILL.md` 当脚本直接执行；技能执行通过标准 tool 调用机制完成。
5. **工程主次（当前阶段）**：本阶段优先交付 **frontmatter 首阶段加载、按需加载与路由**；`scripts/` 执行通过 **统一 shell tool** 启用，并受 feature flag、allowlist、timeout 与 sandbox 约束。

### 当前阶段主次（摘要）


| 优先级           | 内容                                                                                                            |
| ------------- | ------------------------------------------------------------------------------------------------------------- |
| **P0（本阶段实现）** | `**SkillRegistry`**、`**SKILL.md` frontmatter 首阶段解析**、**按选择加载 body/references/assets**、**统一 shell 执行 tool**、**日志 / trace**、与 **Agent / Runner** 配置接线。 |
| **P1（受控开启）** | 在策略允许时执行 `scripts/` 中的 shell 脚本，并做完整性 + 配额 + sandbox 校验。 |
| **后续增强** | 缓存与并发配额、失败回退策略。 |


### Skill 执行接口（v0.7.0 受控执行语义）

所有 skill 的脚本执行统一使用 **shell**；v0.7.0 通过 feature flag 受控开启执行路径，并逐步补齐沙箱、配额与依赖策略。

建议落点：`**pkg/skill/job.go`**（或 `**pkg/skill/skilljob`），与 `**SkillRegistry`** 解耦：**Registry** 负责解析与选择，**统一 shell tool** 负责执行 scripts 并回填结果。

#### 与 VarStore 的交互

SkillJob 执行时可能需要访问用户传入的动态参数（来自 v0.5.1 的 VarStore 机制）。执行结果通过 **Tool 调用** 或 **Variable 机制** 返回，最终通过 `RuntimeOutcome.VarStore` 交回外部。

示意（**英文标识符**；实现须以未来 ADR 为准）：

```go
// pkg/skill — executable skill job contract.

// SkillJob describes a runnable artifact (path, argv, env policy TBD).
type SkillJob struct {
	WorkingDir string
	Entry      string   // script path, e.g., "scripts/setup.sh"
	Args       []string
	// Context: optional user parameters from VarStore snapshot (v0.5.1 compatibility).
	// Passed to skill scripts for context recovery; not directly used in runner internal logic.
	Context map[string]any
}

// SkillJobRunner runs skill jobs under sandbox/timeout policy.
// All scripts are executed via shell (sh/bash).
type SkillJobRunner interface {
	Run(ctx context.Context, job SkillJob) (stdout string, err error)
}
```

**说明**：
- 执行路径必须经过 feature flag、allowlist 与完整性校验；默认可保持关闭，按环境逐步放开。
- `Job.Context` 来自 `VarStore` 快照，用于恢复 skill 的参数上下文，但不直接参与 runner 内部逻辑。
- SkillJob 执行结果通过 `stdout` 返回，应用层可选择：
  1. 作为 **Tool 调用结果** 回灌到 Agent loop
  2. 写入 **VarStore** 通过 `RuntimeOutcome.VarStore` 返回外部
  3. 记录到 **slog/trace** 用于审计
- **所有脚本统一使用 shell 执行**，skill.md 的 prompt 部分告诉 Agent 如何调用。

### 路线图（v0.8+，非 v0.7.0 承诺）

若产品日后需要 **优化执行性能**，计划上 **倾向先优化 shell 执行器**（例如 **缓存执行结果** 或 **受限 shell 环境**），把 **语义、配额、沙箱** 说死，再考虑其他运行时——**不**一上来并行支持多种通用脚本语言，以避免 **安全组合爆炸** 与 **维护面** 失控。具体选型需在独立 ADR 中论证。

---

## 改造计划（v0.7.0）

1. **API 形状**：新增 `**SkillRegistry`**（或等价名）：`LoadFromPaths(ctx, paths []string) error`、`Get(name) (*SkillSpec, error)`、`List() []SkillMeta`；**线程安全**与 **热加载策略**（默认启动时一次性加载；可选 **watch**，不阻塞 MVP）。
2. **Runner / Agent 构造**：在 `**AgentConfig`**（或等价结构）上增加 `**Skills []string` 或 `SkillRefs**`；`**bindModel` 前** 将 skill 正文 **拼接进 system/developer**（顺序：基础 system → MCP prompt 片段（若有）→ skills 按配置顺序）。
3. **校验**：与 `**tool.ValidateBindings`** 类似，对 **未知 skill 名** 在 **构造期** 失败并返回明确错误（避免运行时才暴露）。
4. **测试**：单元测试覆盖 front matter 解析、路径遍历、重名冲突、注入顺序；可选 **集成测试** 使用临时目录 fixture。
5. **示例**：在 **test-server** 或 **loopforged** 增加最小 `**SKILL_PATH` + 示例 SKILL.md**，便于一键演示。

---

## 代码改造计划（数据结构 / 接口预留）

以下名称均为 **计划稿**，实现时可微调，但需在 PR 中说明与本文档的偏差。

### 编码约定（`pkg/skill` 与文档示例）

- **保持类型简洁**：所有 skill 相关类型使用英文标识符，避免冗余注释。
- **默认 shell**：MVP 优先 **统一 shell tool**；多运行时 **仅**通过 **`Executor` + 显式 `Runtime` 或 `auto` + `detectRuntime`** 接入，避免在 `SKILL.md` 正文里隐式执行代码。

### 1. 新增 `pkg/skill`（或等价包名）


| 类型 / API            | 职责                                                                                                                                                                     |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `**SkillSpec`**     | 单次解析结果：`Name`、`Version`、`Description`、`Body`（注入用正文）、`SourcePath`（审计）、`ContentHash`（可选，用于热更新检测）。                                                                        |
| `**SkillMeta**`     | 列表/发现用轻量信息：`Name`、`Version`、`Description`。                                                                                                                             |
| `**SkillRegistry**` | `LoadFromPaths(ctx, paths []string) error`；`Get(name string) (*SkillSpec, error)`；`List() []SkillMeta`；可选 `Generation() uint64` 或等价 **世代号**，供 per-Run 快照与动态 reload 区分。 |
| `**RunContext`**    | **per-Run 快照**：包含 `SkillsSnapshot []SkillSpec` 和 `VarStore *variable.VarStore`；确保并发 Run 之间互不影响。 |
| **解析器**             | `ParseSKILLFile(path string) (*SkillSpec, error)`；多目录下 **重名策略**（先赢 / 后赢 / 报错）在 `LoadFromPaths` 内固定并文档化。                                                                |


**线程安全与 per-Run 隔离**：
- `**Get` / `List` 读路径** 与 `**LoadFromPaths` / `Reload` 写路径** 需 **RWMutex** 或 **atomic 指针替换整表**（推荐后者降低锁竞争）。
- **per-Run 隔离**：每个 Run 开始时创建 `RunContext`，固化当时的 `SkillRegistry` 快照和 `VarStore`；即使后续 Registry 热更新，进行中的 Run 仍使用旧快照。
- 示意：
```go
type RunContext struct {
	SkillsSnapshot []SkillSpec  // 固化 Run 开始时的 skill 列表
	VarStore       *variable.VarStore  // 用户参数上下文（v0.5.1）
}
```

#### 1.1 Skill 文件解析结构与关键类型

**磁盘形态（约定）**：单文件 `**SKILL.md`**，**必须包含 YAML front matter**（`---` … `---`），其后为 **Markdown 正文**（注入 `**SkillSpec.Body`**）。等价 **JSON 清单** 可由 `**ParseSkillManifest([]byte) ([]SkillSpec, error)`** 单独入口解析，字段与 front matter **对齐**。

**Frontmatter 字段（对齐官方约定）**：

| 字段 | 必需 | 规则 / 说明 |
|------|------|-------------|
| **`name`** | 是 | 最多 **64** 字符；仅允许 **`[a-zA-Z0-9_-]`**（与 ToolInfo.Name 命名规则一致）。 |
| **`description`** | 是 | 非空；最多 **1024** 字符；应描述 skill 功能与使用时机。 |
| **`allowed-tools`** | 否 | 逗号分隔工具列表，如 `Read,Write,Bash(git:*)`；解析后建议标准化为 `[]string`。 |
| **`model`** | 否 | 指定模型；缺省继承当前会话模型。 |
| **`license`** | 否 | 许可证信息。 |

**项目扩展字段（可选）**：`version`、`author`、`tags` 等可继续保留，但不得与上表的官方字段语义冲突。


| 结构（计划名）                | 字段 / 含义                                                                                                                                                                                           |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `**SkillFrontmatter`** | 从 YAML 解出官方字段：`**name**`、`**description**`、`allowed-tools`、`model`、`license`；并支持项目扩展字段（如 `version`、`author`、`tags`）。 |
| `**ParsedSkillFile`**  | `**Frontmatter SkillFrontmatter**`、`**Body string**`（front matter 之后全文，trim 首尾空白）、`**SourcePath string**`、`**RawSHA256 []byte**` 或 `**ContentHash string**`（整文件哈希，用于热更新与审计）。                      |
| `**SkillSpec**`        | 由 `**ParsedSkillFile**` **物化**为注册表条目：`**Name`** ← `Frontmatter.name`，`**Version**`、`**Description**`、`**Body**`、`**SourcePath**`、`**ContentHash**`；可选 `**Extra**` 透传。                             |
| `**SkillMeta**`        | `**Name`、`Version`、`Description`、可选 `SourcePath**`；供 `**List()**` 与 UI，**不含** 大段 `Body`。                                                                                                          |


**解析管线（逻辑顺序）**：

1. 读文件字节 → 可选 **UTF-8 校验**（非法则报错）。
2. **切分 front matter**：若无 `---` front matter，**直接报错**（`SKILL.md` frontmatter 为必需项）。
3. **YAML.Unmarshal** → `**SkillFrontmatter`**；执行强校验：`name` 正则 `^[a-zA-Z0-9_-]{1,64}$`、`description` 非空且长度 `<=1024`。
4. 解析 `allowed-tools`（若存在）为标准 `[]string`，并校验每个工具项非空。
5. 校验 `model` / `license`（若存在）字段类型为字符串；`model` 为空时继承会话模型。
6. 组装 `**ParsedSkillFile`** → 再生成 `**SkillSpec**` 填入 `**SkillRegistry**`。

示意（**英文标识符**）：

```go
// pkg/skill — parse model (illustrative)

type SkillFrontmatter struct {
	Name        string
	Description string
	AllowedTools []string
	Model       string
	License     string
	Version     string
	Author      string
	Tags        []string
	Extra       map[string]any // optional: unknown YAML keys
}

type ParsedSkillFile struct {
	Frontmatter SkillFrontmatter
	Body        string
	SourcePath  string
	ContentHash string // e.g. SHA-256 hex of full file bytes
}

type SkillSpec struct {
	Name        string
	Version     string
	Description string
	AllowedTools []string
	Model       string
	License     string
	Body        string
	SourcePath  string
	ContentHash string
	Extra       map[string]any
}

// SkillMeta is the list/discovery view without Body.
type SkillMeta struct {
	Name        string
	Version     string
	Description string
	SourcePath  string
}
```

**JSON 清单（可选第二载体）**：例如 `skills.json` 数组元素字段与 `**SkillFrontmatter` + 单文件 `body` 字段** 同构，便于 CI 生成；`**LoadFromPaths`** 可约定 **仅目录递归 `**/SKILL.md`**，或 **额外** 读根清单（实现时二选一或并存，写清优先级）。

**与 `SkillJob` 的关系**：`**SkillSpec`** 描述 **提示资产**；`**SkillJob`** 描述 **可执行作业**。SkillJob 统一使用 **shell** 执行，执行逻辑由 skill.md 的 prompt 部分告诉 Agent。

#### 1.2 Skill 执行模型

**核心设计**：
- **skill.md 中不声明脚本语言类型** - 移除 `job_kind` 字段
- **所有 skill 脚本统一使用 shell 执行** - 通过统一的 shell tool 调用
- **执行逻辑由 prompt 指示** - skill.md 的 body 部分告诉 Agent 如何调用脚本

**执行流程**：
1. Agent 读取 skill.md 的 frontmatter 和 body
2. body 中的 prompt 指示 Agent 调用哪个脚本（如 `scripts/setup.sh`）
3. Agent 调用统一的 shell 执行 tool（如 `execute_shell_script`）
4. Tool 执行脚本并返回 stdout/stderr

**示例 skill.md**：
```markdown
---
name: code-review
description: Code review skill
allowed-tools: Read,Bash
---

## 使用说明

本 skill 用于代码审查。调用时执行 `scripts/review.sh` 脚本。

## 执行步骤

1. 读取待审查的文件
2. 调用 `bash scripts/review.sh <file_path>`
3. 返回审查结果
```

**优势**：
- **简化设计** - 无需维护多种运行时，统一使用 shell
- **灵活性** - skill 作者可以在 prompt 中自由定义执行逻辑
- **安全性** - 所有脚本通过统一的 shell tool 执行，便于审计和沙箱控制

#### 1.2.1 Skill `scripts/` 执行方式：`ToolCallHandler` + `Executor`（可选多运行时）

**与 loopForge 的衔接**：模型侧仍只认识 **`model.ToolInfo`**（`loopforge/pkg/model/types`）及其 **`Handle ToolCallHandler`**。Skill bundle 内的脚本 **不**自动变成工具；由集成代码在 **加载 skill 后** 为允许的脚本 **构造 `ToolInfo`**，`**Handle**` 绑定到下面的 **`Executor.Execute`**。

**执行形态（示意）**：

1. 为每个允许的脚本条目定义 **`ScriptToolSpec`**（计划名）：`**Name**`（暴露给模型的 tool 名）、`**Entry**`（bundle 内相对路径，如 `scripts/process_data.py`）、`**Runtime**`（`python` / `node` / `http` / `shell` / 空 / `auto`）、`**Description**`、`**Parameters**`（JSON Schema）。  
2. `**makeToolHandler**` 返回 **`ToolCallHandler`**，闭包捕获 `**Executor**` 与 **`ScriptToolSpec`**，在 **`Handle`** 内调用 `**Executor.Execute**`。  
3. `**Executor.Execute**`：若 `**Runtime**` 为空或为 **`auto`**，则调用 **`detectRuntime(Entry)`**；再 **`switch`** 到 `**runPython**` / `**runNode**` / `**callHTTP**` / `**runShell**`。  
4. **安全**：`**Entry**` 必须限制在 **skill bundle 根目录** 下；`**detectRuntime**` 的推断结果若与 allowlist 不符则 **拒绝执行**；`**unknown**` 必须报错，**禁止**回退到任意 shell。

**说明**：当前仓库的 **`ToolInfo`** 尚无 `Runtime` / `Entry` 字段；上文的 **`ScriptToolSpec`** 为 **skill 执行层** 的扩展结构，**实现时**可放在 **`pkg/skill/exec`** 或集成代码中，再组装成 **`model.ToolInfo`**（`**Handle**` 由 `makeToolHandler` 生成）。

示意代码（**英文**；类型名与仓库可微调）：

```go
package skillexec

import (
	"context"
	"fmt"
	"strings"

	"loopforge/pkg/model/types"
)

// ScriptToolSpec binds a bundle script to a model-facing tool name.
// Not part of model.ToolInfo today; used to build ToolInfo.Handle.
type ScriptToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]interface{}
	Entry       string
	Runtime     string // "", "auto", "python", "node", "http", "shell"
}

type Executor struct{}

func makeToolHandler(exec *Executor, tool ScriptToolSpec) types.ToolCallHandler {
	return func(ctx context.Context, argumentsJSON string) (string, error) {
		return exec.Execute(ctx, tool, argumentsJSON)
	}
}

func (e *Executor) Execute(ctx context.Context, tool ScriptToolSpec, args string) (string, error) {
	runtime := tool.Runtime
	if runtime == "" || runtime == "auto" {
		runtime = detectRuntime(tool.Entry)
	}

	switch runtime {
	case "python":
		return runPython(ctx, tool.Entry, args)
	case "node":
		return runNode(ctx, tool.Entry, args)
	case "http":
		return callHTTP(ctx, tool.Entry, args)
	case "shell":
		return runShell(ctx, tool.Entry, args)
	default:
		return "", fmt.Errorf("unknown runtime: %s", runtime)
	}
}

// detectRuntime infers runtime from entry when Runtime is empty or "auto".
func detectRuntime(entry string) string {
	switch {
	case strings.HasSuffix(entry, ".py"):
		return "python"
	case strings.HasSuffix(entry, ".js"):
		return "node"
	case strings.HasPrefix(entry, "http"):
		return "http"
	case strings.HasSuffix(entry, ".sh"):
		return "shell"
	default:
		return "unknown"
	}
}

func runPython(ctx context.Context, entry string, args string) (string, error)  { return "", nil }
func runNode(ctx context.Context, entry string, args string) (string, error)   { return "", nil }
func callHTTP(ctx context.Context, entry string, args string) (string, error) { return "", nil }
func runShell(ctx context.Context, entry string, args string) (string, error) { return "", nil }
```

**与「仅 shell」策略的关系**：**MVP** 仍可只暴露 **一个** `execute_shell_script` 式工具；当需要 **`.py` / `.js` / HTTP** 时，再按上表 **显式注册** 对应 `ToolInfo`，避免模型绕过 bundle 边界。

#### 1.3 Skill 目录约定（对齐社区实践）与完整性检查

**推荐规范**：`SKILL.md` 为必须项，`scripts/`、`references/`、`assets/` 为可选项。  
采用 **两阶段加载 + 执行** 链路：

1. **阶段 A（预加载）**：Agent 启动时仅解析 `SKILL.md` 的 **frontmatter**，用于做 skill 选择与路由。  
2. **阶段 B（按需加载）**：确定要启用的 skill 后，再加载该 skill 的 `Body`、`references/`、`assets/`。  
3. **阶段 C（执行）**：运行策略允许时，Agent 根据 skill 的 prompt 指示调用统一的 shell tool 执行 `scripts/` 中的脚本。

同时，加载时必须做完整性检查，避免篡改、路径逃逸和不一致加载。

推荐约定如下：


| 路径                | 是否必须 | 加载行为（v0.7.0）                                 |
| ----------------- | ---- | -------------------------------------------- |
| `**SKILL.md`**    | 必须   | 阶段 A 解析 frontmatter；阶段 B 按需加载 body，生成 `SkillSpec` 并注入上下文 |
| `**scripts/**`    | 可选   | 阶段 A 建索引与哈希；阶段 C 在策略允许时由 **统一 shell tool** 执行 |
| `**references/**` | 可选   | 阶段 B 按需加载（文件名、标签、哈希） |
| `**assets/**`     | 可选   | 阶段 B 按需加载模板/静态资源 |


为支持“完整性检查”，新增（计划）结构：


| 结构                         | 字段 / 用途                                                                              |
| -------------------------- | ------------------------------------------------------------------------------------ |
| `**SkillBundleManifest**`  | `SkillName`、`SkillVersion`、`Files []ManifestFile`、`BundleHash`；用于描述 bundle 内文件期望状态   |
| `**ManifestFile**`         | `Path`、`SHA256`、`Size`、`Required`、`Role`（`skill` / `script` / `reference` / `asset`） |
| `**SkillIntegrityReport**` | `OK bool`、`Errors []string`、`Warnings []string`、`VerifiedFiles int`                  |


加载阶段（`LoadFromPaths`）的强制校验（计划）：

1. **结构校验**：必须存在 `SKILL.md` 且包含 front matter；禁止目录外路径引用。  
2. **路径校验**：拒绝 `..`、绝对路径、非法符号链接（symlink escaping）。
3. **哈希校验**：若存在 `manifest`（如 `skill.manifest.json`），逐文件校验 `SHA256` 与 `Size`；不一致直接失败。
4. **最小一致性**：`SkillFrontmatter.name/description` 必填且合法；若存在 `version`，则 `SkillFrontmatter.name/version` 与 manifest 的 `SkillName/SkillVersion` 必须一致。  
5. **可执行资产校验**：
   - 若 `scripts/` 目录存在，则必须包含至少一个可执行脚本（`.sh` 文件）。
   - 执行前校验策略（allowlist、timeout、sandbox）。
6. **审计输出**：记录 `ContentHash`、`BundleHash`、`VerifiedFiles` 到日志与 trace 字段。

示意（英文标识符）：

```go
type ManifestFile struct {
	Path     string
	SHA256   string
	Size     int64
	Required bool
	Role     string
}

type SkillBundleManifest struct {
	SkillName    string
	SkillVersion string
	Files        []ManifestFile
	BundleHash   string
}

type SkillIntegrityReport struct {
	OK            bool
	Errors        []string
	Warnings      []string
	VerifiedFiles int
}
```

### 1.4 Skill 与 MCP Prompts 合并策略

为避免多个提示来源（Base System、MCP Prompts、Skills）合并时的不确定性，明确定义**默认优先级**与**可扩展机制**：

**（1）默认合并顺序**（v0.7.0 固定策略）：

```
1. Base System Prompt（Agent.SystemInstructions）
2. MCP Prompts（按 server 配置顺序追加）
3. Skills（按 Agent.SkillNames 顺序追加）
```

**（2）优先级控制**（可选扩展）：

```go
type SkillInjectionPriority int

const (
	// 默认：追加到 Base System 之后，MCP Prompts 之前
	SkillPriorityAfterBase SkillInjectionPriority = 0
	// 前置：插入到 Base System 之前
	SkillPriorityBeforeBase = 1
	// 覆盖：替换 MCP Prompts（谨慎使用）
	SkillPriorityOverrideMCP = 2
)

type SkillFrontmatter struct {
	// ...
	// Optional: controls where this skill is injected in the system prompt chain.
	// Default: SkillPriorityAfterBase.
	Priority *SkillInjectionPriority `yaml:"priority,omitempty"`
}
```

**（3）冲突处理**：
- 若多个 skill 声明相同的 `name`，以 **先加载** 的为准（`LoadFromPaths` 路径顺序决定）。
- 若 skill 与 MCP Prompt 内容冲突，默认 **后者覆盖前者**（即 Skills 在 MCP 之后）。
- 若需精细控制，使用 `Priority` 字段显式声明。

---

### 2. `agent.Agent` 字段（`pkg/agent/agent.go`）


| 计划字段（示意）                                                   | 说明                                                                                                                         |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `**SkillNames []string**`                                  | 本 Agent 固定加载的 skill 逻辑名列表；**顺序有意义**（注入顺序与列表一致）。                                                                            |
| `**SkillRefs []SkillRef`（可选）**                             | 若需 **pin 版本**：`SkillRef{Name, MinVersion}`；与纯 `SkillNames` 二选一或并存（并存时需在文档定义优先级）。                                           |
| `**SkillRegistry *skill.SkillRegistry`（指针，通常 `json:"-"`）** | 解析 **SkillNames** 的来源；可为 **进程单例** 或由 **Runner** 注入；**nil** 且 **SkillNames 非空** → **构造期 / RunLoop 前** 报错（明确「未挂载 Registry」）。 |


`**Clone()`**：须 **深拷贝** `SkillNames` / `SkillRefs` 切片头；`**SkillRegistry` 指针共享**（只读）；若未来存在 **per-Run skill 叠加**，再增加 Run 级副本字段。

### 3. `agent.Option` 与构造器（`pkg/agent/options.go`）

预留 **functional option**（与现有 `**WithSystemInstructions`**、`**WithMCPServerProfiles**` 同风格）：

- `**WithSkillNames(names ...string)**` / `**WithSkills(registry *skill.SkillRegistry, names ...string)**`（一次设置 Registry + 名称，避免半初始化）。  
- 可选 `**WithSkillRefs(refs ...SkillRef)**`（类型放在 `pkg/skill` 或 `pkg/agent` 需评审：若仅 agent 使用可放 `agent` 包，避免循环 import）。

### 4. `SystemPromptBuildContext` 与 `SystemPromptBuilder`（`pkg/agent/user_message.go`）

当前 `**SystemPromptBuildContext**` 含 `**BaseSystemPrompt**` 等字段。计划 **扩展**（向后兼容：新增字段 + 旧 `SystemPromptBuilder` 可忽略）：


| 计划字段                                                           | 说明                                                                        |
| -------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `**ResolvedSkills []skill.SkillSpec` 或 `[]ResolvedSkillView`** | 已在 **本步** 解析好的 skill（含 body）；供 builder 决定 **拼接格式**（如 `## Skill: {name}`）。 |
| `**SkillInjectionOrder`**                                      | 枚举或文档约定：**append_after_base** / **prepend** 等（若默认策略足够，可先不暴露）。             |


`**RunLoop` / `composeSystem` 路径**（`pkg/agent/loop.go`）：在调用 `**SystemPromptBuilder`** 之前，将 `**Agent.SkillNames` + `SkillRegistry.Get**` 的结果填入 **context**；合并顺序建议 **文档化固定**：`**BaseSystemPrompt`（`SystemInstructions`）→ MCP prompt 片段（若本版本已接）→ skill 正文块**。

### 5. `runner.Runner` 与 `RunOption`（`pkg/runner/runner.go`）


| 计划 API                                            | 说明                                                                                                                                        |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `**WithSkillRegistry(reg *skill.SkillRegistry)`** | 若 **Agent** 上 **未** 显式设置 `**SkillRegistry`**，则在 `**Run` 内** 对 `**entryAgent.Clone()`** 之前 **注入默认 Registry**（与 `**WithVarStore`** 注入模式对称）。 |
| `**WithSkillPath(paths ...string)**`（可选糖）         | 内部 `**skill.NewRegistry()` + `LoadFromPaths**`，便于 demo；生产更推荐 **进程启动时** 建好 **单例 Registry** 再 `**WithSkillRegistry`**。                      |


`**runTransferLoop**`：每个 hop 的 `**Agent.Clone()**` 应已携带 **SkillNames + Registry**；若未来 **transfer 时收紧 skill 列表**，在 **Runner** 层覆写或清空（本版本可 **不实现**，仅预留注释）。

### 6. `request.RuntimeRequest`（`pkg/runtime/request`）

可选扩展（**非 MVP 必选**）：


| 字段                                | 语义                                                                         |
| --------------------------------- | -------------------------------------------------------------------------- |
| `**SkillNamesOverride []string`** | 单次 Run **临时追加或替换** skill 列表（产品与 `**Agent.SkillNames` 合并规则** 需定稿：替换 vs 追加）。 |


若本版本 **不做** 请求级覆盖，则 **不增加字段**，避免 API 膨胀。

### 7. 校验与错误类型

- 新增 `**skill.ErrNotFound`** / `**skill.ErrDuplicate**` 等，或复用 `**pkg/errors**` 中 `**SetupError**` 语义（与 `**bindModel**` 的 `**invalid_config**` 一致）。  
- `**ValidateSkillBindings(names []string, reg *SkillRegistry) error**`：在 `**RunLoop` 入口**（或 `**Agent` 构造完成时**）调用，对齐 `**tool.ValidateBindings`** 的体验。

### 8. 动态加载（接口预留，可与 MVP 分 PR）


| 组件                                                    | 计划                                                                                                                                                                                                              |
| ----------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `**SkillRegistry.Reload(ctx context.Context) error**` | 管理面或测试用；**进行中的 Run** 仍绑定 **旧 generation**（见 `**RunLoop` 入口快照**）。                                                                                                                                                |
| `**load_skill` 工具**                                   | 若实现：作为 `**ToolInfo`**，`**Handle**` 内校验 **allowlist** → 更新 **per-Run** 的 `**[]ResolvedSkill`**（存在 `**LoopState` 扩展字段** 或 **闭包状态**）；**下一轮** `composeSystem` 可见新 skill。**须** 与 `**ExtraTools`** 注册路径对齐，且 **默认关闭**。 |


### 9. 与现有 MCP 生命周期

- `**bindModel`** 仍只负责 **tools**；**skill 注入** 不改变 `**WithTools`** 列表。  
- `**mcpStop**` 与 skill **无耦合**；skill 仅为 **提示文本**，不持有 MCP session。

### 10. 示意：类型与选项（非最终 API）

与 **§1.1** 一致；此处仅列 **Agent 侧** 再摘录。

```go
// pkg/skill — registry (illustrative)
type SkillRegistry struct { /* load/get/list; mu or atomic snapshot */ }

// pkg/agent — illustrative fields on Agent
// SkillNames   []string
// SkillRegistry *skill.SkillRegistry `json:"-"`
```

`**SkillSpec` / `SkillFrontmatter` / `ParsedSkillFile` / `SkillMeta` 的完整字段** 见 **§1.1** 示意块。

### 11. Skill 脚本执行路径（**shell 默认 + Executor 可选**）


| 项 | v0.7.0 |
| --- | --- |
| **`SkillJob` + `SkillJobRunner`** | **bundle 内脚本** 的 **默认**执行面：统一 **shell** 策略（见 **「Skill 执行接口」**）。 |
| **`Executor` + `makeToolHandler` + `detectRuntime`** | **可选**：将 `scripts/` 中条目注册为 **`model.ToolInfo`**，`**Handle**` 走多运行时分支（见 **§1.2.1**）。 |
| **与 `Agent` / `RunLoop`** | **先解析 frontmatter 再按需加载**；执行前 **integrity + allowlist**；模型只通过 **已注册 tool 名** 触发执行。 |


---

## 交付清单

- **解析与注册**：`SKILL.md`（front matter + 正文）或等价 JSON；`**SkillRegistry`** 从 `**SKILL_PATH**` 加载。  
- **注入**：根 Agent（及文档约定的静态 Network 角色）在 **首轮请求前** 将 skill 注入 **system/developer**；顺序与优先级 **可配置且有默认**。  
- **安全**：路径限制在受信根目录；**禁止** 未校验的任意文件路径；加载与解析失败时 **明确错误**（不静默跳过，除非显式 `optional` 标记，若引入需在文档说明）。  
- **观测**：**slog** 审计日志；OTel **Skill 装载** span（与现有 Run span 可关联）。  
- **文档**：`doc/acceptance/v0.7.0-acceptance.md`；更新 `**multi-agent-engine.md`** §3.6 实现指针。  
- **示例**：仓库内示例 skill + 最小配置片段。  
- **Skill 脚本执行**：`**SkillJob` / `SkillJobRunner`（shell 默认）** 与可选 **`Executor` / `makeToolHandler` / `detectRuntime`**（§1.2.1）接入；执行前必须通过策略与完整性校验。

---

## 风险与待决


| 项                        | 说明                                                          |
| ------------------------ | ----------------------------------------------------------- |
| **与 MCP Prompts 的优先级**   | 需在实现前定稿 **默认合并规则**（例如 system 段内固定顺序），避免模型侧不可复现行为。           |
| **spawn 参数 `skill_ids`** | 若 v0.7.0 发布时 **spawn** 仍未合并，本项以 **文档顺延** 为主，不在此版本强绑实现。      |
| **热加载 / watch**          | MVP 以 **进程启动时加载** 为主；目录 watch 可作为 **后续小版本** 扩展，避免 scope 膨胀。 |


---

## 变更日志


| 日期         | 说明                                                                                                                                                                                                                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 2026-04-17 | 初稿：v0.7.0 版本计划（**Skills** 支持；对齐 `multi-agent-engine.md` §3.6）。                                                                                                                                                                       |
| 2026-04-17 | 补充：`multi-agent-engine.md` §3.6 增加 **概念边界、注册/注入、动态加载、`skill function` 语义、与 Tool 分工及执行含义**；详见该节 **「概念 / 关键改造点 / Agent 注册 / 运行时 / 函数与代码」** 各小节。                                                                                        |
| 2026-04-17 | 补充：本文档 **「代码改造计划（数据结构 / 接口预留）」** — `pkg/skill`、`Agent` / `**Option`** / `**Clone**`、`**SystemPromptBuildContext**`、`**Runner` `RunOption**`、`**RuntimeRequest` 可选扩展**、动态 `**load_skill` / `Reload`**、与 **MCP `bindModel`** 边界及示意代码块。 |
| 2026-04-17 | 补充：**「可执行」Skill 与脚本** — v0.7.0 **零种**内置脚本运行时；**三种**内容形态（正文 / 可选结构化块 / Tool 说明书）；理由（安全、职责分离、可测试、与 §3.6 一致）；v0.8+ **若**引入真脚本则倾向 **先一种**受控运行时。                                                                                          |
| 2026-04-17 | 补充：**当前阶段主次** — 本阶段 **不实现** Skill 脚本执行（复杂度高、非主线）；主线为 **Skill 发现 / 注册 / 注入 / 观测**；**仅预留** `SkillJob`、`**SkillJobRunner`**、`**RunPythonSkillJob` / `RunTypeScriptSkillJob` / `RunShellSkillJob**` 等接口（stub 或未接线）；代码改造计划 **§11**。       |
| 2026-04-17 | 补充：**编码约定** — `pkg/skill` 及文档示例 **禁止 `iota`**，枚举 **显式赋值**；新增 **§1.1 Skill 文件解析结构**（`SkillFrontmatter`、`ParsedSkillFile`、`SkillSpec`、`SkillMeta`、解析管线、JSON 清单）；`**SkillJobKind`** 示例改为显式常量；修正 **§10** 与 §1.1 交叉引用。                    |
| 2026-04-17 | 补充：新增 **§1.3 Skill 目录约定与完整性检查**（`SKILL.md` + 可选 `scripts/`/`references/`/`assets/`）；定义 `**SkillBundleManifest` / `ManifestFile` / `SkillIntegrityReport`**，并约束加载阶段必须执行结构、路径、哈希、一致性与审计校验。                                             |
| 2026-04-17 | 补充：`SKILL.md` front matter 改为**必需**；新增官方字段约束（`name`、`description`、`allowed-tools`、`model`、`license`）与强校验规则；`SkillFrontmatter` / `SkillSpec` 示例补充 `AllowedTools`、`Model`、`License`。                                                                       |
| 2026-04-17 | 调整：采用 **frontmatter 首阶段加载 → 按需加载正文/资源 → 受控执行 scripts** 链路；`SkillJobRunner` 从“仅预留”改为“策略控制下可执行”；同步更新 **§1.2/§1.3/§11** 与 `multi-agent-engine.md` §3.6。 |
| 2026-04-17 | 补充：**§1.2.1** — Skill `scripts/` 通过 **`ToolCallHandler` + `Executor.Execute`** 执行；`**Runtime**` 为空或 **`auto`** 时 **`detectRuntime(entry)`**；与 **`model.ToolInfo.Handle`** 对接；**§11** 改为 shell 默认 + Executor 可选。 |


---

## 参考文档

- `doc/design/multi-agent-engine.md` — §3.5 Observability、§**3.6 Skills**、§3.7 MCP（与 Prompts 的关系）  
- `doc/log/progress-v0.6.0.md` — MCP 客户端与工具接入（前置能力）  
- `doc/design/abstractions.md` — Agent / Runner 配置扩展时对齐现有抽象边界


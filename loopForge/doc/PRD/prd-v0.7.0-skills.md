# loopForge 产品需求文档 — v0.7.0 Skills

> **版本**：v0.7.0  
> **日期**：2026-04-17  
> **里程碑**：Skills（技能/指令包）  
> **状态**：计划稿  
> **关联文档**：[progress-v0.7.0.md](../log/progress-v0.7.0.md)、[multi-agent-engine.md](../design/multi-agent-engine.md#36-skills)

---

## 1. 概述

### 1.1 产品定位

**Skills（技能/指令包）** 是 loopForge v0.7.0 的核心功能，提供**可发现、可版本化、可审计**的技能装载与注入机制。通过对齐 `doc/design/multi-agent-engine.md` §3.6 与 Cursor **Agent Skills** 体验，使 Agent 能够在运行时动态加载和执行预定义的技能包。

### 1.2 核心价值

1. **可发现性** — 启动时自动扫描配置目录，发现可用技能
2. **版本化管理** — 每个技能携带版本信息，支持多版本共存
3. **审计与合规** — 所有技能来自受信根目录，加载过程完整记录
4. **灵活注入** — 支持 system/developer 消息片段注入，与 MCP Prompts 可组合
5. **受控执行** — 脚本执行受策略约束（allowlist、timeout、sandbox）

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.6.0** | MCP 客户端与工具接入；v0.7.0 在「提示来源管线」上与 MCP Prompts 可组合 |
| **v0.8.0+** | 后续增强：缓存与并发配额、失败回退策略、更多脚本运行时支持 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.7.0）

#### P0（必须实现）

1. **载体与元数据**
   - 约定 Skill 的磁盘形态（优先 `SKILL.md`：front matter 含 `name`、`version`、`description` 等）或等价 JSON 清单
   - 引擎解析为内部 `SkillSpec`（名称唯一、版本可比较、正文可注入）

2. **发现机制**
   - 启动时扫描可配置的 `SKILL_PATH`（可多目录、顺序有意义）
   - 可选运行时通过白名单工具 `load_skill(name)` 再装载（若开放，则必须配合 allowlist 与审计）

3. **注入机制**
   - 将 skill 正文以 system 或 developer 消息片段挂到指定 Agent（根 Agent / 后续 spawn 子 Agent）
   - 与 MCP Prompts 的合并策略在引擎层显式定义（例如后加载覆盖或 priority 字段）

4. **与子 Agent 的衔接（最小）**
   - 为后续 spawn 预留参数形状（如 `skill_ids`）
   - 本版本至少保证静态 Network / 单 Agent 路径上 skill 列表可配置
   - 若 spawn 尚未落地，则在文档中标注顺延项

5. **安全与观测**
   - Skill 仅允许来自受信根目录或签名清单
   - 禁止未校验的任意路径
   - slog 记录「谁、何时、加载哪一版 skill」
   - OpenTelemetry 侧为 Skill 装载增加 span（与 §3.5 表格一致）

6. **文档与验收**
   - 新增 `doc/acceptance/v0.7.0-acceptance.md`
   - 更新 `doc/design/multi-agent-engine.md` §3.6 的实现状态指针与示例配置片段

#### P1（受控开启）

- 在策略允许时执行 `scripts/` 中的 shell 脚本
- 完整性 + 配额 + sandbox 校验

#### 后续增强

- 缓存与并发配额
- 失败回退策略

### 2.2 不在本版本范围

1. **多脚本运行时** — 本版本仅支持统一 shell 执行，Python/Node 等运行时留待后续版本
2. **动态技能市场** — 不支持从网络下载或第三方来源加载技能
3. **技能间依赖管理** — 不支持技能间的依赖声明与自动解析
4. **热重载** — 技能加载后不支持运行时热重载（需重启进程）

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够以标准格式编写技能包，包含元数据、正文和可选脚本  
**验收标准**：
- 创建 `SKILL.md` 文件，包含 front matter（name、version、description、allowed-tools、model、license）
- body 部分包含技能的使用说明和执行步骤
- 可选包含 `scripts/` 目录存放可执行脚本
- 可选包含 `references/` 目录存放参考资料
- 可选包含 `assets/` 目录存放模板/静态资源

**US-2**：能够将技能包放置在受信目录并被自动发现  
**验收标准**：
- 配置 `SKILL_PATH` 环境变量或配置文件
- 启动时自动扫描目录下所有 `SKILL.md` 文件
- 解析 front matter 并建立技能索引
- 支持多目录配置，按顺序加载

**US-3**：能够在运行时按需加载特定技能  
**验收标准**：
- 提供 `load_skill(name)` 工具（可选，受 allowlist 控制）
- 加载时验证技能来源和完整性
- 记录审计日志（谁、何时、加载哪一版）

**US-4**：能够控制技能的执行策略  
**验收标准**：
- 配置 allowlist 限制可执行的技能
- 配置 timeout 限制脚本执行时间
- 配置 sandbox 限制脚本权限
- 执行前校验策略，不符合则拒绝执行

### 3.2 作为 Agent，我希望...

**US-5**：能够在初始化时自动注入已选中的技能  
**验收标准**：
- 根 Agent 在首轮请求前将 skill 注入 system/developer
- 注入顺序与优先级可配置且有默认
- 后续 spawn 的子 Agent 可选择性继承技能列表

**US-6**：能够根据技能指示调用统一的 shell tool 执行脚本  
**验收标准**：
- skill.md 的 body 部分指示 Agent 调用哪个脚本
- Agent 调用统一的 `execute_shell_script` tool
- Tool 执行脚本并返回 stdout/stderr
- 执行结果通过 VarStore 或 Tool 调用返回

---

## 4. 功能需求

### 4.1 技能包结构

#### 4.1.1 目录约定

```
skill-name/
├── SKILL.md              # 必须：技能元数据 + 正文
├── scripts/              # 可选：可执行脚本
│   ├── setup.sh
│   └── cleanup.sh
├── references/           # 可选：参考资料
│   ├── api-doc.md
│   └── examples.md
└── assets/               # 可选：模板/静态资源
    └── template.md
```

#### 4.1.2 SKILL.md 格式

```markdown
---
name: skill-name
version: 1.0.0
description: 技能描述
allowed-tools: Read,Bash
model: default
license: MIT
---

## 使用说明

技能的详细使用说明。

## 执行步骤

1. 步骤 1
2. 步骤 2
3. 步骤 3
```

**Front matter 字段约束**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 技能唯一标识，全局唯一 |
| `version` | string | 是 | 语义化版本号（如 `1.0.0`） |
| `description` | string | 是 | 技能简短描述 |
| `allowed-tools` | string | 是 | 允许使用的工具列表（逗号分隔） |
| `model` | string | 否 | 推荐使用的模型（默认 `default`） |
| `license` | string | 否 | 许可证（如 `MIT`、`Apache-2.0`） |

### 4.2 技能加载流程

#### 4.2.1 两阶段加载 + 受控执行

```
┌─────────────────────────────────────────────────────────────┐
│ 阶段 A：预加载（启动时）                                      │
├─────────────────────────────────────────────────────────────┤
│ 1. 扫描 SKILL_PATH 目录                                      │
│ 2. 解析 SKILL.md front matter                               │
│ 3. 建立技能索引（SkillRegistry）                             │
│ 4. 用于技能选择与路由                                        │
└─────────────────────────────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────┐
│ 阶段 B：按需加载（选中后）                                    │
├─────────────────────────────────────────────────────────────┤
│ 1. 加载 skill Body                                           │
│ 2. 加载 references/（如有）                                  │
│ 3. 加载 assets/（如有）                                      │
│ 4. 生成 SkillSpec 并注入上下文                               │
└─────────────────────────────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────┐
│ 阶段 C：受控执行（策略允许时）                                │
├─────────────────────────────────────────────────────────────┤
│ 1. Agent 根据 skill 的 prompt 指示调用脚本                    │
│ 2. 调用统一的 shell tool（execute_shell_script）             │
│ 3. 执行前校验策略（allowlist、timeout、sandbox）             │
│ 4. 执行脚本并返回 stdout/stderr                              │
│ 5. 记录审计日志                                              │
└─────────────────────────────────────────────────────────────┘
```

#### 4.2.2 完整性校验

加载阶段（`LoadFromPaths`）的强制校验：

1. **结构校验**：必须存在 `SKILL.md` 且包含 front matter；禁止目录外路径引用
2. **路径校验**：拒绝 `..`、绝对路径、非法符号链接（symlink escaping）
3. **哈希校验**：若存在 `manifest`（如 `skill.manifest.json`），逐文件校验 SHA256 与 Size；不一致直接失败
4. **最小一致性**：`SkillFrontmatter.name/description` 必填且合法；若存在 `version`，则 `SkillFrontmatter.name/version` 与 manifest 的 `SkillName/SkillVersion` 必须一致
5. **可执行资产校验**：
   - 若 `scripts/` 目录存在，则必须包含至少一个可执行脚本（`.sh` 文件）
   - 执行前校验策略（allowlist、timeout、sandbox）
6. **审计输出**：记录 `ContentHash`、`BundleHash`、`VerifiedFiles` 到日志与 trace 字段

### 4.3 技能执行模型

#### 4.3.1 核心设计

- **skill.md 中不声明脚本语言类型** — 移除 `job_kind` 字段
- **所有 skill 脚本统一使用 shell 执行** — 通过统一的 shell tool 调用
- **执行逻辑由 prompt 指示** — skill.md 的 body 部分告诉 Agent 如何调用脚本

#### 4.3.2 执行流程

```
1. Agent 读取 skill.md 的 frontmatter 和 body
        ▼
2. body 中的 prompt 指示 Agent 调用哪个脚本（如 scripts/setup.sh）
        ▼
3. Agent 调用统一的 shell 执行 tool（如 execute_shell_script）
        ▼
4. Tool 执行脚本并返回 stdout/stderr
        ▼
5. 执行结果通过 Tool 调用、VarStore 或 slog/trace 返回
```

#### 4.3.3 示例 skill.md

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

### 4.4 安全策略

#### 4.4.1 来源控制

- Skill 仅允许来自**受信根目录**或**签名清单**
- 禁止未校验的任意路径
- 配置 `SKILL_PATH` 白名单

#### 4.4.2 执行控制

- **Allowlist**：配置允许执行的技能列表
- **Timeout**：配置脚本执行超时时间（默认 30s）
- **Sandbox**：配置脚本执行权限（如禁止网络访问、限制文件系统访问）
- **Feature Flag**：通过配置开关控制是否启用脚本执行

#### 4.4.3 审计日志

```go
// slog 审计日志字段
{
    "timestamp": "2026-04-17T10:00:00Z",
    "event": "skill_loaded",
    "skill_name": "code-review",
    "skill_version": "1.0.0",
    "source_path": "/path/to/skill",
    "content_hash": "sha256:...",
    "bundle_hash": "sha256:...",
    "verified_files": 5
}
```

---

## 5. 技术需求

### 5.1 数据结构

#### 5.1.1 SkillSpec

```go
type SkillSpec struct {
	Name         string
	Version      string
	Description  string
	AllowedTools []string
	Model        string
	License      string
	Body         string
	SourcePath   string
	ContentHash  string
	Extra        map[string]any
}
```

#### 5.1.2 SkillMeta（发现/列表视图）

```go
type SkillMeta struct {
	Name        string
	Version     string
	Description string
	SourcePath  string
}
```

#### 5.1.3 SkillJob（可执行作业）

```go
type SkillJob struct {
	WorkingDir string
	Entry      string   // script path, e.g., "scripts/setup.sh"
	Args       []string
	Context    map[string]any // 用户参数（VarStore 快照）
}
```

#### 5.1.4 SkillJobRunner（执行接口）

```go
type SkillJobRunner interface {
	Run(ctx context.Context, job SkillJob) (stdout string, err error)
}
```

### 5.2 接口设计

#### 5.2.1 SkillRegistry

```go
type SkillRegistry interface {
	LoadFromPaths(ctx context.Context, paths []string) error
	Get(name string) (*SkillSpec, error)
	List() []*SkillMeta
	LoadSkill(name string) (*SkillSpec, error) // 按需加载
}
```

#### 5.2.2 与 VarStore 的交互

```go
// SkillJob 执行时访问用户参数
type SkillJob struct {
	// ...
	Context map[string]any // VarStore snapshot
}

// 执行结果返回
type RuntimeOutcome struct {
	// ...
	VarStore map[string]any
}
```

### 5.3 观测性

#### 5.3.1 日志（slog）

- Skill 装载事件
- 技能执行事件
- 错误与告警

#### 5.3.2 Trace（OpenTelemetry）

- Skill 装载 span（与 Run span 关联）
- 技能执行 span
- 审计字段（`ContentHash`、`BundleHash`、`VerifiedFiles`）

---

## 6. 验收标准

### 6.1 功能验收

详见 `../acceptance/v0.7.0-acceptance.md`

### 6.2 性能验收

- 启动时技能扫描时间 < 1s（100 个技能以内）
- 按需加载时间 < 100ms
- 脚本执行超时可控（默认 30s）

### 6.3 安全验收

- 无法从非受信目录加载技能
- 无法执行未在 allowlist 中的技能
- 脚本执行受 sandbox 约束
- 所有加载和执行事件均有审计日志

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **安全面扩大** | 脚本执行可能引入任意代码执行风险 | 严格 sandbox、allowlist、审计日志 |
| **提示注入** | skill body 可能包含恶意提示 | 仅加载受信来源，执行前校验 |
| **版本冲突** | 多版本技能共存可能导致冲突 | 文档明确版本优先级，实现时告警 |
| **性能开销** | 大量技能加载可能影响启动时间 | 两阶段加载、缓存索引 |

---

## 8. 参考文档

- [progress-v0.7.0.md](../log/progress-v0.7.0.md) — 版本进度日志
- [multi-agent-engine.md](../design/multi-agent-engine.md#36-skills) — Skills 设计
- [v0.7.0-acceptance.md](../acceptance/v0.7.0-acceptance.md) — 验收标准
- [progress-v0.6.0.md](../log/progress-v0.6.0.md) — MCP 前置能力

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-17 | v0.7.0 | 初始版本 |

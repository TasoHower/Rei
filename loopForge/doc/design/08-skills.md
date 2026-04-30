# loopForge Skills（技能包）

> 本文档描述 loopForge 中 **Skill 的加载、注入与动态执行机制**。Skill 是独立于 Agent 的领域知识包，以 `SKILL.md` 文件形式存在，通过 system prompt 注入影响 LLM 行为，并支持随附脚本执行。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md) §4——RunLoop 如何在每次 LLM 调用前构建 system prompt，Skill 在此阶段注入。
>
> **关联讨论**：[07-transfer.md](07-transfer.md) §9——Transfer（Multi-Agent）vs Skill（Single-Agent）的架构取舍。

---

## 1. 概念

### 1.1 什么是 Skill

Skill 是一份受信的领域知识文档，由两部分组成：

```
---                           ← YAML 前置元数据（front matter）
name: professional-tone
description: 回复时使用正式礼貌的语气
version: "1.0"
---

正文内容：详细的指令、格式要求、注意事项、示例对话等
```

Skill 与 Transfer 的核心区别：

| 维度 | Skill | Transfer |
|------|-------|---------|
| 本质 | 文本指令注入到 LLM 上下文 | 切换 Agent 实例 |
| 影响范围 | 当前 Agent 的后续推理 | 接管整个对话 |
| 成本 | 零额外开销（只是 system prompt 多了几段文本） | 创建 Agent 实例 + 消息历史移交 |
| 灵活性 | 可动态加载（`load_skill` 工具） | 需预先注册 handoff 目标 |
| 代码执行 | 支持随附脚本（`execute_shell_script`） | Agent 各自独立 |

### 1.2 为什么需要 Skill

Skill 解决的是"教 LLM 新知识但不增加 Agent 个数"的问题：

- 同样的 Agent 实例 + 不同的 Skill 组合 → 不同的行为
- 新增能力只需新增一个 `SKILL.md` 文件，不需要修改 Go 代码
- 适合频繁调整的规则、格式、话术模板

---

## 2. SKILL.md 文件格式

### 2.1 Front Matter（前置元数据）

`pkg/skill/parse.go` 解析 YAML front matter（`---` 包裹的头部块）：

```go
type SkillFrontmatter struct {
	Name         string          // 技能名称（必填，正则 [a-zA-Z0-9_-]{1,64}）
	Description  string          // 技能描述（必填，最长 1024 字符）
	AllowedTools []string        // 声明式工具限制（目前存储但未强制执行）
	Model        string          // 建议模型名（可选）
	License      string          // 许可证（可选）
	Version      string          // 版本号（可选）
	Author       string          // 作者（可选）
	Tags         []string        // 标签（可选）
	Extra        map[string]any  // 扩展字段（不认识的键归入此处）
}
```

### 2.2 完整示例

```markdown
---
name: routing-guide
description: 帮助 triage Agent 判断应该将请求路由到哪个专家 Agent
version: "1.0"
tags: [triage, routing]
---

## Routing Rules

When classifying a user request, follow these priorities:

1. Math / Arithmetic / Calculation → route to `math_expert`
2. Writing / Creative / Content → route to `writer`
3. Variable updates / Preferences → route to `writer`

## Transfer Format

Always provide a concise reason when calling transfer_to_*. Example:

```
reason: "This problem requires step-by-step arithmetic calculation"
```
```

### 2.3 文件校验

`ParseSKILLFile` 会在加载时执行以下校验：

| 校验项 | 说明 |
|--------|------|
| UTF-8 | 文件必须是合法 UTF-8 |
| front matter | 必须包含 `---` 包裹的 YAML 头部 |
| name | 必填，匹配 `^[a-zA-Z0-9_-]{1,64}$` |
| description | 必填，最长 1024 字符 |
| allowed-tools | 可选，支持 YAML 数组或逗号分隔字符串 |
| hash | SHA-256 哈希，用于审计追踪 |

---

## 3. SkillRegistry：发现与加载

`pkg/skill/registry.go`：

```go
func NewRegistry() *SkillRegistry
func (r *SkillRegistry) LoadFromPaths(ctx context.Context, paths []string) error
func (r *SkillRegistry) Get(name string) (*SkillSpec, error)
func (r *SkillRegistry) List() []SkillMeta
func (r *SkillRegistry) Generation() uint64
```

### 3.1 加载流程

```
SkillRegistry.LoadFromPaths(ctx, paths)
  │
  ├── 遍历每个 path（abs → WalkDir 扫描 SKILL.md）
  │     │
  │     ├── 检查目录（非目录跳过）
  │     ├── 安全边界：只扫描 path 子树，不允许路径穿越
  │     │
  │     ├── 对每个 SKILL.md：
  │     │     ├── ParseSKILLFile(absPath)
  │     │     │     ├── os.ReadFile
  │     │     │     ├── SHA-256 哈希
  │     │     │     ├── splitFrontMatter（解析 YAML front matter）
  │     │     │     └── validateFrontmatter（name/description 校验）
  │     │     │
  │     │     ├── 重名检查：先加载者胜
  │     │     ├── OTel span：skill.parse（path/name/version/hash）
  │     │     └── slog.Info：skill loaded
  │     │
  │     └── 汇总 next[name] → SkillSpec
  │
  ├── atomic Snap：非阻塞快照切换（不影响进行中的 Run）
  │     r.snap.Store(&regSnapshot{byName: next, order: order})
  │
  └── r.gen.Add(1)    // generation 递增
```

关键特性：
- **并发安全**：`LoadFromPaths` 原子替换内部快照，进行中的 Run 仍使用旧快照
- **Generation 追踪**：每次加载递增 generation，调用方可判断是否需重新获取
- **路径安全**：`isUnderRoot` 防止 `SKILL.md` 引用根目录外的脚本

### 3.2 与 Runner 的接线

SkillRegistry 通过三种路径进入 Runner：

```go
// 路径 1：直接注入
runner.WithSkillRegistry(existingReg)

// 路径 2：惰性加载（首次 Run 时扫描路径）
runner.WithSkillPath("./skills")

// 路径 3：Agent 自带（构造时 WithSkills）
agent.WithSkills(reg, "routing-guide", "professional-tone")
```

---

## 4. Skill 注入到 Agent

### 4.1 静态绑定（Agent 构造时）

`pkg/agent/options.go`：

```go
func WithSkills(reg *skill.SkillRegistry, names ...string) Option
```

Agent 在 `RunLoop` 启动时调用 `resolveSkills()` 加载：

```go
// pkg/agent/skills.go
func (a *Agent) resolveSkills() ([]skill.SkillSpec, error) {
    for _, name := range a.SkillNames {
        sp, err := a.SkillRegistry.Get(name)
        out = append(out, *sp)
    }
    return out, nil
}
```

### 4.2 动态加载（LLM 运行时）

`load_skill` 工具允许 LLM 在运行时动态加载 Skill：

```go
// pkg/skill/load_tool.go
func LoadSkillTool(reg *SkillRegistry, appendLoaded func(SkillSpec)) *model.ToolInfo
```

LLM 调用 `load_skill(name="客服话术")` → 工具 Handle 从 registry 查找 → `appendLoaded` 回调把 SkillSpec 追加到 `LoopState.ExtraSkills` → 下一轮 LLM 调用时自动注入。

`load_skill` 在 Agent 配置中需显式启用：

```go
agent.WithLoadSkillTool(true)
```

### 4.3 System Prompt 注入位置

每步 LLM 调用的 system prompt 构建流程（`runLoopFullSystem`）：

```
baseSystem + MCP Prompts + resolvedSkills
       ↓
  [Variables] 块
       ↓
  {{key}} 模板替换
       ↓
  system prompt → emit(call_llm_start)
```

Skill 的注入格式由 `ComposeSystemSegments` 控制：

```go
// pkg/skill/compose.go
func ComposeSystemSegments(base, mcpFragment string, skills []SkillSpec) string {
    // base → MCP fragment → 每个 skill 的 "## Skill: {name}\n\n{body}"
}
```

最终注入后的 system prompt 结构示例：

```
[base system instructions]

[A useful skill...]

## Variables
...

## Skill: routing-guide

## Routing Rules
...
```

动态加载的 Skill 也一样注入——它们通过 `mergeSkillLists` 合并到 `resolvedSkills` 列表尾部。

---

## 5. Skill 随附脚本执行

Skill bundle 可以包含 `scripts/` 目录中的 shell 脚本。

### 5.1 ShellTool

```go
// pkg/skill/shell_tool.go
func ShellTool(reg *SkillRegistry, runner SkillJobRunner) *model.ToolInfo
```

`execute_shell_script` 工具参数：

```json
{
    "skill": "demo",          // 技能名
    "script": "scripts/setup.sh",  // 脚本相对路径（必须位于 skill bundle 内）
    "args": ["--verbose"]     // 可选参数
}
```

脚本通过 `/bin/sh` 执行，默认超时 1 分钟：

```go
type ShellSkillJobRunner struct {
    Timeout time.Duration  // 0 = 默认 1 分钟
}
```

### 5.2 安全约束

- 脚本路径必须相对 bundle 根（`SkillSpec.BundleRoot()`）
- 禁止 `../` 穿越离开 bundle 目录
- 超时强制终止子进程

### 5.3 启用

```go
agent.WithSkillShellTool(true)
agent.WithSkillShellTimeout(30 * time.Second)  // 可选超时
```

---

## 6. 完整数据流

```
进程启动
  │
  ├── SkillRegistry.LoadFromPaths(ctx, paths)
  │     └── 扫描 SKILL.md → 解析 YAML → SHA-256 → 内存快照
  │
  ├── Agent 构造：
  │     ├── agent.WithSkills(reg, "skill-a", "skill-b")
  │     ├── agent.WithLoadSkillTool(true)          // 可选
  │     └── agent.WithSkillShellTool(true)         // 可选
  │
  ├── Runner.Run():
  │     ├── runner.WithSkillRegistry(reg)
  │     └── 或 runner.WithSkillPath(dirs)
  │
  └── RunLoop 每步 LLM 调用前：
        │
        ├── resolveSkills() → 静态 Skill 列表
        ├── mergeSkillLists(base, LoopState.ExtraSkills) → 含动态加载
        ├── system prompt 构建：
        │     ComposeSystemSegments(base, mcpFragment, mergedSkills)
        │       → "## Skill: {name}\n\n{body}" 逐条注入
        │     + [Variables] 块 + {{key}} 替换
        │
        └── emit(call_llm_start, SystemPrompt=...)
```

---

## 7. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/skill/registry.go` | SkillRegistry：LoadFromPaths、Get、List、Generation、并发快照切换 |
| `pkg/skill/parse.go` | ParseSKILLFile：YAML front matter 解析、SHA-256 哈希、frontmatter 校验 |
| `pkg/skill/spec.go` | SkillSpec/SkillMeta/SkillFrontmatter 类型定义 |
| `pkg/skill/compose.go` | ComposeSystemSegments：base + MCP + skills 拼接 |
| `pkg/skill/load_tool.go` | LoadSkillTool：load_skill 工具定义 |
| `pkg/skill/shell_tool.go` | ShellTool：execute_shell_script 工具定义 |
| `pkg/skill/job.go` | SkillJob/SkillJobRunner/ShellSkillJobRunner |
| `pkg/skill/validate.go` | ValidateSkillBindings |
| `pkg/agent/skills.go` | resolveSkills、mergeSkillLists |
| `pkg/agent/loop.go` | runLoopFullSystem（技能注入点） |
| `pkg/agent/options.go` | WithSkills、WithSkillShellTool、WithLoadSkillTool |
| `pkg/runner/skills.go` | WithSkillRegistry、WithSkillPath、prepareAgent |

---

## 8. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) §4 | RunLoop 的 system prompt 构建——Skill 注入入口 |
| [07-transfer.md](07-transfer.md) §9 | Transfer vs Skill 架构取舍讨论 |
| [02-runner-core.md](02-runner-core.md) §5 | Runner 的引擎注入（含 SkillRegistry） |
| [04-tools.md](04-tools.md) §2.3 | `execute_shell_script`、`load_skill` 作为注入工具 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：Skill 概念、SKILL.md 格式与校验、SkillRegistry 加载机制、静态/动态注入、system prompt 拼接、shell 脚本执行。 |

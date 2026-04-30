# loopForge System Prompt 自定义注入

> 本文档描述 Agent 的 **SystemPromptBuilder** 函数钩子——调用方如何通过自定义函数完全接管 system prompt 的组装逻辑，以及默认路径下 Skill 和 Variable 的注入时序。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md) §4——RunLoop 每步调用 `runLoopFullSystem` 的时机；[08-skills.md](08-skills.md)——Skill 的内容格式与注入机制；[06-Variable.md](06-Variable.md)——`[Variables]` 块与 `{{key}}` 替换。

---

## 1. 背景

### 1.1 为什么开放自定义入口

loopForge 的默认路径下，system prompt 按固定顺序拼接：

```
Agent.SystemInstructions + Skills 正文 + [Variables] 块 + {{key}} 替换
```

这种"引擎吞下所有上下文，按固定顺序吐出一段文本"的策略在起步阶段够用——调用方写一段 system prompt，挂几个 Skill，Agent 自动把四块拼好。但在真实工程场景中，这套固定管道很快成为瓶颈。

**问题首先出在结构上。** 引擎不知道调用方的 prompt 设计意图。默认拼接只能"全部放在 base system 后面"——但如果调用方的意图是"Skill 应该出现在 base system 的中间某处，而非末尾"，引擎做不到。只有调用方自己知道 prompt 的正确结构。

**问题也出在策略上。** Prompt Engineering 是 LLM 应用的核心竞争力，不应该被框架锁定。不同团队对 prompt 的设计理念完全不同：角色扮演式（`你是一个xxx专家`）、指令式（`请按以下步骤...`）、few-shot 式（`示例1: ...`）。如果 loopForge 强制一种拼接模式，等于在这个核心能力上替调用方做了不可推翻的决定。

**同样是策略问题，还有动态性。** "第一步详细说明工具用法，后续步骤只给简短提示"是有效的 prompt 优化手段；"根据用户语种切换 system prompt 语言"、"对话超过 10 轮时注入'请尽快结束'提示"也是。这些策略不复杂，但引擎内部的固定拼接做不到。

**模型差异进一步加剧了这一矛盾。** 同一段 system prompt 对不同模型的效果不同——DeepSeek 下简洁有效、GPT-4 下啰唆冗长、Claude 下可能过于生硬。不同模型有不同的指令遵循偏好、不同的 token 计费模式、不同的上下文窗口长度。如果 prompt 必须经过引擎的固定管道，调用方就无法在运行时根据 `req.Options.Model` 做模型适配。这种逻辑的代码量很少，但引擎做不到。

**规模增长带来了终极问题。** 一个 Agent 可能挂 10 个 Skill，VarStore 可能有 20 个变量。全部无差别注入到 system prompt 尾部，prompt 越来越长，LLM 的实际遵循度反而下降。调用方需要"选择性注入"——不是所有 Skill 都适合在每个 step 出现，不是所有变量都应该写进 prompt。

**还有一个比"拼 prompt"更根本的能力。** `SystemPromptBuilder` 归根到底就是一个普通的 Go 函数 `func(SystemPromptBuildContext) (string, error)`——它的能力边界是整个 Go 运行时。调用方可以在 builder 中查询数据库获取用户偏好、检查 Redis 缓存避免重复计算、触发 webhook 做审计记录。引擎对此不做任何限制，因为引擎的职责是调用 builder 并信任其返回的 prompt，而不是决定 prompt 怎么生成。

---

loopForge 的默认 builder 把这些能力全压在了一个固定拼接管道里——它保证了最基础的行为：把 system instructions、Skill 和变量稳定地拼成一段可用的 prompt，让 Agent 能够立即跑起来。但**我们相信 SDK 的使用方能够结合自己的领域知识、模型特性和对话场景，做出更好的上下文工程作品**。默认 builder 是脚手架，自定义 builder 是装修——前者保证结构安全，后者决定住着是否舒服。

**SystemPromptBuilder 的定位**：它不是在"facilitate" prompt 构建——它是在**交出控制权**。引擎说："我帮你管好 LLM 调用、工具执行、事件流、变量存储。但 prompt 怎么写，你自己决定。"

这就是为什么 `SystemPromptBuilder` 接收的是一个完整的 `SystemPromptBuildContext`（含 baseSystem、skills、varStore、step、agent 自身），而不是只给一个 string 参数——调用方拥有全量上下文，自己做决策。

### 1.2 执行时机：每次 LLM 调用都重新构建

一个常见的疑问是：builder 是 Run 开始时调用一次，还是每次 LLM 调用都重新执行？

**答案是每次 LLM 调用都重新构建。** 直接看 `pkg/agent/loop.go` 的迭代循环：

```go
// loop.go L162-L190
for step := range maxSteps {
    // ...
    mergedSkills := mergeSkillLists(resolvedSkills, state)   // ← 每步合并动态 Skill
    fullSystem, err := a.runLoopFullSystem(ctx, req, vstore, baseSystem, step, runID, mergedSkills)  // ← 每步调用 builder
    // ...
    emit(step, &event.CallLLMStartPayload{
        SystemPrompt: fullSystem,   // ← 每步使用最新 prompt
        // ...
    })
}
```

`runLoopFullSystem`（含 `SystemPromptBuilder` 调用）位于 `for step := range maxSteps` **循环体内**，与 `emit(call_llm_start)` 在同一轮迭代中。因此 Agent 的 Step 0、Step 1、Step 2... 每次 LLM 调用都会触发一次完整的 builder 执行。

这个设计不是性能疏忽，而是刻意为之。原因是 **builder 的入参在每步都可能变化**：

| 入参 | 变化频率 | 典型场景 |
|------|---------|---------|
| `ctx.Step` | 每步递增 | Step 0 详细指导工具用法，Step 1+ 简洁提示 |
| `ctx.ResolvedSkills` | LLM 调用 `load_skill` 后变化 | 中途动态加载新 Skill，下轮立即注入 |
| `ctx.VarStore` | LLM 调用 `var_set` 后变化 | 用户偏好被写入，下轮 prompt 作出调整 |
| `ctx.VariablePromptBlock` | 同上 | `[Variables]` 块在 builder 返回后被引擎拼接，但 builder 可提前读取做决策 |

如果 builder 只运行一次，上面这些"上下文驱动的动态 prompt"就全部无法实现。User 每次调用都重新构建的代价微不足道（一次 Go 函数调用 + 字符串拼接，相比 LLM API 调用的数百毫秒延迟），但获得的能力是 prompt 可以随对话实时演进。

---

## 2. SystemPromptBuildContext

`pkg/agent/user_message.go`：

```go
type SystemPromptBuildContext struct {
	Ctx      context.Context           // 当前请求的 context
	Request  *request.RuntimeRequest   // 用户请求
	VarStore *variable.VarStore        // 当前运行时的变量存储
	Agent    *Agent                    // 当前 Agent 实例（可读取其 SystemInstructions 等字段）

	Step    int                        // 当前 LLM 调用轮次（从 0 开始）
	BaseSystemPrompt string            // Agent.SystemInstructions（原始值，未经任何处理）
	MCPPromptFragment string          // MCP Prompts 片段（预留，当前为空）
	ResolvedSkills []skill.SkillSpec   // 本轮的 Skill 列表（含静态绑定 + 动态加载）
	VariablePromptBlock string         // VarStore.PromptBlock() 的快照
}
```

调用方的 builder 函数可以从这个 context 中读取任何信息，自由决定 system prompt 的内容、顺序、格式。

### 2.1 注册

```go
ag := agent.New(chat,
	agent.WithSystemInstructions("You are a general-purpose assistant."),
	agent.WithSystemPromptBuilder(func(ctx agent.SystemPromptBuildContext) (string, error) {
		// ctx.BaseSystemPrompt  = "You are a general-purpose assistant."
		// ctx.ResolvedSkills    = []skill.SkillSpec{{Name: "routing-guide", Body: "..."}, ...}
		// ctx.VarStore          = 当前变量存储
		// ctx.Step              = 当前步数
		// ctx.VariablePromptBlock = [Variables] 文本块

		return myCustomPromptComposer(ctx), nil
	}),
)
```

---

## 3. runLoopFullSystem：两条件分支

`pkg/agent/loop.go` L289-L349：

```
runLoopFullSystem(ctx, req, vstore, baseSystem, step, runID, resolvedSkills)
  │
  ├── 1. 获取 VariablePromptBlock（若启用 Variable）
  │
  ├── 2. 判断 SystemPromptBuilder 是否设置：
  │     │
  │     ├── 设置了（自定义路径）：
  │     │      a.SystemPromptBuilder(SystemPromptBuildContext{...})
  │     │      → 返回调用方自定义的 system prompt 文本
  │     │
  │     └── 未设置（默认路径）：
  │            skill.ComposeSystemSegments(baseSystem, "", resolvedSkills)
  │            → 按顺序拼接：base + MCP（留空）+ 每个 Skill 的 "## Skill: {name}\n\n{body}"
  │
  ├── 3. 追加 [Variables] 块（若启用 Variable）：
  │       full = full + "\n\n" + block
  │
  └── 4. 应用 {{key}} 模板替换：
          full = ReplaceDoubleBraceParams(full, stringParamsFromVarStore(vstore))
```

关键点：

- **变量块和 `{{key}}` 替换始终在 builder 之后执行**，与选择自定义路径还是默认路径无关
- 自定义路径中，调用方**不负责**拼接 [Variables] 块和做 `{{key}}` 替换——引擎保证这两步一定会执行
- 调用方如果**不需要** Skill 内容出现在 system prompt 中，在 builder 中不引用 `ctx.ResolvedSkills` 即可

### 3.1 默认路径的拼接逻辑

`pkg/skill/compose.go`：

```go
func ComposeSystemSegments(base string, mcpFragment string, skills []SkillSpec) string {
    // 顺序：base → MCP（当前为空）→ ## Skill: {name}\n\n{body}
}
```

最终形如：

```
[Agent.SystemInstructions]

## Skill: routing-guide

## Routing Rules
...

## Skill: professional-tone

请使用正式礼貌的语气回复。

[Variables 块]
session_note = "偏好简洁回答"

{{key}} 替换后
session_note 的实际值已替换到 {{session_note}} 位置
```

---

## 4. 常用自定义场景

### 4.1 根据 Step 切换提示词

Agent 的第一步可能需要详细的工具使用说明，后续步只需简短提示：

```go
agent.WithSystemPromptBuilder(func(ctx agent.SystemPromptBuildContext) (string, error) {
	if ctx.Step == 0 {
		return ctx.BaseSystemPrompt + "\n\n" +
			"You have access to the following tools. Think step by step and call them as needed.\n\n" +
			joinSkills(ctx.ResolvedSkills), nil
	}
	return ctx.BaseSystemPrompt + "\n\nContinue your work. Do not repeat instructions.", nil
})
```

### 4.2 在提示词中间插入 Skill

默认路径 Skill 在末尾，但可能需要在 base system 的特定位置插入：

```go
agent.WithSystemPromptBuilder(func(ctx agent.SystemPromptBuildContext) (string, error) {
	prompt := ctx.BaseSystemPrompt

	// 在 "## Tools" 标记后插入 Skill
	if idx := strings.Index(prompt, "## Tools"); idx >= 0 {
		prompt = prompt[:idx+10] + "\n\n" + joinSkills(ctx.ResolvedSkills) + "\n\n" + prompt[idx+10:]
	}
	return prompt, nil
})
```

### 4.3 动态根据变量调整提示词

当 VarStore 中的变量影响提示词风格时：

```go
agent.WithVariable()
agent.WithSystemPromptBuilder(func(ctx agent.SystemPromptBuildContext) (string, error) {
	tone, _ := ctx.VarStore.Get("session_note")  // 如 "偏好简洁回答"

	if toneStr, ok := tone.(string); ok && strings.Contains(toneStr, "简洁") {
		return ctx.BaseSystemPrompt + "\n\nUse short, direct answers. One sentence only.", nil
	}
	return ctx.BaseSystemPrompt + "\n\nProvide detailed, step-by-step answers.", nil
})
```

---

## 5. 与 Skill 和 Variable 的关系

| 组件 | SystemPromptBuilder 内的行为 |
|------|---------------------------|
| **Skill** | 通过 `ctx.ResolvedSkills` 访问完整列表（含静态+动态加载）。builder 自行决定是否、如何、在哪里注入 |
| **`[Variables]` 块** | 在 builder 返回**之后**由引擎自动追加，builder 不需要拼接 |
| **`{{key}}` 替换** | 在 builder 返回**之后**由引擎自动执行，builder 可以放心写 `{{key}}` 占位符 |
| **`ctx.VariablePromptBlock`** | builder 可以读取此时的变量块快照来决定提示词策略，但不需要手动拼回来 |

---

## 6. 完整数据流

```
RunLoop 每步 LLM 调用前：
  │
  ├── 1. baseSystem = a.SystemInstructions
  │
  ├── 2. runLoopFullSystem(ctx, req, vstore, baseSystem, step, runID, resolvedSkills)
  │     │
  │     ├── ctx = SystemPromptBuildContext{
  │     │         BaseSystemPrompt, ResolvedSkills, VarStore,
  │     │         VariablePromptBlock, Step, Agent
  │     │       }
  │     │
  │     ├── if a.SystemPromptBuilder != nil:
  │     │       full = a.SystemPromptBuilder(ctx)    ← 调用方自定义
  │     │   else:
  │     │       full = ComposeSystemSegments(base, "", skills)  ← 默认拼接
  │     │
  │     ├── full += "\n\n" + VarStore.PromptBlock()    ← 引擎保证
  │     └── full = ReplaceDoubleBraceParams(full, ...) ← 引擎保证
  │
  ├── 3. msgs = replaceSystemMessage(msgs, full)
  │
  └── 4. emit(call_llm_start, SystemPrompt=full)
```

---

## 7. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/agent/user_message.go` | SystemPromptBuildContext、SystemPromptBuilder 类型定义 |
| `pkg/agent/loop.go` | runLoopFullSystem（两分支逻辑 + 变量块 + {{key}} 替换） |
| `pkg/agent/options.go` | WithSystemPromptBuilder（注册自定义 builder） |
| `pkg/skill/compose.go` | ComposeSystemSegments（默认拼接路径） |

---

## 8. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) §4 | RunLoop 每步调用 runLoopFullSystem 的上下文 |
| [08-skills.md](08-skills.md) | Skill 内容格式与注册——builder 通过 ctx.ResolvedSkills 访问 |
| [06-Variable.md](06-Variable.md) | `[Variables]` 块与 `{{key}}` 替换——在 builder 返回后自动执行 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：SystemPromptBuilder 钩子机制、两条件分支、三种自定义场景示例、与 Skill/Variable 的关系。 |

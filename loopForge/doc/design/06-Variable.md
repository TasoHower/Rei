# loopForge 变量系统

> 本文档描述 loopForge 中**会话级变量的存储、注入与操作机制**。变量提供跨 Agent hop（transfer 模式）和跨轮次的状态持久化能力，通过 `var_set` 工具暴露给 LLM 写入，通过 `{{key}}` 占位符和 `[Variables]` 提示块注入到 LLM 上下文中。
>
> **前置阅读**：[01-agent-core.md](01-agent-core.md) §4——RunLoop 如何在每次 LLM 调用前构建 system prompt；[04-tools.md](04-tools.md)——`var_set` 作为注入工具（不参与 ValidateBindings）。

---

## 1. 概念

### 1.1 什么是 Variable

Variable 是会话级共享状态。其核心特性：

| 特性 | 说明 |
|------|------|
| **会话级生命周期** | 变量在一次会话中跨轮次持久，但不在进程重启后自动保留 |
| **跨 Agent 共享** | transfer 模式下，多个 Agent 读写同一份变量（通过共享 `VarStore`） |
| **LLM 可读写** | LLM 通过 `var_set` 工具写入变量，通过 system prompt 中的 `[Variables]` 块读取当前值 |
| **Manifest 驱动** | 可选预定义变量清单（名称、描述、默认值），在 `Materialize` 阶段合并到 `VarStore` |

### 1.2 与普通 Tool 的区别

| 维度 | 普通 Tool | `var_set` |
|------|----------|-----------|
| 执行方式 | 一次调用返回一次结果 | 写入 `VarStore`，所有后续 LLM 调用和 Agent 可见 |
| 返回 | `resultJSON` | `"ok"` |
| 状态持久 | 不持久 | 会话内持久 |
| 验证 | 参与 `ValidateBindings` | 不参与（extraRuntime 注入工具） |

---

## 2. VarStore 核心类型

`pkg/variable/store.go`：

```go
type VarEntry struct {
	Value       any    // 变量值（nil 表示"已定义但未赋值"）
	Description string // 变量含义描述（注入到 [Variables] 提示块中）
	Visitable   bool   // 是否对 LLM 可见（注入到 [Variables] 块中）
}

type VarStore struct {
	mu   sync.RWMutex
	vars map[string]*VarEntry  // 线程安全：所有读写通过 RWMutex 保护
}
```

关键方法：

| 方法 | 用途 |
|------|------|
| `Set(key, value)` | 写入值（不存在则自动创建） |
| `AgentSet(key, value)` | 写入值，但**拒绝 `const_` 前缀**的只读键 |
| `Get(key)` | 读取值 |
| `Define(key, desc)` | 声明变量（Value=nil，前端显示为 `<unset>`） |
| `Delete(key)` | 删除变量 |
| `PromptBlock()` | 生成用于注入 system prompt 的 `[Variables]` 文本块 |
| `Snapshot()` / `Import()` | 序列化/反序列化（用于跨请求恢复） |

### 2.1 const_ 只读键

`pkg/variable/const.go`：

```go
const ConstPrefix = "const_"

func IsConst(key string) bool {
	return strings.HasPrefix(key, ConstPrefix)
}
```

以 `const_` 开头的变量对 LLM 的 `var_set` 工具**只读**——`AgentSet` 会直接拒绝并返回错误。这些键由应用层在 `Materialize` 或 runtime 绑定时设置，供提示词说明「只读上下文」（如 `const_session_id`、`const_user_name`）。

### 2.2 PromptBlock 示例

当 VarStore 中有 `session_note`("track user preferences") 和 `user_goal`("what the user wants to achieve") 时，`PromptBlock()` 生成：

```
## Shared Variables
The following variables are shared across agents in this session.
They persist through agent transfers and reflect the current state.

### How to use
- Variables marked <unset> have been declared but not yet assigned.
- Variables prefixed with const_ are read-only — they are injected by the system.
- All other variables can be updated at any time using var_set.
- You do not need to call any tool to read variables — their current values are shown below.

### Current values
session_note = "用户偏好简洁回答"  # track user preferences
user_goal = "想了解天气"  # what the user wants to achieve
```

---

## 3. Manifest：静态变量清单

`pkg/variable/manifest.go`：

```go
type Spec struct {
	Key         string  // 变量名
	Description string  // 变量含义
	Visitable   *bool   // nil = true（默认对 LLM 可见）
	Default     any     // 默认值（nil = 声明为未设置）
}

type Manifest struct {
	Specs []Spec
}

func Materialize(m *Manifest, persisted *StoreSnapshot, runtime map[string]any, opts ...MaterializeOption) *VarStore
```

Manifest 是变量的静态声明。`Materialize` 是变量初始化的标准入口，按三层合并：

```
Materialize(manifest, persisted, runtimeBindings)
  │
  ├── 1. 导入持久化快照（persisted）→ base VarStore
  │
  ├── 2. 应用 Manifest.Specs：
  │       - 若 key 不存在于 base → 用 Default 初始化；无 Default → Define
  │       - 若 key 已存在 → 保留现有值，补全 Description/Visitable 元数据
  │
  ├── 3. 应用 runtime 绑定：Set(key, value)
  │
  └── 4. 若 WithDropOrphans(true)：删除不在 Manifest 且不在 runtime 中的键
```

### 3.1 使用示例

```go
m := &variable.Manifest{
	Specs: []variable.Spec{
		{Key: "session_note", Description: "track user preferences"},
		{Key: "user_goal", Description: "what the user wants to achieve"},
	},
}

// 跨请求恢复
store := variable.Materialize(m, prevSnapshot, map[string]any{
	"const_session_id": "sess-123",  // 只读键
})

// 传给 Runner
r := runner.NewRunner(entry, runner.WithVarStore(store))
```

---

## 4. var_set 工具

`pkg/variable/tools.go`：

```go
func VarSetTool(store *VarStore) *model.ToolInfo
```

`var_set` 工具的定义：

| 字段 | 值 |
|------|-----|
| Name | `var_set` |
| 推荐用法 | `{"updates": {"key1": value1, "key2": value2}}`——一次调用修改多个变量 |
| 兼容用法 | `{"key": "session_note", "value": "..."}`——旧版单字段形式 |

### 4.1 工具注册

`var_set` 不通过 `WithToolInfos` 注册，而是在 `RunLoop` 启动时作为 `varTools`（extraRuntime）注入：

```go
// loop.go L91-L92
if a.Variable {
    varTools = append(varTools, variable.VarSetTool(vstore))
}
```

这确保了 `var_set` 不参与 `ValidateBindings`——它由引擎保证必有 Handle。

### 4.2 执行流程

```
LLM 调用 var_set({"updates": {"session_note": "..."}})
  │
  ├── tool.Invoke → info.Handle(ctx, argsJSON)
  │     │
  │     ├── 解析 updates/key+value
  │     ├── 对每个 key：
  │     │     ├── 检查 IsConst(key) → 拒绝
  │     │     └── store.AgentSet(key, value)
  │     └── 返回 "ok"
  │
  └── role=tool 消息回注到历史 → LLM 下一次调用看到更新后的 [Variables] 块
```

---

## 5. {{key}} 占位符替换

`RunLoop` 在每步构建 system prompt 后，调用 `ReplaceDoubleBraceParams` 将 `{{key}}` 占位符替换为变量值：

```go
// loop.go L345-L347
if p := stringParamsFromVarStore(vstore); len(p) > 0 {
    full = ReplaceDoubleBraceParams(full, p)
}
```

替换规则：
- `{{key}}` → 如果 `key` 有值 → 替换为字符串化表示
- `{{key}}` → 如果 `key` 为 `<unset>` → 替换为约定占位
- 只有 `Visitable=true` 的变量参与替换

典型使用场景：在 system prompt 中写 `"用户偏好：{{session_note}}"`，RunLoop 在调用 LLM 前自动将 `{{session_note}}` 替换为实际值。

---

## 6. 在 Agent RunLoop 中的注入流程

```
RunLoop 每步 LLM 调用前：
  │
  ├── 1. runLoopFullSystem(ctx, req, vstore, baseSystem, step, runID, skills)
  │       │
  │       ├── SystemPromptBuilder（若设置）→ 自定义组装
  │       └── 默认：baseSystem + skills
  │
  ├── 2. 追加 VarStore.PromptBlock()
  │       └── full = full + "\n\n" + [Variables]
  │
  ├── 3. ReplaceDoubleBraceParams(full, stringParamsFromVarStore(vstore))
  │       └── 将 {{key}} 替换为实际值
  │
  └── 4. 替换消息中的 system message → emit(call_llm_start)
```

### 6.1 启用条件

只有当 `Agent.WithVariable()` 被调用（`Variable = true`）时：

- `var_set` 工具被注入
- `[Variables]` 块被拼入 system prompt
- `{{key}}` 占位符替换生效

若 `Variable = false`，以上全部跳过，变量系统不参与执行。

---

## 7. 跨 Agent Transfer 共享

Runner 在 transfer 模式下通过 `WithVarStore` 注入共享存储：

```go
store := variable.New()

r := runner.NewRunner(entryAgent,
    runner.WithVarStore(store),  // 所有 Agent hop 共享同一份变量
)
```

Runner 在 `runTransferLoop` 中将 `store` 注入每个 Agent hop 的 `LoopState.VarStore`，实现跨 Agent 变量共享：

```
[Agent A] var_set session_note="偏好"
   → store.session_note = "偏好"

[Agent A → B transfer]

[Agent B] 在 system prompt 中看到:
   [Variables]
   session_note = "偏好"
```

---

## 8. 持久化与快照

### 8.1 StoreSnapshot

```go
type StoreSnapshot struct {
    Vars []VarSnapshot `json:"vars"`
}

type VarSnapshot struct {
    Key         string `json:"key"`
    Value       any    `json:"value"`
    Description string `json:"description"`
    Visitable   bool   `json:"visitable"`
}
```

### 8.2 序列化路径

`VarStore` 实现 `json.Marshaler` / `json.Unmarshaler`，序列化时走 `Snapshot()`，反序列化时走 `Import()`：

```go
// 序列化
data, _ := json.Marshal(store)

// 反序列化（跨请求恢复）
var snap variable.StoreSnapshot
json.Unmarshal(data, &snap)
restored := variable.Import(&snap)
```

### 8.3 test-server 的持久化用法

test-server 按 `session_id` 缓存 `VarStore` 快照，每次请求结束时 `persistVarSnapshot(sessionID, lastVarStore)` 保存，下次请求时恢复。

---

## 9. 完整数据流

```
用户请求
  │
  ├── Runner.Run() 或直接 Agent.Run()
  │     └── VarStore 来源：
  │           ├── runner.varStore（WithVarStore 注入）
  │           ├── 或 variable.New()（独立新建）
  │
  ├── RunLoop 启动
  │     ├── Variable == true?
  │     │      ├── varTools += VarSetTool(vstore)
  │     │      └── system prompt 管线加入 [Variables] + {{key}} 替换
  │     └── Variable == false? → 跳过
  │
  ├── 每步 LLM 调用：
  │     ├── system prompt 含 [Variables] 块（运行时的变量值清单）
  │     └── LLM 看到变量后决定是否调 var_set
  │
  ├── LLM 调用 var_set：
  │     ├── AgentSet(key, value) → 写入 VarStore
  │     └── 返回 "ok" → role=tool 消息回注
  │
  ├── 下一轮 LLM 调用：
  │     └── [Variables] 块包含更新后的值
  │
  └── Run 结束：
        ├── QueryEndPayload.Outcome.VarStore 存有最终快照
        └── 调用方可 Snapshot() 持久化
```

---

## 10. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/variable/store.go` | VarStore、VarEntry、Set/Get/AgentSet/Define/Delete、PromptBlock、Snapshot/Import |
| `pkg/variable/manifest.go` | Spec、Manifest、Materialize |
| `pkg/variable/tools.go` | VarSetTool（var_set 工具定义） |
| `pkg/variable/const.go` | ConstPrefix、IsConst |
| `pkg/variable/context.go` | NewContext、FromContext |
| `pkg/variable/doc.go` | 包文档 |
| `pkg/agent/loop.go` | RunLoop 中变量注入：varTools 构造 + PromptBlock + {{key}} 替换 |
| `pkg/agent/user_message.go` | SystemPromptBuildContext、SystemPromptBuilder |
| `pkg/agent/options.go` | WithVariable |
| `pkg/runner/runner.go` | WithVarStore（跨 Agent 共享） |

---

## 11. 相关文档

| 文档 | 关系 |
|------|------|
| [01-agent-core.md](01-agent-core.md) §4 | RunLoop 的 system prompt 构建——变量注入的入口 |
| [02-runner-core.md](02-runner-core.md) §3.3 | Runner transfer 模式中的 VarStore 共享 |
| [04-tools.md](04-tools.md) §2.3 | var_set 作为注入工具（extraRuntime，不参与 ValidateBindings） |
| [00-abstractions.md](00-abstractions.md) §6 | VarStore 类型的代码级定义 |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：VarStore 核心类型、Manifest 静态清单、var_set 工具、{{key}} 占位符替换、system prompt 注入流程、跨 Agent 共享、持久化。 |

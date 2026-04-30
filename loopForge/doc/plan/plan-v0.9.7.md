# loopForge 实施计划 — v0.9.7（tools 自动化注册：通过 struct 类型自动生成 JSON Schema，消除手写 Parameters）

> **对应版本进度**：`doc/log/progress-v0.9.7.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的任务拆分与执行顺序；**先于编码**成文。  
> **主目标**：引入基于 Go struct 反射的 JSON Schema 自动生成机制，使工具注册不再需要手动编写 `Parameters map[string]interface{}`，同时 Handle 自动完成 JSON 反序列化，消除重复解析逻辑。

---

## 1. 背景

### 1.1 当前痛点

loopForge 中每个工具在注册时，必须手动编写 JSON Schema 参数定义和 JSON 解析逻辑。以现有代码为例：

**手写 Parameters（4 处）**：

| 文件 | 工具名 | Parameters 行数 |
|------|--------|----------------|
| `pkg/variable/tools.go` | `var_set` | 17 行 (L25-41) |
| `pkg/skill/shell_tool.go` | `execute_shell_script` | 21 行 (L29-49) |
| `pkg/skill/load_tool.go` | `load_skill` | 10 行 (L20-29) |
| `pkg/agent/spawn_tool.go` | `spawn_subagent` | 33 行 (L68-100) |

每处定义都遵循同一模式：

```go
Parameters: map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "field": map[string]interface{}{
            "type":        "string",
            "description": "...",
        },
    },
    "required": []string{"field"},
},
```

**Handle 中重复 JSON 解析（4 处）**：

| 文件 | 解析方式 |
|------|---------|
| `variable/tools.go` | `sonic.UnmarshalString(argumentsJSON, &envelope)` |
| `skill/shell_tool.go` | `json.Unmarshal([]byte(argumentsJSON), &raw)` |
| `skill/load_tool.go` | `json.Unmarshal([]byte(argumentsJSON), &payload)` |
| `agent/spawn_tool.go` | `sonic.UnmarshalString(argumentsJSON, &a)` |

**问题总结**：

| 问题 | 影响 |
|------|------|
| Parameters 手写、与 Go struct 类型脱节 | 修改 struct 字段时必须同步修改 Parameters 映射，易遗漏、难维护 |
| 无类型安全 | `Parameters` 是 `map[string]interface{}`，key/value 拼写错误在运行时才暴露 |
| Handle 重复 JSON 解析 | 每个工具都有一模一样的 `Unmarshal` 样板代码 |
| 新增工具成本高 | 写一个新工具至少需要：定义 struct → 手写 Parameters → 在 Handle 中 Unmarshal → 编写错误处理 |

### 1.2 v0.9.6 铺垫

v0.9.6 完成了 OpenAI SDK 兜底适配器的接入，当前工具执行链路已稳定。v0.9.7 在此基础上优化工具注册的 Developer Experience（DX），**不改变工具执行链路**。

### 1.4 兼容性分析：反射生成 Parameters vs 原始手动 Parameters

在实施重构前，必须逐工具分析反射生成的 Parameters 是否与原始手动定义**完全一致**。

#### 逐工具对比总表

| 工具 | 文件 | 差异数 | 详情 |
|------|------|:------:|------|
| `var_set` | `variable/tools.go` | 2 项 | `updates` 类型选择 + `required` 键省略 |
| `execute_shell_script` | `skill/shell_tool.go` | 2 项 | `args` 缺 `omitempty` + 缺 `description` tags |
| `load_skill` | `skill/load_tool.go` | 0 | ✅ 完全一致 |
| `spawn_subagent` | `agent/spawn_tool.go` | 6 项 | 4 字段缺 `omitempty` + `lifecycle` 暴露 + 缺 `description` tags |

#### 关键发现

**① `var_set` — `updates` 字段类型选择**

原始定义：
```go
"updates": map[string]interface{}{
    "type":        "object",
    "description": "Map from variable name...",
}
// 无 additionalProperties
```

如果 struct 用 `map[string]json.RawMessage`，反射生成：
```go
"updates": map[string]interface{}{
    "type":                 "object",
    "additionalProperties": map[string]interface{}{"type": "object"},
    "description":          "Map from variable name...",
}
```

增加 `additionalProperties` 在语义上更精确，但**与原始定义结构不同**。需要改用 `map[string]interface{}` 以保持无约束 object 的原始语义。

**② `required` 键的省略规则**

原始 Parameters 在 `all fields optional` 时省略 `"required"` 键。反射引擎也需要遵循此规则：
- required 数组为空时 → **不输出** `"required"` 键
- required 数组非空时 → 正常输出 `"required": [...]`

**③ `spawn_subagent` — `Lifecycle` 字段隐藏**

原始 Parameters **没有** `lifecycle` 参数。该字段是 Handle 内部使用的，不应暴露给 LLM。
需要用 `json:"-"` 隐藏它。但 `json:"-"` 会导致 `sonic.Unmarshal` 也不填充该字段——
好在 Handle 的逻辑是 `if a.Lifecycle == "" { Lifecycle = LifecycleEphemeral }`，因此空字符串与原行为一致。

#### 修复方案

| 修复项 | 涉及工具 | 具体操作 |
|--------|---------|---------|
| 类型调整 | `var_set` | `Updates` 用 `map[string]interface{}` 而非 `map[string]json.RawMessage` |
| omitempty 补齐 | `execute_shell_script`、`spawn_subagent` | 可选字段的 json tag 加 `,omitempty` |
| description 补齐 | `execute_shell_script`、`spawn_subagent` | 所有字段加 `description` tag |
| 字段隐藏 | `spawn_subagent` | `Lifecycle` 加 `json:"-"` |
| required 省略 | `SchemaFromStruct` 引擎 | required 为空时不输出该键 |

### 1.5 变更范围总结

| 类别 | 变更 | 操作 |
|------|------|------|
| **新增** | `pkg/tool/autoreg/` 包 (3 文件) | 新增——JSON Schema 生成 + 类型安全注册器 |
| **修改** | `pkg/variable/tools.go` | 重构——`var_set` 改用自动化注册 |
| **修改** | `pkg/skill/shell_tool.go` | 重构——`execute_shell_script` 改用自动化注册 |
| **修改** | `pkg/skill/load_tool.go` | 重构——`load_skill` 改用自动化注册 |
| **修改** | `pkg/agent/spawn_tool.go` | 重构——`spawn_subagent` 改用自动化注册（利用已有 `spawnSubagentArgs` 结构体） |
| **文档** | `doc/design/04-tools.md` | 修订——新增自动化注册章节，更新示例代码 |

### 1.4 不涉及的范围

- `pkg/tool/` 核心执行链路（`Invoke`、`ValidateBindings`、`Executor`）——不修改
- `pkg/model/types/` 核心类型（`ToolInfo`、`ToolCallHandler`、`ToolCallPart`）——不修改
- MCP 工具注册机制——MCP 工具通过 `tools/list` 由远端定义，不在本版本范围
- `pkg/agent/options.go`——`WithToolInfos` 的签名不变，调用方可继续混合使用新旧两种注册方式
- `test-server/`、`test-mcp/`——不涉及

---

## 2. 任务拆分与执行顺序

### 作业 #0 — 新增 `pkg/tool/autoreg/` 包（核心基础设施）

**目标**：创建自动化注册包，包含 3 个文件。

#### 2.1 `pkg/tool/autoreg/schema.go` —— JSON Schema 反射生成器

核心函数：

```go
package autoreg

// SchemaFromStruct generates a JSON Schema map from a Go struct type using reflection.
//   - T must be a struct.
//   - Field tags: json:"name,omitempty" for property name / optional, description:"..." for description.
//   - Only exported fields with json tags are included.
//   - Pointers are treated as optional (omitted from "required").
func SchemaFromStruct[T any]() map[string]interface{}
```

Go 类型 → JSON Schema 类型映射规则：

| Go 类型 | JSON Schema |
|---------|-------------|
| `string` | `{"type": "string"}` |
| `int`, `int8`, `int16`, `int32`, `int64` | `{"type": "integer"}` |
| `uint`, `uint8`, `uint16`, `uint32`, `uint64` | `{"type": "integer"}` |
| `float32`, `float64` | `{"type": "number"}` |
| `bool` | `{"type": "boolean"}` |
| `[]T` | `{"type": "array", "items": {T's schema}}` |
| `map[string]V` | `{"type": "object", "additionalProperties": {V's schema}}` |
| `*T`（指针） | T's schema，但不在 `required` 中 |
| struct（嵌套） | 递归生成 `{"type": "object", "properties": {...}}` |
| `json.RawMessage` | `{"type": "object"}`（无进一步约束） |

`required` 推导：
- 所有 `json` tag 中**不含 `omitempty`** 且非指针、非 `json.RawMessage` 的字段 → 加入 `required` 数组
- 指针字段（`*T`）、含 `omitempty` 的字段 → 可选，不加入 `required`

`description` 读取：
- 优先从 `description` struct tag 读取
- 无 `description` tag 时，该字段不写入 `"description"`

`json.RawMessage` 特殊处理：
- 由于 `json.RawMessage` 是 `[]byte` 的别名，反射 `Kind()` 为 `reflect.Slice`、`Elem` 为 `reflect.Uint8`
- 需要在反射检测中增加 `json.RawMessage` 的特别分支，直接输出 `{"type": "object"}` 而非分解为 `[]uint8`

`map[string]json.RawMessage` 特殊处理：
- `map[string]V` 反射规则会生成 `{"type": "object", "additionalProperties": {V's schema}}`
- 当 `V = json.RawMessage` 时，additionalProperties 会得到 `{"type": "object"}`
- 这比原始手动定义的 `{"type": "object"}`（无 additionalProperties）更精确，但结构不同
- **需要统一为 `{"type": "object"}`（无 additionalProperties）**，以保持与原始定义完全一致
- 在反射循环中，当检测到 `map[string]json.RawMessage` 时，跳过 additionalProperties

类型别名支持：
```go
type MyString string     // Kind() == reflect.String → "string"
type JSONMap map[string]any  // Kind() == reflect.Map → "object" + additionalProperties
```

`required` 输出策略：
- required 数组为空时（所有字段都是可选的），**不输出 `"required"` 键**
- 否则输出 `"required": [...]`
- 这与原始 `Parameters` 省略 `"required"` 时的语义一致（全字段可选）

`map[string]interface{}` 类型处理：
- 对于 `map[string]interface{}` 这种无约束的 map，反射生成 `{"type": "object"}`（无 additionalProperties）
- 这与原始手动定义的 `updates`（type: object，无 additionalProperties）一致

验证：
- 编写单元测试覆盖以下场景：
  - 全字段类型覆盖（string/int/float/bool/slice/map/struct/pointer）
  - `omitempty` 影响 required
  - `description` tag 写入
  - 嵌套结构体
  - `json.RawMessage` 分支
  - `map[string]json.RawMessage` 分支（无 additionalProperties）
  - 类型别名
  - empty required 时省略 `"required"` 键

#### 2.2 `pkg/tool/autoreg/handler.go` —— 类型安全注册器 + Parameters 废弃兼容

核心类型与函数：

```go
package autoreg

// ToolHandlerTyped is a typed tool handler that receives pre-parsed parameters.
type ToolHandlerTyped[T any] func(ctx context.Context, params T) (string, error)

// ToolOption configures the ToolInfo returned by NewToolFromStruct.
type ToolOption func(*model.ToolInfo)

// WithParameters overrides the auto-generated Parameters with a custom map.
//
// Deprecated: Parameters are now auto-generated from struct tags.
// Passing custom Parameters may conflict with the auto-wrapped Handle
// (the Handle unmarshals into struct T, but Parameters advertise different fields).
// This option is provided for migration only and will be removed in a future version.
func WithParameters(params map[string]interface{}) ToolOption {
    return func(info *model.ToolInfo) {
        slog.Warn("WithParameters is deprecated: "+
            "Parameters are auto-generated from struct tags. "+
            "Custom Parameters may conflict with the auto-wrapped Handle. "+
            "This option will be removed in a future version.")
        info.Parameters = params
    }
}

// NewToolFromStruct creates a *model.ToolInfo from a typed handler.
//   - name / description: tool name + description (same as ToolInfo fields).
//   - handler: typed handler that receives auto-parsed params T.
//   - opts: optional ToolOption values (e.g. WithParameters for migration).
//   - T must be a struct with json tags on exported fields.
//
// The returned ToolInfo has:
//   - Parameters auto-generated by SchemaFromStruct[T]()
//   - Handle auto-wrapping: unmarshal argumentsJSON → T → call handler
//
// If no ToolOption is provided, Parameters are auto-generated from struct tags.
// WithParameters overrides the auto-generated Parameters with a deprecation warning.
//
// Example (recommended — auto-generated Parameters):
//
//	    type AddParams struct {
//	        A float64 `json:"a" description:"First operand"`
//	        B float64 `json:"b" description:"Second operand"`
//	    }
//
//	    info := NewToolFromStruct("add", "Adds two numbers",
//	        func(ctx context.Context, p AddParams) (string, error) {
//	            return fmt.Sprintf("%f", p.A + p.B), nil
//	        },
//	    )
//
// Example (migration — custom Parameters, deprecated):
//
//	    info := NewToolFromStruct("add", "Adds two numbers",
//	        func(ctx context.Context, p AddParams) (string, error) {
//	            return fmt.Sprintf("%f", p.A + p.B), nil
//	        },
//	        WithParameters(legacyParams),
//	    )
func NewToolFromStruct[T any](name, description string, handler ToolHandlerTyped[T], opts ...ToolOption) *model.ToolInfo
```

自动包装逻辑：
1. 调用 `SchemaFromStruct[T]()` 生成 `Parameters`
2. 构造 Handle 闭包——自动将 `argumentsJSON` 反序列化为 `T` 后调用 typed handler：
   ```go
   Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
       var params T
       if err := sonic.UnmarshalString(argumentsJSON, &params); err != nil {
           return "", fmt.Errorf("parse %q arguments: %w", name, err)
       }
       return handler(ctx, params)
   }
   ```
3. 构建 `&model.ToolInfo{Name, Description, Parameters, Handle}`
4. 依次应用 `opts`（如 `WithParameters` 覆盖 Parameters 并打印废弃警告）

**⚠ Parameters 废弃策略**：

| 阶段 | 行为 | 版本 |
|------|------|------|
| **当前** | `WithParameters` 接受自定义 Parameters，触发 `slog.Warn` 废弃警告 | v0.9.7 |
| **下个大版本** | `WithParameters` 仍接受但警告升级为 `slog.Error`，强烈建议迁移 | v0.10.x |
| **未来** | `WithParameters` 移除，Parameters 仅由 struct 反射生成 | v1.0.0 |

**冲突说明**：
- `NewToolFromStruct` 自动生成的 Handle 将 `argumentsJSON` 按 struct `T` 的字段反序列化
- 如果用户通过 `WithParameters` 传入与 struct 字段不匹配的 JSON Schema，LLM 可能生成不符合预期的参数
- 例如：struct 定义 `Name string`，但自定义 Parameters 声明 `name` 为 `integer`——LLM 将传整型，Handle 反序列化会失败
- 因此强烈建议：**使用自动生成的 Parameters**，不要手动覆盖

#### 2.3 `pkg/tool/autoreg/doc.go` —— 包文档

```go
// Package autoreg provides reflection-based auto-generation of JSON Schema
// Parameters for ToolInfo, eliminating manual map[string]interface{} definitions.
//
// Usage (recommended):
//
//	type MyToolParams struct {
//	    Query string `json:"query" description:"Search query"`
//	    Limit int    `json:"limit,omitempty" description:"Max results"`
//	}
//
//	tool := autoreg.NewToolFromStruct("search", "Performs a search",
//	    func(ctx context.Context, p MyToolParams) (string, error) {
//	        return doSearch(ctx, p.Query, p.Limit)
//	    },
//	)
//
// Migration (deprecated — custom Parameters with warning):
//
//	tool := autoreg.NewToolFromStruct("search", "Performs a search",
//	    func(ctx context.Context, p MyToolParams) (string, error) {
//	        return doSearch(ctx, p.Query, p.Limit)
//	    },
//	    autoreg.WithParameters(legacyParams),  // logs deprecation warning
//	)
package autoreg
```

验证：
- `go vet ./pkg/tool/autoreg/...` 无报错
- 单元测试全部通过
- `WithParameters` 触发 `slog.Warn`（可通过 `slogtest` 验证日志输出）

---

### 作业 #1 — 重构 `var_set` 工具

**目标**：将 `variable/tools.go` 中的 `var_set` 工具改为使用 `autoreg.NewToolFromStruct`。

**⚠ 兼容性分析**：原始 Parameters 中 `updates` 字段定义为 `{"type": "object"}`（无 additionalProperties）。
直接使用 `map[string]json.RawMessage` 会生成 `{"type":"object","additionalProperties":{"type":"object"}}`。
使用 `map[string]interface{}` 会生成 `{"type": "object"}`（无 additionalProperties），与原始一致。
因此 `Updates` 类型应使用 `map[string]interface{}` 而非 `map[string]json.RawMessage`。

```go
type varSetParams struct {
    Updates map[string]interface{} `json:"updates,omitempty" description:"Map from variable name to JSON value. Include only variables you are changing; leave others out."`
    Key     string                 `json:"key,omitempty"    description:"Legacy: single variable name (use updates for multiple keys)."`
    Value   json.RawMessage        `json:"value,omitempty"  description:"Legacy: JSON object value for key (ignored when updates is non-empty)."`
}
```

`VarSetTool` 函数改为：

```go
func VarSetTool(store *VarStore) *model.ToolInfo {
    return autoreg.NewToolFromStruct(varSetToolName,
        "Set one or more shared variables in a single call. "+
            "Pass only keys you need to change inside `updates`; omit variables that stay the same. "+
            "Keys prefixed with const_ are read-only and cannot be set here. "+
            "Legacy single-field form `key` + `value` is still accepted.",
        func(ctx context.Context, p varSetParams) (string, error) {
            // ... 原 Handle 逻辑，但 p 已解析好
        },
    )
}
```

变更要点：
- 删除原手动 `Parameters` 定义（17 行）
- 删除 Handle 内的 `sonic.UnmarshalString` 解析（1 行）
- 提取 `varSetParams` 结构体（替代 Handle 内的匿名 struct）
- Handle 逻辑不变，但入参从 `argumentsJSON string` 变为 `p varSetParams`

验证：
- `go vet ./pkg/variable/...` 无报错
- `var_set` 工具行为不变（构造的 Parameters 与原始定义完全等价）

---

### 作业 #2 — 重构 `execute_shell_script` 工具

**目标**：将 `skill/shell_tool.go` 中的 `execute_shell_script` 工具改为使用自动化注册。

**⚠ 兼容性分析**：原始 `shellToolArgs` 的 `Args` 字段 tag 为 `json:"args"`（无 `omitempty`），
反射会将其标记为 required。但原始 Parameters 中 `args` **不在** required 中。
需要改为 `json:"args,omitempty"` 以匹配原始行为。
同时为所有字段添加 `description` tags。

```go
type shellToolArgs struct {
    Skill  string   `json:"skill"               description:"Logical skill name from the loaded skill registry."`
    Script string   `json:"script"              description:"Path to a .sh file relative to the skill bundle root (directory containing SKILL.md)."`
    Args   []string `json:"args,omitempty"       description:"Optional arguments passed to the script."`
}
```

`ShellTool` 函数改为：

```go
func ShellTool(reg *SkillRegistry, runner SkillJobRunner) *model.ToolInfo {
    if runner == nil {
        runner = &ShellSkillJobRunner{}
    }
    return autoreg.NewToolFromStruct(ShellToolName,
        "Runs a shell script path relative to the named skill bundle...",
        func(ctx context.Context, p shellToolArgs) (string, error) {
            // ... 原 Handle 逻辑，但 p 已解析好
        },
    )
}
```

变更要点：
- 删除原手动 `Parameters` 定义（21 行）
- 删除 Handle 内的 `json.Unmarshal` 解析（1 行）
- 为 `shellToolArgs` 补齐 `description` tags

验证：
- `go vet ./pkg/skill/...` 无报错
- 生成的 Parameters 与原始定义等价（注意 `args` 的 `type: array, items: {type: string}` 来自 `[]string`）

---

### 作业 #3 — 重构 `load_skill` 工具

**目标**：将 `skill/load_tool.go` 中的 `load_skill` 工具改为使用自动化注册。

新建参数结构体：

```go
type loadSkillParams struct {
    Name string `json:"name" description:"Logical skill name matching SKILL.md front matter."`
}
```

`LoadSkillTool` 函数改为：

```go
func LoadSkillTool(reg *SkillRegistry, appendLoaded func(SkillSpec)) *model.ToolInfo {
    return autoreg.NewToolFromStruct(LoadSkillToolName,
        "Loads a skill by logical name from the registry for the remainder of this run.",
        func(ctx context.Context, p loadSkillParams) (string, error) {
            // ... 原 Handle 逻辑
        },
    )
}
```

变更要点：
- 删除原手动 `Parameters` 定义（10 行）
- 删除 Handle 内的 `json.Unmarshal` 解析（1 行）
- 提取 `loadSkillParams` 结构体（替代 Handle 内的匿名 struct）

验证：
- `go vet ./pkg/skill/...` 无报错
- 生成的 Parameters 与原始定义等价

---

### 作业 #4 — 重构 `spawn_subagent` 工具

**目标**：将 `agent/spawn_tool.go` 中的 `spawn_subagent` 工具改为使用自动化注册。

**⚠ 兼容性分析**：原始 `spawnSubagentArgs` 结构体有 3 类兼容性问题：

| 问题 | 字段 | 原始 required | 当前 tag | 修复 |
|------|------|:------------:|:---------:|:----:|
| 缺少 omitempty | `SystemAddendum` | 不在 required（可选） | `json:"system_addendum"` | 加 `,omitempty` |
| 缺少 omitempty | `SkillIDs` | 不在 required（可选） | `json:"skill_ids"` | 加 `,omitempty` |
| 缺少 omitempty | `Model` | 不在 required（可选） | `json:"model"` | 加 `,omitempty` |
| 缺少 omitempty | `AllowChildSpawn` | 不在 required（可选） | `json:"allow_child_spawn"` | 加 `,omitempty` |
| 隐藏字段暴露 | `Lifecycle` | **不在原始 Parameters 中** | `json:"lifecycle"` | 加 `json:"-"`（Handle 内部使用，不应暴露给 LLM） |
| 缺 description | 全部字段 | — | 无 `description` tag | 添加所有字段的 description |

```go
type spawnSubagentArgs struct {
    Task            string   `json:"task"                           description:"User task for the child agent"`
    SystemAddendum  string   `json:"system_addendum,omitempty"      description:"Optional extra system instructions for the child"`
    SkillIDs        []string `json:"skill_ids,omitempty"            description:"Optional skill ids for the child"`
    Model           string   `json:"model,omitempty"                description:"Optional model override for the child"`
    MaxSteps        *int     `json:"max_steps,omitempty"            description:"Optional max ReAct steps for the child"`
    AllowChildSpawn bool     `json:"allow_child_spawn,omitempty"    description:"If true, the child may register spawn in its run (default false)"`
    Lifecycle       string   `json:"-"` // 内部使用，不暴露给 LLM（Handle 内读取，默认为 LifecycleEphemeral）
}
```

注意：`Lifecycle` 用 `json:"-"` 隐藏后，sonic.Unmarshal 不会填充该字段。
Handle 中读取 `a.Lifecycle` 将得到空字符串，触发 `else` 分支 `Lifecycle = LifecycleEphemeral`。
与原逻辑一致（原始 Parameters 也无 `lifecycle`，Handle 内通过 `a.Lifecycle == ""` 兜底）。

变更要点：
- 为 `spawnSubagentArgs` 每个字段调整 `json` tag（加 omitempty）和补充 `description` tag
- `Lifecycle` 改为 `json:"-"`（隐藏，不参与自动生成 Parameters，且保持 JSON 反序列化行为不变）
- 删除手动 `Parameters` 定义（33 行）
- 删除 Handle 内的 `sonic.UnmarshalString` 解析（1 行）
- 利用 `autoreg.NewToolFromStruct[spawnSubagentArgs]` 构建 ToolInfo

验证：
- `go vet ./pkg/agent/...` 无报错
- 生成的 Parameters 与原始定义逐字段等价（注意 `max_steps` 的 `*int` 指针 → 可选；其他字段 `omitempty` → 不在 required 中；`lifecycle` 不暴露）
- `go test ./pkg/agent/...` 全部通过

---

### 作业 #5 — 修订 `doc/design/04-tools.md`

**目标**：在工具系统设计文档中新增自动化注册章节，更新示例代码。

具体修改：

| 位置 | 变更 |
|------|------|
| §2.1 本地工具注册示例代码 | 将手写 Parameters 的示例改为使用 `autoreg.NewToolFromStruct` |
| §7 文件表 | 追加 `pkg/tool/autoreg/` 行 |
| **新增 §9** | **自动化注册** —— 描述设计动机、`SchemaFromStruct` 类型映射规则、`NewToolFromStruct` 使用方式 |
| 变更日志 | 添加 v0.9.7 条目 |

---

### 作业 #6 — 新增 `pkg/tool/autoreg/schema_test.go` 单元测试

**目标**：为 JSON Schema 生成器 + 废弃兼容机制编写全面测试。

覆盖场景：

```go
func TestSchemaFromStruct_Basic(t *testing.T) {
    type S struct {
        Name string `json:"name" description:"The name"`
        Age  int    `json:"age"`
    }
    s := SchemaFromStruct[S]()
    // assert "type": "object"
    // assert properties.name.type == "string"
    // assert properties.name.description == "The name"
    // assert properties.age.type == "integer"
    // assert required == ["name", "age"]
}

func TestSchemaFromStruct_Optional(t *testing.T) {
    type S struct {
        Name string `json:"name,omitempty"`
    }
    s := SchemaFromStruct[S]()
    // assert required is empty (omitempty → optional)
}

func TestSchemaFromStruct_Pointer(t *testing.T) {
    type S struct {
        Count *int `json:"count" description:"Optional count"`
    }
    s := SchemaFromStruct[S]()
    // assert properties.count.type == "integer"
    // assert required is empty (pointer → optional)
}

func TestSchemaFromStruct_Nested(t *testing.T) {
    type Inner struct {
        Value string `json:"value"`
    }
    type Outer struct {
        Inner Inner `json:"inner"`
    }
    s := SchemaFromStruct[Outer]()
    // assert properties.inner.type == "object"
    // assert properties.inner.properties.value.type == "string"
}

func TestSchemaFromStruct_Slice(t *testing.T) {
    type S struct {
        Items []string `json:"items"`
    }
    s := SchemaFromStruct[S]()
    // assert properties.items.type == "array"
    // assert properties.items.items.type == "string"
}

func TestSchemaFromStruct_Map(t *testing.T) {
    type S struct {
        Labels map[string]string `json:"labels"`
    }
    s := SchemaFromStruct[S]()
    // assert properties.labels.type == "object"
    // assert properties.labels.additionalProperties.type == "string"
}

func TestSchemaFromStruct_JSONRawMessage(t *testing.T) {
    type S struct {
        Data json.RawMessage `json:"data"`
    }
    s := SchemaFromStruct[S]()
    // assert properties.data.type == "object" (special case)
}

func TestSchemaFromStruct_MapStringInterface(t *testing.T) {
    type S struct {
        Data map[string]interface{} `json:"data"`
    }
    s := SchemaFromStruct[S]()
    // assert properties.data.type == "object"
    // assert properties.data has NO additionalProperties key
    // (unlike map[string]string which generates additionalProperties)
}

func TestSchemaFromStruct_EmptyRequired(t *testing.T) {
    type S struct {
        Name string `json:"name,omitempty"`
    }
    s := SchemaFromStruct[S]()
    // assert no "required" key at all (empty required → omit key)
}

func TestSchemaFromStruct_TypeAlias(t *testing.T) {
    type MyStr string
    type S struct {
        Value MyStr `json:"value"`
    }
    s := SchemaFromStruct[S]()
    // assert properties.value.type == "string"
}

func TestNewToolFromStruct(t *testing.T) {
    type AddParams struct {
        A float64 `json:"a"`
        B float64 `json:"b"`
    }
    tool := NewToolFromStruct("add", "Adds two numbers",
        func(ctx context.Context, p AddParams) (string, error) {
            return fmt.Sprintf("%f", p.A+p.B), nil
        },
    )
    // assert tool.Name == "add"
    // assert tool.Parameters["type"] == "object"
    // assert tool.Handle != nil
    // assert tool.Handle(ctx, `{"a":3,"b":5}`) == "8.000000"
}

func TestNewToolFromStruct_WithDeprecatedParameters(t *testing.T) {
    type AddParams struct {
        A float64 `json:"a"`
        B float64 `json:"b"`
    }
    customParams := map[string]interface{}{"type": "object"}
    tool := NewToolFromStruct("add", "Adds two numbers",
        func(ctx context.Context, p AddParams) (string, error) {
            return fmt.Sprintf("%f", p.A+p.B), nil
        },
        WithParameters(customParams),
    )
    // assert tool.Parameters == customParams (overridden)
    // assert Handle still works correctly (unmarshal into AddParams)
}
```

---

### 作业 #7 — 创建 `doc/log/progress-v0.9.7.md`

**目标**：版本进度日志，记录 v0.9.7 的交付清单。

---

### 作业 #8 — 最终验证

- `go build ./...` 编译通过
- `go vet ./...` 无问题
- `go test ./pkg/tool/autoreg/...` 全部通过
- `go test ./pkg/variable/...` 全部通过（var_set 重构后行为不变）
- `go test ./pkg/skill/...` 全部通过（shell_tool + load_tool 重构后行为不变）
- `go test ./pkg/agent/...` 全部通过（spawn_tool 重构后行为不变）
- 4 个重构工具的 Parameters 与原始定义逐字段等价：
  - `var_set`：`updates` 无 additionalProperties，无 `required` 键，全部对应字段 description 一致
  - `execute_shell_script`：`args` 不在 required 中，全部 description 一致
  - `load_skill`：全部一致
  - `spawn_subagent`：可选字段（`system_addendum`/`skill_ids`/`model`/`max_steps`/`allow_child_spawn`）不在 required 中；`lifecycle` 不暴露；全部 description 一致
  - 使用 `encoding/json.Marshal` 将反射生成的 Parameters 与原始 Parameters 序列化后对比，确保 JSON 输出完全一致
- `WithParameters` 废弃路径验证：
  - `slog.Warn` 在传入自定义 Parameters 时正常触发
  - Handle 在自定义 Parameters 下仍能正确反序列化（Handle 与 Parameters 解耦，各自独立工作）
- 搜索 `map[string]interface{}{"type": "object"` 确认不再有工具采用手写 Parameters（仅保留 `toolinfo.go` 中 `ToOpenAITool` 的兜底默认值）
- 文档交叉引用一致

---

## 3. 执行顺序示意图

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

## 4. 后置依赖 / 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| `sonic.UnmarshalString` 对泛型类型参数 `T` 的支持一致性 | 泛型类型反射可能在某些边界情况表现与 `json.Unmarshal` 不同 | 单元测试覆盖所有基础类型 + 边界场景；`autoreg` 内部使用 `sonic` 与项目一致 |
| `json.RawMessage` 反射检测 | `json.RawMessage` 底层是 `[]byte`，`reflect.Kind()` 返回 `Slice`，`Elem()` 返回 `Uint8`，不经特殊处理会生成 `{"type": "array", "items": {"type": "integer"}}` | 在反射循环中增加 `json.RawMessage` 的类型检查（`reflect.Type` 直接比较） |
| 循环引用结构体 | 如果 struct 包含对自身的引用（如 `type Node struct { Next *Node }`），反射会无限递归 | 在 `SchemaFromStruct` 中维护已访问的类型集合，遇到循环引用时停止递归（输出 `{"type": "object"}` 占位） |
| 重构后工具行为偏离 | 生成 JSON Schema 与手动定义可能不完全一致 | 重构后的 Parameters 与原始 JSON Schema 逐字段对比验证 |
| 外部调用方仍使用旧模式 | 不影响——`WithToolInfos` 仍然接受 `[]*model.ToolInfo`，`NewToolFromStruct` 返回的就是 `*ToolInfo` | 零成本迁移，可混用 |
| `WithParameters` 废弃警告被忽略 | 用户虽然看到 `slog.Warn`，但持续依赖自定义 Parameters，未来版本升级时断裂 | 文档中明确标注废弃时间线（当前 v0.9.7 → 警告 → v0.10.x 升级 → v1.0.0 移除） |
| Handle 与自定义 Parameters 冲突 | 用户通过 `WithParameters` 传入与 struct 字段类型不匹配的 JSON Schema，LLM 生成错误参数，Handle 反序列化失败 | 文档中详细说明冲突机制；`WithParameters` 的 deprecation 消息包含冲突警告 |

---

## 5. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-30 | 初稿：v0.9.7 tools 自动化注册实施计划。 |

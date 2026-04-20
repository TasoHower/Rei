# loopForge 产品需求文档 — v0.5.0 Variable 共享变量

> **版本**：v0.5.0  
> **日期**：2026-04-16  
> **里程碑**：Variable — Agent 间自定义共享变量  
> **状态**：已完成  
> **关联文档**：[progress-v0.5.0.md](../log/progress-v0.5.0.md)、[progress-v0.5.1.md](../log/progress-v0.5.1.md)、[progress-v0.5.2.md](../log/progress-v0.5.2.md)

---

## 1. 概述

### 1.1 产品定位

**Variable（共享变量）** 是 loopForge v0.5.0 的核心功能，提供**Agent 间自定义共享变量**机制。通过在多 Agent Transfer 链中自动传递和共享结构化状态，解决「Agent 间无状态传递」、「Tool 无法积累上下文」、「跨 Agent 协作缺少共享空间」三大核心问题。

### 1.2 核心价值

1. **有状态传递** — Transfer 不仅转发对话历史，还传递结构化中间状态
2. **Tool 上下文积累** — 工具执行结果可跨 step 保留（如搜索结果 ID、分页 cursor）
3. **类型安全的共享空间** — 前序 Agent 的决策结果（分类标签、路由参数）可结构化传递给下游
4. **零侵入访问** — Tool 通过 `context.Context` 访问 Store，handler 签名不变
5. **灵活可见性** — visitable 变量自动注入 prompt，非 visitable 仅程序可见

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.4.1** | 包拆分 + 命名统一 + 结构化日志；v0.5.0 在此基础上增加 Variable 机制 |
| **v0.5.1** | 增强 VarStore 参数上下文恢复能力，支持用户动态传入参数 |
| **v0.5.2** | Lark 适配器、Runner 默认 Lark、Ark 工具 schema 兼容；Variable 机制与 Lark 路径集成 |
| **v0.6.0** | MCP 客户端与工具接入；Variable 可作为 MCP tools 的参数来源 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.5.0）

#### P0（必须实现）

1. **新包 `pkg/variable`**
   - Variable Store 独立为包，不耦合 agent 或 runner
   - 提供 `VarStore` 类型（线程安全的 sync.RWMutex map）

2. **核心数据结构**
   - 变量名用 `string` key，值用 `any`
   - 每个变量携带 `Visitable` 元属性（默认 `true`）
   - 每个变量携带 `Description` 描述
   - 支持预声明（Define）未赋值变量

3. **并发安全**
   - `VarStore` 采用 `sync.RWMutex` 并发安全 map
   - 单 Run 内可能有并发 tool 执行，Store 跨 goroutine 传递，必须线程安全

4. **Tool 访问**
   - Tool 通过 `context.Context` 访问 Store
   - Tool handler 签名不变（`func(ctx, args) (string, error)`）
   - 通过 `variable.FromContext(ctx)` 取 Store

5. **内置 `var_set` 工具**
   - Agent 只需一个 `var_set` 工具用于赋值
   - 读取不需要工具——visitable 变量已在 prompt 中，Agent 直接看到
   - `var_set` 自动拦截 `const_` 前缀写入

6. **Store 随 `LoopState` 传递**
   - Transfer 时 Store 自动随 `LoopState` 流转到下游 Agent
   - 无需额外配置

7. **`WithVariable()` Option 开关**
   - Agent 粒度控制是否注入 Variable tools
   - 默认关闭，显式开启

#### P1（增强功能）

1. **`const_` 前缀只读保护**
   - 以 `const_` 为前缀的变量对 Agent 只读
   - `var_set` 工具拒绝写入，仅允许程序化 `store.Set()` 修改
   - 用于注入外部环境常量（用户等级、配置参数等）

2. **Manifest + Materialize（SDK 自动装配）**
   - 业务方只维护一份静态变量清单（key、description、默认 visitable、可选默认值）
   - 每轮 Run 由 `variable.Materialize(manifest, persisted, runtime)` 自动合并
   - 清单保证 prompt 结构稳定，快照恢复上轮值，runtime 注入本轮 `const_*`
   - 调用方无需知道本轮每个变量是否已赋值

### 2.2 不在本版本范围

1. **变量持久化** — Store 仍为纯内存，持久化由应用/SDK 封装负责
2. **变量版本控制** — 不支持变量的版本历史追溯
3. **变量间依赖** — 不支持变量间的依赖声明与自动解析
4. **跨进程共享** — 仅限单进程内 Agent 间共享

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够定义自定义变量并在 Tool 中访问  
**验收标准**：
- 调用 `store.Set("key", value, description, visitable)`
- 在 Tool handler 中通过 `variable.FromContext(ctx)` 获取 Store
- 使用 `store.Get("key")` 读取变量
- 使用 `store.Set("key", value)` 修改变量

**US-2**：能够控制变量是否对 Agent 可见  
**验收标准**：
- 设置 `visitable=true`：变量在每次 LLM 调用前自动序列化并注入 system prompt
- 设置 `visitable=false`：变量仅程序可见，不占用 prompt token
- 在 prompt 中展示格式：`{key}: {value} ({description})`

**US-3**：能够预声明变量而不立即赋值  
**验收标准**：
- 调用 `store.Define("key", description)`
- prompt 中显示 `<unset>`
- 后续可通过 `var_set` 工具或程序化 `store.Set()` 赋值

**US-4**：能够注入只读常量变量  
**验收标准**：
- 使用 `const_` 前缀命名（如 `const_user_tier`）
- `var_set` 工具拒绝写入
- 仅允许程序化 `store.Set()` 修改
- 用于注入用户等级、配置参数等外部环境常量

**US-5**：能够使用 Manifest 自动装配变量  
**验收标准**：
- 定义变量 Manifest（key、description、默认 visitable、可选默认值）
- 调用 `variable.Materialize(manifest, persisted, runtime)`
- 自动合并：清单保证 prompt 结构稳定，快照恢复上轮值，runtime 注入本轮 `const_*`
- 调用方无需知道本轮每个变量是否已赋值

### 3.2 作为 Agent，我希望...

**US-6**：能够看到 visitable 变量的值而无需调用工具  
**验收标准**：
- 每次 LLM 调用前，visitable 变量自动序列化
- 注入到 system prompt 中
- LLM 直接看到变量值和描述
- 无需调用 `var_get` 工具

**US-7**：能够使用 `var_set` 工具修改变量  
**验收标准**：
- 调用 `var_set(key, value)`
- 自动拦截 `const_` 前缀写入（拒绝）
- 修改变量后记录到 Store
- 返回成功消息

**US-8**：能够在 Transfer 时自动传递 Store 给下游 Agent  
**验收标准**：
- Transfer 时 Store 自动随 `LoopState` 流转
- 下游 Agent 可访问上游 Agent 设置的变量
- 无需额外配置

---

## 4. 功能需求

### 4.1 核心数据结构

#### 4.1.1 Variable

```go
type Variable struct {
    Key         string      // 变量名
    Value       any         // 变量值（nil 表示未赋值）
    Visitable   bool        // 是否对 Agent 可见（注入 prompt）
    Description string      // 变量描述
    Readonly    bool        // 是否只读（const_ 前缀自动设为 true）
}
```

#### 4.1.2 VarStore

```go
// VarStore is a thread-safe map for sharing variables across Agents and Tools.
type VarStore struct {
    mu sync.RWMutex
    data map[string]*Variable
}

// NewVarStore creates an empty VarStore.
func NewVarStore() *VarStore

// Set sets a variable value.
func (s *VarStore) Set(key string, value any, description string, visitable bool) error

// Get gets a variable value.
func (s *VarStore) Get(key string) (any, bool)

// Define defines a variable without value (to be set later).
func (s *VarStore) Define(key string, description string) error

// Delete deletes a variable.
func (s *VarStore) Delete(key string)

// List returns all variables.
func (s *VarStore) List() []*Variable

// ToPrompt serializes visitable variables for LLM prompt.
func (s *VarStore) ToPrompt() string
```

### 4.2 Context 集成

#### 4.2.1 注入 Store 到 Context

```go
// WithVarStore returns a context with VarStore attached.
func WithVarStore(ctx context.Context, store *VarStore) context.Context

// FromContext gets VarStore from context.
func FromContext(ctx context.Context) (*VarStore, bool)
```

#### 4.2.2 Tool 访问示例

```go
tool := &model.ToolInfo{
    Name:        "search",
    Description: "Search for orders",
    Parameters:  // ...
    Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
        // 从 Context 获取 Store
        store, ok := variable.FromContext(ctx)
        if !ok {
            return "", fmt.Errorf("VarStore not found")
        }
        
        // 读取变量
        userID, _ := store.Get("user_id")
        
        // 修改变量
        store.Set("last_search", "order_123", "Last search result", true)
        
        // 执行业务逻辑
        return doSearch(userID.(string)), nil
    },
}
```

### 4.3 var_set 工具

#### 4.3.1 工具定义

```go
// VarSetTool creates a tool for setting variables.
func VarSetTool(store *VarStore) *model.ToolInfo {
    return &model.ToolInfo{
        Name:        "var_set",
        Description: "Set a variable value",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "key": map[string]any{
                    "type":        "string",
                    "description": "Variable key",
                },
                "value": map[string]any{
                    "type":        "string",
                    "description": "Variable value",
                },
            },
            "required": []string{"key", "value"},
        },
        Handle: func(ctx context.Context, argumentsJSON string) (string, error) {
            // 解析参数
            var args struct {
                Key   string `json:"key"`
                Value string `json:"value"`
            }
            if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
                return "", err
            }
            
            // 拦截 const_ 前缀写入
            if strings.HasPrefix(args.Key, "const_") {
                return "", fmt.Errorf("cannot modify readonly variable: %s", args.Key)
            }
            
            // 从 Context 获取 Store
            store, ok := FromContext(ctx)
            if !ok {
                return "", fmt.Errorf("VarStore not found")
            }
            
            // 设置变量
            store.Set(args.Key, args.Value, "", true)
            
            return fmt.Sprintf("Set %q to %q", args.Key, args.Value), nil
        },
    }
}
```

### 4.4 Manifest + Materialize

#### 4.4.1 Manifest 定义

```go
// VariableManifest describes a variable.
type VariableManifest struct {
    Key       string // 变量名
    Default   any    // 可选：默认值
    Visitable bool   // 是否对 Agent 可见
    Description string // 变量描述
}

// Example manifest
var manifest = []VariableManifest{
    {
        Key:       "const_user_tier",
        Default:   "free",
        Visitable: true,
        Description: "用户等级",
    },
    {
        Key:       "intent",
        Visitable: true,
        Description: "用户意图分类",
    },
    {
        Key:       "search_cursor",
        Visitable: false,
        Description: "分页游标",
    },
}
```

#### 4.4.2 Materialize 函数

```go
// Materialize merges manifest, persisted snapshot, and runtime variables.
// - manifest: static variable definitions
// - persisted: snapshot from previous run (optional)
// - runtime: runtime-injected variables (e.g., const_* from environment)
// Returns a ready-to-use VarStore.
func Materialize(manifest []VariableManifest, persisted map[string]any, runtime map[string]any) (*VarStore, error) {
    store := NewVarStore()
    
    // 1. 应用 manifest（保证 prompt 结构稳定）
    for _, m := range manifest {
        if m.Default != nil {
            store.Set(m.Key, m.Default, m.Description, m.Visitable)
        } else {
            store.Define(m.Key, m.Description)
        }
    }
    
    // 2. 恢复 persisted 快照（上轮值）
    for key, value := range persisted {
        store.Set(key, value, "", true)
    }
    
    // 3. 注入 runtime 变量（本轮 const_*）
    for key, value := range runtime {
        store.Set(key, value, "", true)
    }
    
    return store, nil
}
```

### 4.5 与 Runner 集成

#### 4.5.1 Runner 参数传递

```go
// Runner.Run 流程：
// 1. 外部 SDK 调用 Materialize 得到 VarStore
// 2. 通过 WithVarStore(store) Option 传入 Runner
// 3. Runner 将 Store 注入每个 Tool 的 Context
// 4. Run 结束返回 RuntimeOutcome.VarStore
// 5. SDK 自动 Snapshot 写回持久化

// 示例：
store, _ := variable.Materialize(manifest, persisted, runtime)

runner := NewRunner(
    agent,
    variable.WithVarStore(store),
)

outcome, err := runner.Run(ctx, request)
if err != nil {
    return err
}

// SDK 自动写回持久化
snapshot := outcome.VarStore.Snapshot()
saveToDatabase(snapshot)
```

#### 4.5.2 Transfer 时自动传递

```go
// Agent.Transfer 自动携带 Store
func (a *Agent) Transfer(ctx context.Context, state *LoopState) (*Agent, error) {
    // 创建新 Agent
    newAgent := NewAgent(
        WithName(state.NextAgent),
        WithInheritedMsgs(state.Messages),
        WithVarStore(state.VarStore), // 自动传递 Store
    )
    
    return newAgent, nil
}
```

---

## 5. 技术需求

### 5.1 并发安全

```go
type VarStore struct {
    mu   sync.RWMutex
    data map[string]*Variable
}

func (s *VarStore) Get(key string) (any, bool) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    v, ok := s.data[key]
    if !ok {
        return nil, false
    }
    return v.Value, ok
}

func (s *VarStore) Set(key string, value any, description string, visitable bool) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    if _, exists := s.data[key]; !exists {
        return fmt.Errorf("variable not defined: %s", key)
    }
    
    s.data[key].Value = value
    s.data[key].Visitable = visitable
    if description != "" {
        s.data[key].Description = description
    }
    return nil
}
```

### 5.2 Prompt 注入

```go
func (s *VarStore) ToPrompt() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    var sb strings.Builder
    sb.WriteString("## Shared Variables\n\n")
    
    for _, v := range s.data {
        if v.Visitable {
            if v.Value == nil {
                sb.WriteString(fmt.Sprintf("- **%s**: `<unset>` (%s)\n", v.Key, v.Description))
            } else {
                sb.WriteString(fmt.Sprintf("- **%s**: `%v` (%s)\n", v.Key, v.Value, v.Description))
            }
        }
    }
    
    return sb.String()
}
```

### 5.3 错误处理

```go
// ErrVariableNotFound returned when getting a non-existent variable.
var ErrVariableNotFound = errors.New("variable not found")

// ErrVariableReadonly returned when trying to modify a readonly variable.
var ErrVariableReadonly = errors.New("variable is readonly")

// ErrVariableNotDefined returned when setting a variable that hasn't been defined.
var ErrVariableNotDefined = errors.New("variable not defined")
```

---

## 6. 验收标准

### 6.1 功能验收

详见 `../acceptance/v0.5.0-acceptance.md`、`../acceptance/v0.5.1-acceptance.md`、`../acceptance/v0.5.2-acceptance.md`

### 6.2 并发验收

- 多 goroutine 并发读写无 race condition
- Transfer 时 Store 正确传递到下游 Agent
- Tool 并发执行时 Store 数据一致

### 6.3 性能验收

- Store 访问延迟 < 1μs
- ToPrompt 序列化时间 < 100μs（100 个变量以内）
- 内存占用 < 1KB/变量

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **Race Condition** | 多 goroutine 并发读写 | `sync.RWMutex`、充分测试 |
| **Prompt 膨胀** | 过多 visitable 变量占用 token | 控制 visitable 数量、使用非 visitable 存储中间状态 |
| **类型安全** | `any` 类型可能导致运行时错误 | 提供泛型辅助函数 `Get[T]`、文档强调类型检查 |
| **持久化语义** | 不同应用持久化策略不同 | SDK 提供 Snapshot/Import，应用自定义存储逻辑 |
| **Transfer 泄漏** | Store 意外传递给不该访问的 Agent | 文档明确边界、代码审查 |

---

## 8. 参考文档

- [progress-v0.5.0.md](../log/progress-v0.5.0.md) — 版本进度日志
- [progress-v0.5.1.md](../log/progress-v0.5.1.md) — VarStore 参数上下文恢复
- [progress-v0.5.2.md](../log/progress-v0.5.2.md) — Lark 适配器集成
- [v0.5.0-acceptance.md](../acceptance/v0.5.0-acceptance.md) — 验收标准
- [v0.5.1-acceptance.md](../acceptance/v0.5.1-acceptance.md) — 验收标准补充

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-16 | v0.5.0 | 初始版本 |
| 2026-04-16 | v0.5.1 | 增强 VarStore 参数上下文恢复 |
| 2026-04-16 | v0.5.2 | Lark 适配器集成 |

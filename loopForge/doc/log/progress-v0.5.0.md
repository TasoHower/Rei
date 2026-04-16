# loopForge 项目进度日志 — v0.5.0

> **版本**：v0.5.0  
> **日期**：2026-04-16  
> **里程碑**：Variable — Agent 间自定义共享变量  
> **上一版本**：v0.4.1（包拆分 + 命名统一 + 结构化日志）

---

## 本版本目标

**引入 Variable 机制**：允许 Agent 在 tool 执行过程中定义、修改、读取自定义变量，变量在多 Agent Transfer 链中自动传递和共享。

### 解决的核心问题

1. **Agent 间无状态传递**：当前 Transfer 只转发对话历史（`inheritedMsgs`），Agent 无法向下游 Agent 传递结构化中间状态（如已收集的表单字段、计算中间结果、用户偏好标记）。
2. **Tool 无法积累上下文**：工具每次调用都是无状态的，无法跨 step 保留结果（如搜索结果 ID、分页 cursor）；只能靠 LLM 从对话历史中"记忆"，既浪费 token 又不可靠。
3. **跨 Agent 协作缺少共享空间**：多 Agent 编排中，前序 Agent 的决策结果（如分类标签、路由参数）只能编码在自然语言里传递，缺少类型安全的结构化通道。

---

## 设计决策

| 决策 | 说明 |
|------|------|
| **新包 `pkg/Variable`** | Variable Store 独立为包，不耦合 agent 或 runner。Agent 和 Runner 按需引用 |
| **`VarStore` 采用 `sync.RWMutex` 并发安全 map** | 单 Run 内可能有并发 tool 执行（未来），且 Store 跨 goroutine 传递，必须线程安全 |
| **变量名用 `string` key，值用 `any`** | 最大灵活性；类型安全通过泛型辅助函数 `Get[T]` 提供 |
| **每个变量携带 `Visitable` 元属性** | 默认 `true`。Visitable 变量在每次 LLM 调用前自动序列化并注入 Agent 的 system prompt，LLM 无需调用 `var_get` 即可感知；设为 `false` 的变量仅程序可见，不占用 prompt token |
| **每个变量携带 `Description` 描述** | 说明变量用途，注入 prompt 时与 key 一起展示。让 Agent 明确知道每个变量的含义和预期用法 |
| **支持预声明（Define）未赋值变量** | 底层能力：`Define(key, description)` 声明 `Value=nil`，prompt 中显示 `<unset>`。多轮/自动编排场景下**不指望业务方每轮手写 Define** |
| **Manifest + Materialize（SDK 自动装配）** | 业务方只维护一份**静态变量清单**（key、description、默认 visitable、可选默认值）；每轮 Run 由 `variable.Materialize(manifest, persisted, runtime)` 自动合并：清单保证 prompt 结构稳定，快照恢复上轮值，runtime 注入本轮 `const_*`（用户 ID、请求时间等）。调用方无需知道本轮每个变量是否已赋值 |
| **`const_` 前缀只读保护** | 以 `const_` 为前缀的变量对 Agent 只读——`var_set` 工具拒绝写入，仅允许程序化 `store.Set()` 修改。用于注入外部环境常量（用户等级、配置参数等） |
| **Store 随 `LoopState` 传递** | Transfer 时 Store 自动随 `LoopState` 流转到下游 Agent，无需额外配置 |
| **Tool 通过 `context.Context` 访问 Store** | Tool handler 签名不变（`func(ctx, args) (string, error)`），通过 `variable.FromContext(ctx)` 取 Store。零侵入 |
| **仅内置 `var_set` 工具** | Agent 只需一个 `var_set` 工具用于赋值。读取不需要工具——visitable 变量已在 prompt 中，Agent 直接看到；`var_set` 自动拦截 `const_` 前缀写入 |
| **`WithVariable()` Option 开关** | Agent 粒度控制是否注入 Variable tools；默认关闭，显式开启 |
| **Store 外部可写入 + Run 结束后返回** | 每轮 Run 前由 SDK 层 `Materialize` 得到 `VarStore` → `WithVarStore()` 传入 Runner → Run 结束 `RuntimeOutcome.VarStore` 返回 → SDK 自动 `Snapshot` 写回持久化，形成参数池 |
| **Store 所有权与持久化** | `VarStore` 仍为纯内存；**核心库**只提供 `Snapshot`/`Import`/`Materialize`。**应用/SDK 封装**负责按 `session_id` 等键存取快照；业务代码每轮只调 `Materialize(...)`，不手写每变量 `Define` |

---

## 核心概念

```
                           SDK 调用者（外部）
                           ┌─────────────────────────────────────┐
                           │  持久化参数池（DB / Redis / File）    │
                           │  ┌─────────────────────────────┐    │
                           │  │ user_tier: premium          │    │
                           │  │ last_order: 12345           │    │
                           │  │ preference_lang: zh-CN      │    │
                           │  └─────────────────────────────┘    │
                           └───────┬─────────────────▲───────────┘
                              ① Materialize（manifest + 持久化快照 + runtime）
                              ⑤ Snapshot → SDK 自动写回持久化
                                  ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                              Runner.Run()                                    │
│                                                                              │
│   VarStore (thread-safe, 外部传入或自动创建)                                    │
│   ┌────────────────────────────────────────────────────────────────────┐     │
│   │  key               value           visitable  description         │     │
│   │  ────────────────   ─────────────   ────────   ─────────────────  │     │
│   │  "const_user_tier"  "premium"       true       "用户等级"    🔒   │     │
│   │  "intent"           "order_status"  true       "用户意图分类"      │     │
│   │  "order_id"         nil (unset)     true       "待查询的订单号"    │     │
│   │  "urgency"          nil (unset)     true       "紧急程度"         │     │
│   │  "search_cursor"    "eyJwYWdl..."   false      "分页游标"         │     │
│   └────────────────────────────────────────────────────────────────────┘     │
│     ② 注入 context          ▲ set/get           ▲ set/get                   │
│          │                   │                   │                           │
│   ┌──────┴──────┐  transfer  ┌──────┴──────┐                                │
│   │  Agent A    │ ─────────→ │  Agent B    │  ③ Agent 运行时读写              │
│   │  (triage)   │            │  (expert)   │                                │
│   │  var_set ←──│── Tools    │──→ var_set  │                                │
│   │  (const_* 🚫)           │  (const_* 🚫)                                │
│   └─────────────┘            └─────────────┘                                │
│                                                                              │
│   ④ Run 结束 → RuntimeOutcome.VarStore 携带完整 Store 返回                    │
└──────────────────────────────────────────────────────────────────────────────┘
```

### VarStore 生命周期：外部持久化循环

```
     ┌──────────────────────────────────────────────────────────┐
     │                 SDK 调用者维护的持久化循环                  │
     │                                                          │
     │   Run 1                Run 2                Run N        │
     │   ┌──────┐            ┌──────┐            ┌──────┐      │
     │   │ 传入 │ ──Store──→ │ 传入 │ ──Store──→ │ 传入 │      │
     │   │Runner│            │Runner│            │Runner│      │
     │   │ 返回 │ ←─Store──  │ 返回 │ ←─Store──  │ 返回 │      │
     │   └──────┘     │      └──────┘     │      └──────┘      │
     │                ▼                   ▼                     │
     │           持久化到 DB          持久化到 DB                 │
     │                                                          │
     └──────────────────────────────────────────────────────────┘
```

### 数据流

1. **业务侧一次配置**：定义 `VariableManifest`（本应用有哪些变量、各自 description、是否 visitable、可选默认值）。不关心某次 Run 里各变量当前是 `<unset>` 还是已有值
2. **每轮 Run 前 — SDK 自动装配**：从持久化读取该会话上次的 `StoreSnapshot`（若无则为 `nil`），调用 `variable.Materialize(manifest, persisted, runtime)` 生成本轮 `VarStore`：
   - **清单优先**：manifest 中出现的 key 保证存在且带正确 description；持久化里有的值自动恢复；manifest 有、持久化无的 key 为 `<unset>` 或默认值
   - **runtime**：本轮才确定的量（如 `const_user_id`、`const_request_time`）由 map 或回调注入，覆盖同 key 的旧快照
   - **孤儿清理**（可选）：`MaterializeOption` 如 `WithDropOrphans(true)` 删除 manifest 已移除、快照仍存在的 key，避免 Agent 看到废弃变量
3. **传入 Runner**：`runner.NewRunner(entry, runner.WithVarStore(store))`
4. **RunLoop 每次调用 LLM 前**，提取所有 `visitable=true` 的变量（含未赋值的），连同 description 一起序列化并追加到 system prompt
5. **Agent 通过 `var_set` 赋值**：LLM 看到 `<unset>` 的变量后主动调用 `var_set` 赋值；`const_` 前缀的变量被 `var_set` 拒绝写入
6. **RunLoop** 每次执行 tool 时，tool handler 通过 `variable.FromContext(ctx)` 获取 Store
7. **Tool handler** 调用 `store.Set("key", value)` / `store.Get("key")` 读写变量（程序化调用不受 `const_` 限制）
8. **Transfer** 时 `LoopState.VarStore` 自动传递给下游 Agent，所有变量（含 description、const 属性）继续注入新 Agent 的 system prompt
9. **Run 结束 → Store 返回**：`RuntimeOutcome.VarStore` 携带完整 Store 引用
10. **每轮 Run 后 — SDK 自动持久化**：`outcome.VarStore.Snapshot()` → 写回 DB / Redis / File（与 `session_id` 绑定）；下一轮回到步骤 2，业务代码仍只调 `Materialize`，无需手写 `Define`/`Set` 拼装

---

## 交付清单

### Step 1：`pkg/variable` — VarStore 核心实现

- [ ] `pkg/variable/store.go` — `VarStore` + `VarEntry`（Description/Visitable）+ CRUD + Define + AgentSet + PromptBlock
- [ ] `pkg/variable/const.go` — `ConstPrefix` + `IsConst()` 判断
- [ ] `pkg/variable/context.go` — `NewContext` / `FromContext` context 注入与提取
- [ ] `pkg/variable/typed.go` — 泛型辅助 `Get[T]` / `MustGet[T]`
- [ ] `pkg/variable/manifest.go` — `Manifest` / `Spec` + `Materialize`（合并清单 + 持久化快照 + runtime 绑定）

#### VarEntry — 变量存储单元

每个变量不再是裸 `any`，而是 `VarEntry`，携带值、描述和元属性：

```go
type VarEntry struct {
    Value       any    // nil 表示已声明但未赋值
    Description string // 变量用途描述，注入 prompt 时展示
    Visitable   bool   // 默认 true；为 true 时自动注入 Agent system prompt
}
```

#### 三层变量状态

| 状态 | Value | Visitable | Prompt 展示 | 说明 |
|------|-------|-----------|-------------|------|
| **已声明未赋值** | `nil` | `true` | `order_id = <unset>  # 待查询的订单号` | Agent 知道有此变量可用，可通过 `var_set` 赋值 |
| **已赋值可见** | 非 nil | `true` | `intent = "order_status"  # 用户意图分类` | Agent 直接在 prompt 中看到值 |
| **已赋值不可见** | 非 nil | `false` | （不出现） | 仅程序化访问，不占 prompt token |

#### `const_` 前缀只读保护

以 `const_` 为前缀的变量对 Agent **只读**：

- **`var_set` 工具拒绝写入**：Agent 调用 `var_set(key="const_xxx", ...)` 时返回错误 `"variable 'const_xxx' is read-only (const_ prefix)"`
- **程序化 `store.Set()` 不受限制**：开发者可通过代码自由设置 `const_*` 变量
- **典型用途**：注入外部环境常量（用户等级、租户 ID、功能开关），Agent 能看到但不能改

```go
const ConstPrefix = "const_"

func IsConst(key string) bool { return strings.HasPrefix(key, ConstPrefix) }
```

#### VarStore API

```go
package variable

type VarStore struct {
    mu   sync.RWMutex
    vars map[string]*VarEntry
}

func New() *VarStore

// Define 预声明变量（Value=nil），Agent 在 prompt 中可看到但值为 <unset>
func (s *VarStore) Define(key, description string, opts ...SetOption)

// CRUD — Set 默认 visitable=true
func (s *VarStore) Set(key string, value any, opts ...SetOption)
func (s *VarStore) Get(key string) (any, bool)
func (s *VarStore) GetEntry(key string) (*VarEntry, bool)
func (s *VarStore) Delete(key string)
func (s *VarStore) Keys() []string
func (s *VarStore) All() map[string]*VarEntry    // 返回浅拷贝
func (s *VarStore) Len() int
func (s *VarStore) IsSet(key string) bool        // 变量已声明且 Value != nil

// Visitable 过滤
func (s *VarStore) Visitable() []*VarEntry       // 仅返回 visitable=true 的条目（含未赋值的）
func (s *VarStore) PromptBlock() string          // 序列化 visitable 变量为 system prompt 文本块

// Agent-safe 写入（const_ 校验）
func (s *VarStore) AgentSet(key string, value any) error  // const_ 前缀返回 error

// 批量操作
func (s *VarStore) SetMany(pairs map[string]any) // 批量设置，均为默认 visitable=true
func (s *VarStore) Merge(other *VarStore)        // other 的 entry 覆盖 s（保留元属性）

// 序列化 / 反序列化 — 供调用者持久化
func (s *VarStore) Snapshot() *StoreSnapshot            // 导出完整快照（JSON-safe 结构体）
func Import(snap *StoreSnapshot) *VarStore              // 从快照恢复 VarStore
func (s *VarStore) MarshalJSON() ([]byte, error)        // json.Marshaler
func (s *VarStore) UnmarshalJSON(data []byte) error     // json.Unmarshaler

// StoreSnapshot 是 VarStore 的可序列化快照，用于持久化
type StoreSnapshot struct {
    Vars []VarSnapshot `json:"vars"`
}
type VarSnapshot struct {
    Key         string `json:"key"`
    Value       any    `json:"value"`           // nil 表示未赋值
    Description string `json:"description"`
    Visitable   bool   `json:"visitable"`
}

// SetOption 控制 Set/Define 行为
type SetOption func(*setConfig)
type setConfig struct {
    visitable   *bool
    description *string
}

func WithVisitable(v bool) SetOption       // 显式指定 visitable，不传则默认 true
func WithDescription(d string) SetOption   // 设置描述文本

// 泛型辅助（typed.go）
func Get[T any](s *VarStore, key string) (T, bool)
func MustGet[T any](s *VarStore, key string) T  // panic on miss or type mismatch
```

#### PromptBlock 输出格式

`PromptBlock()` 将所有 `visitable=true` 的变量（含未赋值的）序列化为如下格式，追加到 system prompt 末尾：

```
[Variables]
const_user_tier = "premium"  # 用户等级 (read-only)
intent = "order_status"  # 用户意图分类
order_id = <unset>  # 待查询的订单号
urgency = <unset>  # 紧急程度 (low/medium/high)
```

- key 按字母序排列，保证 prompt 稳定（避免无意义的 cache miss）
- value 用 JSON 编码（string 带引号，数组/对象保持 JSON 格式），未赋值显示 `<unset>`
- description 作为 `# 注释` 追加在行尾
- `const_` 前缀的变量额外标注 `(read-only)`
- 若无 visitable 变量，不追加任何内容（空 block 不注入）

#### Context 集成

```go
type ctxKey struct{}

func NewContext(parent context.Context, store *VarStore) context.Context
func FromContext(ctx context.Context) *VarStore  // nil-safe，不存在返回空 Store
```

#### Manifest 与 Materialize（多轮 / 自动 Run 的默认路径）

业务方只描述「本应用有哪些变量」，不负责每轮 Run 前逐个 `Define`。SDK 或应用封装在**每次** `Runner.Run` 前调用：

```go
// Spec 描述一个变量在清单中的静态元数据（与某次 Run 的值无关）。
type Spec struct {
    Key         string
    Description string
    Visitable   bool // 默认 true
    Default     any  // 可选；非 nil 则首轮即带默认值，否则首轮为 <unset>
}

type Manifest struct {
    Specs []Spec
}

type MaterializeOption func(*materializeConfig)

// Materialize 合并：manifest（结构） + persisted（上轮快照，可为 nil） + runtime（本轮 const_* 等）
func Materialize(m *Manifest, persisted *StoreSnapshot, runtime map[string]any, opts ...MaterializeOption) *VarStore
```

**合并规则（摘要）**：

1. 以 `Import(persisted)` 得到的 store 为基底（`persisted == nil` 时等价于空 store）。
2. 对 manifest 中每个 `Spec`：若 key 尚不存在 → `Define` 或 `Set(Default)`；若已存在（来自快照）→ 保留值，必要时用 manifest 补全 description / visitable。
3. 对 `runtime` 中每个 key：`Set(k, v)`（典型为 `const_*`），覆盖快照中的同 key，保证本轮请求上下文最新。
4. 可选 `WithDropOrphans(true)`：删除 store 中存在但 manifest 中已不声明的 key。

> **与手写 `Define` 的关系**：`Define` / `Set` 仍保留给单测、一次性脚本、或不用 manifest 的极简集成；**产品级多轮对话应默认走 `Materialize`**。

### Step 2：Variable Tool — `var_set`

- [ ] `pkg/variable/tools.go` — `VarSetTool`（含 const_ 保护）

> **为什么只有 `var_set`，不提供 `var_get` / `var_list`？**  
> 所有 visitable 变量（含未赋值的）已经通过 `PromptBlock()` 注入 system prompt，Agent 直接在 prompt 中看到变量名、值、描述和只读标记。不需要额外的工具来读取或列举——这只会浪费一次 tool call 的 token 开销。Agent 唯一需要的操作是**赋值**。

#### Tool 定义

| Tool 名称 | 参数 | 返回 | 说明 |
|-----------|------|------|------|
| `var_set` | `{"key": "string", "value": "any"}` | `"ok"` 或 error | 为变量赋值。`const_*` 前缀返回错误 |

`var_set` 的 const_ 保护逻辑：

```go
func varSetHandle(store *VarStore) func(ctx context.Context, args string) (string, error) {
    return func(ctx context.Context, args string) (string, error) {
        // ...parse args...
        if IsConst(key) {
            return "", fmt.Errorf("variable %q is read-only (const_ prefix)", key)
        }
        store.AgentSet(key, value)
        return "ok", nil
    }
}
```

```go
func VarSetTool(store *VarStore) *model.ToolInfo
```

### Step 3：Agent 集成 — `WithVariable` Option

- [ ] `pkg/agent/options.go` — 新增 `WithVariable()` Option
- [ ] `pkg/agent/agent.go` — 新增 `Variable bool` 字段，`Clone()` 同步拷贝

```go
// options.go
func WithVariable() Option {
    return func(a *Agent) { a.Variable = true }
}
```

### Step 4：RunLoop 注入 VarStore

- [ ] `pkg/agent/loop.go` — RunLoop 注入 VarStore 到 context，Variable Agent 追加 tools
- [ ] `pkg/agent/agent.go` — `LoopState` 新增 `VarStore *Variable.VarStore` 字段

#### 变更要点

```go
// agent.go — LoopState 扩展
type LoopState struct {
    AccumulatedMetrics outcome.RunMetrics
    TransferChain      []string
    SuppressBookends   bool
    VarStore           *variable.VarStore  // 新增：跨 Agent 共享变量
}

// loop.go — RunLoop 内部
func (a *Agent) RunLoop(...) *InterceptedCall {
    // 解析或创建 VarStore
    var store *variable.VarStore
    if state != nil && state.VarStore != nil {
        store = state.VarStore
    } else {
        store = variable.New()
    }
    ctx = variable.NewContext(ctx, store)

    // 如果 Agent 开启了 Variable，追加内置 tools
    if a.Variable {
        a.ExtraTools = append(a.ExtraTools, variable.Tools(store)...)
    }

    // 保存原始 system prompt（用于每次 LLM 调用前动态拼接）
    baseSystemInstructions := a.SystemInstructions

    // --- main loop ---
    for step := range maxSteps {
        // ★ 关键：每次调用 LLM 前，将 visitable 变量注入 system prompt
        if block := store.PromptBlock(); block != "" {
            a.SystemInstructions = baseSystemInstructions + "\n\n" + block
        } else {
            a.SystemInstructions = baseSystemInstructions
        }

        // ...调用 LLM、处理 tool calls（与现有流程一致）...
    }
}
```

> **为什么每次循环都重新拼接？**  
> Tool 执行可能修改变量（`var_set` 或程序化 `store.Set`），下一次 LLM 调用需要看到最新值。
> 保存 `baseSystemInstructions` 避免变量段无限累加。

### Step 5：Runner 集成 — Transfer 传递 VarStore

- [ ] `pkg/runner/runner.go` — `WithVarStore` Option + `runTransferLoop` 使用 VarStore
- [ ] Runner 支持外部传入已预声明好的 VarStore（开发者预定义变量结构）

```go
// runner option
func WithVarStore(s *variable.VarStore) RunOption {
    return func(r *Runner) { r.varStore = s }
}

func (r *Runner) runTransferLoop(...) {
    // 使用外部传入的 Store 或新建
    store := r.varStore
    if store == nil {
        store = variable.New()
    }

    // ...
    for {
        st := &agent.LoopState{
            // ...existing fields...
            VarStore: store,  // 同一个 Store 贯穿 Transfer 链
        }
        // ...
    }
}

func (r *Runner) Run(...) <-chan *event.RuntimeEvent {
    go func() {
        defer close(ch)
        store := r.varStore
        if store == nil {
            store = variable.New()
        }
        if len(r.entryAgent.Handoffs()) > 0 {
            r.runTransferLoop(ctx, req, ch)
        } else {
            ctx = variable.NewContext(ctx, store)
            r.entryAgent.RunLoop(ctx, req, ch, nil, &agent.LoopState{VarStore: store})
        }
    }()
    return ch
}
```

### Step 6：RuntimeOutcome 携带 VarStore — Run 结束后返回给调用者

- [ ] `pkg/runtime/outcome/outcome.go` — `RuntimeOutcome` 新增 `VarStore` 字段
- [ ] `pkg/agent/loop.go` — RunLoop 在构建 outcome 时写入 VarStore 引用
- [ ] `pkg/runner/runner.go` — runTransferLoop 结束时将 Store 写入 outcome

```go
// outcome.go
type RuntimeOutcome struct {
    RunID         string
    FinalText     string
    Termination   TerminationReason
    Metrics       RunMetrics
    ChildRunIDs   []string
    TransferChain []string
    VarStore      *variable.VarStore  // 新增：Run 结束时的完整变量状态，调用者可读取/持久化
}
```

#### InterceptedCall 携带 VarStore 快照（供审计）

- [ ] `pkg/agent/agent.go` — `InterceptedCall` 新增 `VarSnapshot`

```go
type InterceptedCall struct {
    ToolCall    model.ToolCallPart
    Msgs        []*model.Message
    Metrics     outcome.RunMetrics
    VarSnapshot *variable.StoreSnapshot  // transfer 时刻的变量快照
}
```

### Step 7：Event 扩展（可选，用于可观测性）

- [ ] `pkg/runtime/event/event.go` — 新增 `EventVarChange` 事件类型

```go
const EventVarChange EventMessageType = "var_change"

type VarChangePayload struct {
    Operation string `json:"operation"` // "set" | "delete"
    Key       string `json:"key"`
    Value     any    `json:"value,omitempty"`
    Agent     string `json:"agent"`
}

func (*VarChangePayload) eventPayload() EventMessageType { return EventVarChange }
```

### Step 8：测试

- [ ] `pkg/variable/store_test.go` — Store CRUD + 并发安全
- [ ] `pkg/variable/store_test.go` — Define 预声明 + IsSet 状态检查
- [ ] `pkg/variable/store_test.go` — Description 设置与读取
- [ ] `pkg/variable/store_test.go` — AgentSet const_ 前缀拒绝 + 程序化 Set 允许
- [ ] `pkg/variable/store_test.go` — PromptBlock 输出格式（含 unset、description、read-only 标注）
- [ ] `pkg/variable/manifest_test.go` — Materialize：清单 + 快照 + runtime 合并、孤儿清理
- [ ] `pkg/variable/store_test.go` — Snapshot 导出 + Import 恢复往返一致性
- [ ] `pkg/variable/store_test.go` — MarshalJSON / UnmarshalJSON
- [ ] `pkg/variable/context_test.go` — context 注入/提取
- [ ] `pkg/variable/typed_test.go` — 泛型 Get[T] 类型匹配/失败
- [ ] `pkg/variable/tools_test.go` — var_set 正常赋值 + const_ 拒绝
- [ ] `pkg/agent/` — RunLoop + Variable tools 集成测试 + prompt 注入验证
- [ ] `pkg/runner/` — Transfer 链 VarStore 传递 + WithVarStore Option
- [ ] `pkg/runner/` — outcome.VarStore 返回验证
- [ ] 集成测试 — Store 外部写入 → Runner 运行 → outcome 返回 → Snapshot → Import 恢复全链路
- [ ] `go build ./...` 全量编译通过

### Step 9：文档 & 示例

- [ ] `pkg/variable/doc.go` — 包文档
- [ ] `cmd/agentdemo` 或 `test-server` — 演示 Variable（Materialize + const_ + 持久化）

---

## 使用示例

### 场景 1：多轮对话推荐 — Manifest + Materialize（无需每轮手写 Define）

```go
// 应用启动时注册一次：本系统有哪些变量（与单次 Run 状态无关）
var appManifest = &variable.Manifest{Specs: []variable.Spec{
    {Key: "const_user_tier", Description: "用户等级（只读）", Visitable: true},
    {Key: "const_tenant_id", Description: "租户标识（只读）", Visitable: true},
    {Key: "order_id", Description: "待查询的订单号"},
    {Key: "intent", Description: "用户意图分类 (order_status/refund/general)"},
    {Key: "urgency", Description: "紧急程度 (low/medium/high)"},
    {Key: "search_cursor", Description: "分页游标", Visitable: false},
}}

// 每次 Run 前：SDK 从存储读快照 + 注入本轮 runtime，自动得到 Store
func prepareVarStore(sessionID string, userTier, tenantID string) *variable.VarStore {
    snapJSON, _ := db.Load(sessionID, "var_snapshot") // 无则 nil
    var persisted *variable.StoreSnapshot
    if len(snapJSON) > 0 {
        var snap variable.StoreSnapshot
        if json.Unmarshal(snapJSON, &snap) == nil {
            persisted = &snap
        }
    }
    runtime := map[string]any{
        "const_user_tier": userTier,
        "const_tenant_id": tenantID,
    }
    return variable.Materialize(appManifest, persisted, runtime)
}
```

### 场景 1b：底层手写（仅测试 / 极简脚本）

```go
store := variable.New()
store.Set("const_user_tier", "premium", variable.WithDescription("用户等级，只读"))
store.Define("order_id", "待查询的订单号")
// ...
```

此时 Agent 的 system prompt 末尾会自动注入：

```
[Variables]
const_tenant_id = "acme-corp"  # 租户标识 (read-only)
const_user_tier = "premium"  # 用户等级 (read-only)
intent = <unset>  # 用户意图分类 (order_status/refund/general)
order_id = <unset>  # 待查询的订单号
urgency = <unset>  # 紧急程度 (low/medium/high)
```

### 场景 2：Tool handler 中读写变量（程序化）

```go
classifyTool := &model.ToolInfo{
    Name:        "classify_intent",
    Description: "Classify user intent and store result",
    Parameters:  classifySchema,
    Handle: func(ctx context.Context, args string) (string, error) {
        store := variable.FromContext(ctx)
        intent := doClassify(args)
        // 赋值 visitable 变量 → 下次 LLM 调用在 prompt 中看到
        store.Set("intent", intent)
        // 程序化可以写 const_（但 Agent 通过 var_set 不能）
        store.Set("const_session_start", time.Now().String())
        // visitable=false → 仅程序可见
        store.Set("raw_embedding", embedding, variable.WithVisitable(false))
        return fmt.Sprintf("Intent: %s", intent), nil
    },
}
```

### 场景 3：LLM 通过 Variable Tools 赋值

```go
triage := agent.New(chat,
    agent.WithName("triage"),
    agent.WithVariable(),   // 注入 var_set 工具
    agent.WithSystemInstructions(`你是分诊 Agent。
        查看 [Variables] 中的可用变量，用 var_set 为未赋值的变量填入合适的值，
        然后 transfer 给对应专家。注意 const_ 前缀的变量是只读的。`),
)

expert := agent.New(chat,
    agent.WithName("expert"),
    agent.WithVariable(),
    agent.WithSystemInstructions(`你是专家 Agent。
        [Variables] 中已有上游 Agent 设置的信息，直接使用即可。`),
)

triage.AddHandoff(expert)
r := runner.NewRunner(triage, runner.WithVarStore(store))
```

### 场景 4：跨 Agent 完整状态流转

```
用户: "帮我查一下订单 #12345 的状态"

[triage Agent]
  system prompt:
    "你是分诊 Agent..."
    [Variables]
    const_user_tier = "premium"  # 用户等级 (read-only)
    intent = <unset>  # 用户意图分类
    order_id = <unset>  # 待查询的订单号
    urgency = <unset>  # 紧急程度

  step 1:
    LLM 看到 3 个 <unset> 变量，决定赋值：
    1. var_set(key="intent", value="order_status")        → ok
    2. var_set(key="order_id", value="12345")             → ok
    3. var_set(key="urgency", value="low")                → ok
    4. var_set(key="const_user_tier", value="free")       → ❌ error: read-only
    5. transfer_to_order_agent(reason="查询订单状态")

[order Agent]
  system prompt:
    "你是订单专家..."
    [Variables]
    const_user_tier = "premium"  # 用户等级 (read-only)
    intent = "order_status"  # 用户意图分类
    order_id = "12345"  # 待查询的订单号
    urgency = "low"  # 紧急程度

  step 1:
    LLM 直接从 prompt 中看到所有变量（已由 triage 赋值），无需 var_get
    1. query_order(id="12345") → 订单信息
    2. 回复用户
```

### 场景 5：SDK 跨会话持久化（每轮只 Materialize + 自动存盘）

```go
manifest := &variable.Manifest{Specs: []variable.Spec{
    {Key: "const_user_tier", Description: "用户等级"},
    {Key: "preference_lang", Description: "用户偏好语言"},
    {Key: "last_topic", Description: "上次对话主题"},
}}

// === Run 1 ===
persisted := loadSnap(userID) // nil 或 *StoreSnapshot
store := variable.Materialize(manifest, persisted, map[string]any{
    "const_user_tier": "premium",
})
r := runner.NewRunner(entry, runner.WithVarStore(store))
var outcome *outcome.RuntimeOutcome
for ev := range r.Run(ctx, req) {
    if qe := ev.QueryEnd(); qe != nil {
        outcome = qe.Outcome
    }
}
snap := outcome.VarStore.Snapshot()
saveSnap(userID, snap)

// === Run 2（用户等级变更：只改 runtime，仍不手写 Define）===
store = variable.Materialize(manifest, loadSnap(userID), map[string]any{
    "const_user_tier": "free",
})
r = runner.NewRunner(entry, runner.WithVarStore(store))
// preference_lang / last_topic 由快照自动恢复；清单保证变量结构一致
```

### 场景 6：HTTP Server 集成模式（Materialize 一条路径）

```go
var chatManifest = &variable.Manifest{Specs: []variable.Spec{
    {Key: "const_user_id", Description: "用户 ID"},
    {Key: "const_request_time", Description: "请求时间（只读）"},
    {Key: "preference_lang", Description: "用户偏好语言"},
}}

func handleChat(ctx context.Context, userID string, msg string) {
    var persisted *variable.StoreSnapshot
    if snapJSON, err := redis.Get(ctx, "vars:"+userID).Bytes(); err == nil {
        _ = json.Unmarshal(snapJSON, &persisted)
    }

    store := variable.Materialize(chatManifest, persisted, map[string]any{
        "const_user_id":      userID,
        "const_request_time": time.Now().Format(time.RFC3339),
    })

    r := runner.NewRunner(entry, runner.WithVarStore(store))
    ch := r.Run(ctx, &request.RuntimeRequest{UserMessage: msg})

    // 流式响应 + 提取 outcome
    var oc *outcome.RuntimeOutcome
    for ev := range ch {
        // ...stream to client...
        if qe := ev.QueryEnd(); qe != nil {
            oc = qe.Outcome
        }
    }

    // 持久化到 Redis（TTL 30 分钟）
    if oc != nil && oc.VarStore != nil {
        snapJSON, _ := json.Marshal(oc.VarStore.Snapshot())
        redis.Set(ctx, "vars:"+userID, snapJSON, 30*time.Minute)
    }
}
```

---

## 依赖关系图

```
pkg/variable              (新包)
  ├── sync（标准库）
  └── encoding/json（标准库）

pkg/runtime/outcome
  └── pkg/variable         (新增依赖：RuntimeOutcome.VarStore)

pkg/agent
  ├── pkg/model
  ├── pkg/tool
  ├── pkg/variable         (新增依赖)
  └── pkg/runtime/{event,outcome,request}

pkg/runner
  ├── pkg/agent
  ├── pkg/log
  ├── pkg/model
  ├── pkg/variable         (新增依赖)
  └── pkg/runtime/{event,outcome,request}
```

无循环依赖。`pkg/variable` 不依赖 `pkg/agent`、`pkg/runner` 或 `pkg/runtime`。

---

## 文件变更预览

| 路径 | 操作 | 说明 |
|------|------|------|
| `pkg/variable/store.go` | **新建** | VarStore + VarEntry + StoreSnapshot + CRUD + Define + AgentSet + PromptBlock + Snapshot/Import + JSON 序列化 |
| `pkg/variable/manifest.go` | **新建** | `Manifest` / `Spec` + `Materialize` + 可选孤儿清理 |
| `pkg/variable/const.go` | **新建** | `ConstPrefix` 常量 + `IsConst()` 判断 |
| `pkg/variable/context.go` | **新建** | context 注入 / 提取 |
| `pkg/variable/typed.go` | **新建** | 泛型辅助 Get[T] / MustGet[T] |
| `pkg/variable/tools.go` | **新建** | `VarSetTool`（含 const_ 保护） |
| `pkg/variable/doc.go` | **新建** | 包文档 |
| `pkg/variable/manifest_test.go` | **新建** | Materialize 合并与孤儿清理 |
| `pkg/variable/store_test.go` | **新建** | Store 单元测试（CRUD + Define + const_ + PromptBlock + Snapshot/Import 往返） |
| `pkg/variable/context_test.go` | **新建** | Context 单元测试 |
| `pkg/variable/typed_test.go` | **新建** | 泛型辅助测试 |
| `pkg/variable/tools_test.go` | **新建** | var_set 测试（正常赋值 + const_ 拒绝） |
| `pkg/agent/agent.go` | 修改 | Agent 新增 `Variable` 字段；LoopState 新增 `VarStore`；InterceptedCall 新增快照 |
| `pkg/agent/options.go` | 修改 | 新增 `WithVariable()` |
| `pkg/agent/loop.go` | 修改 | RunLoop 创建/注入 VarStore + 追加 Variable tools + PromptBlock 注入 |
| `pkg/runner/runner.go` | 修改 | 新增 `WithVarStore` Option + runTransferLoop 使用 VarStore + outcome 写入 Store |
| `pkg/runtime/outcome/outcome.go` | 修改 | `RuntimeOutcome` 新增 `VarStore *variable.VarStore` 字段 |
| `pkg/runtime/event/event.go` | 修改 | 新增 `EventVarChange` + `VarChangePayload`（可选） |

---

## 与 v0.4.1 下一步计划的关系

v0.4.1 规划的 v0.5.0 原计划包含多个方向（Agent Logger、Guardrails、Context/Memory 等）。本版本聚焦 **Variable** 这一核心能力，其余方向顺延至后续版本：

| 原计划 | 调整 |
|--------|------|
| Agent Logger（P0） | → v0.5.1，Variable 完成后再接入 |
| Guardrails（P0） | → v0.5.2，需先完成 Variable + Logger 基础设施 |
| Context / Memory（P0） | Variable 即为其子集（短期结构化状态）；完整 Memory 含 token 预算管理 → v0.6.0 |
| Spawn（P1） | → v0.6.0 |
| MCP Client（P1） | → v0.6.0 |

---

## 提交记录（开发日志）

| 日期 | 说明 |
|------|------|
| 2026-04-16 | 版本计划初始化。确定 Variable 机制设计：`pkg/variable` 独立包 + VarStore + context 注入 + var_set Tool + Agent/Runner 集成。 |
| 2026-04-16 | 补充 Visitable 属性设计：`VarEntry` 携带 `Visitable bool`（默认 true），visitable 变量通过 `PromptBlock()` 自动注入 system prompt。`var_set` 工具增加可选 `visitable` 参数。RunLoop 每次 LLM 调用前动态拼接。 |
| 2026-04-16 | 补充 Description + Define + const_ 设计：变量增加 `Description` 描述字段；`Define()` 预声明未赋值变量（prompt 中显示 `<unset>`）；`const_` 前缀变量对 Agent 只读（`var_set` 拒绝，`AgentSet` 返回 error）；Runner 新增 `WithVarStore` Option 支持外部传入预定义 Store。 |
| 2026-04-16 | 补充 Store 外部持久化循环设计：Store 由 SDK 调用者创建/预填充 → 传入 Runner → Agent 运行时读写 → Run 结束通过 `RuntimeOutcome.VarStore` 返回给调用者。新增 `Snapshot()` / `Import()` / JSON 序列化用于持久化。调用者可跨会话维护参数池（DB/Redis/File）。 |
| 2026-04-16 | 精简 Agent Tools：移除 `var_get` 和 `var_list` 工具，仅保留 `var_set`。visitable 变量已通过 PromptBlock 注入 prompt，Agent 直接可见，无需工具读取。 |
| 2026-04-16 | 多轮对话默认路径：`Manifest` + `Materialize(manifest, persisted, runtime)` 自动合并清单、持久化快照与本轮 runtime；业务方不每轮手写 `Define`。数据流与场景 1/5/6 改为 SDK 自动装配与持久化。 |

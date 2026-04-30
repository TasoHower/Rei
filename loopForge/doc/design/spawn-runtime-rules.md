# spawn 运行时规则

> **角色**：loopForge 引擎内 spawn 父子 Agent 隔离的**硬约束**——所有实现的正确性最终以此文件为准。  
> **覆盖范围**：`DefaultSpawner.Spawn()` 中校验和执行阶段的全部规则，以及 `engineSpawner` 注入的引擎层约束。  
> **关联实现**：[10-spawn.md](10-spawn.md)（Spawn 实现详解）  
> **事件协议**：[03-events.md](03-events.md) §2.9–2.10（`spawn_start` / `spawn_end`）  
>
> **本文件与 `10-spawn.md` 的约定**：  
> `10-spawn.md` 提供完整的数据流、代码结构、字段定义和序列图，可以作为查找实现的入口。  
> 本文件聚焦 spawn 子 Agent 的运行时规则，作为 **引擎内约束的权威引用源**。
>
> **版本历史**：v0.8.0 初稿（计划阶段），v0.9.8 补充实稿（与实际代码对齐）。

---

## §1 父子隔离

**子 Agent 与父 Agent 在运行时层面完全隔离。**

1. **内存隔离**：子 Agent 拥有独立的 `VarStore`（`variable.New()`），不继承父 Agent 的变量空间。
2. **goroutine 隔离**：子 `RunLoop` 在独立的 goroutine 中运行，事件写入本地 channel，对父端不可见。
3. **消息隔离**：子 Agent 不继承父 Agent 的消息历史。子 Agent 的输入仅由以下组成：
   - `SpawnSpec.Task` 作为子 Agent 的 `UserMessage`
   - `SpawnSpec.SystemAddendum` 合并到子 Agent 的 `SystemInstructions`
   - `SpawnSpec.MemoryDigest`（可选）作为 `<short_term_memory>` XML 块注入
4. **事件隔离**：子 Agent 内部的事件（`EventAnswer`、`EventCallLLMStart/End`、`EventToolCallStart/End`）不直接转发到父端事件流。仅 `EventStart` / `EventQueryEnd` 通过 `SpawnSpec.OutputCh` 非阻塞转发为 `spawn_start` / `spawn_end`。
5. **工具集隔离**：子 Agent 的工具集通过 `SpawnSpec.ToolAllowlist` / `ToolBlocklist` 双名单过滤（见 §7）。`spawn_subagent` 工具本身不参与双名单过滤。

### 对应实现

`DefaultSpawner.Spawn()`（`pkg/agent/spawn.go`）的事件循环中选择性转发事件。

---

## §2 父终态 → 子级联退出

**父 Agent 进入终态时，所有活跃的子 Agent 必须立即退出，不得等待当前回合完成。**

1. **触发条件**：父 `RunLoop` 返回（正常完成、达到 max_steps、发生不可恢复错误）或父 context 取消。
2. **作用范围**：所有深度、所有生命周期模式的子 Agent（包括 `ephemeral` 和预留的 `linger`）。
3. **退出方式**：通过 SpawnHandle 的 `Shutdown(reason)` 调用子 context 的 cancel 函数，goroutine 内的 select 分支收到 `childCtx.Done()` 后立即退出。
4. **结果定义**：子 Agent 未在取消前产生 `EventQueryEnd` 的，SpawnResult 以 `no_outcome`（父正常退出）或 `parent_cancelled`（父被取消）标记失败，并发射 `spawn_end(OK=false, ErrorCode=parent_cancelled)`。
5. **看门狗机制**（`TerminateWithGuard`）：父终态时启动看门狗 goroutine，确保在超时后强制终止子 process。

### 对应实现

`engine.RunState.Terminate(reason)` / `TerminateWithGuard()`（`internal/engine/runstate.go`），`ChildRegistry.TerminateAll()`（`internal/engine/child_registry.go`），`DefaultSpawner.Spawn()` 事件循环中的 `childCtx.Done()` 分支。

---

## §3 生命周期

**子 Agent 的生命周期由 `SpawnSpec.Lifecycle` 控制，当前仅实现 `ephemeral`。**

| 生命周期 | 语义 | 实现状态 |
|---------|------|---------|
| `ephemeral` | 子 Agent 结束后立即释放所有资源 | v0.9.0+ 已实现 |
| `linger` | 子 Agent 结束后保留/挂起，可被父 Agent 后续唤醒 | 预留，v0.9.x 未实现 |

1. **`ephemeral` 行为**：子 Agent 的 `RunLoop` 返回后，`ChildRegistry` 立即 `Unregister` 该 child handle；`SpawnResult` 作为工具结果回注到父 LLM。
2. **`linger` 拒绝**：传递 `spec.Lifecycle == LifecycleLinger` 时，`DefaultSpawner.Spawn()` 返回 `SpawnRejected` + `Error{Code: "unsupported_lifecycle", Message: "linger is not implemented in this version"}`。

### 对应实现

`DefaultSpawner.Spawn()` 开头的生命周期校验（`pkg/agent/spawn.go:#L51-L66`）。

---

## §4 子 Agent 异步、不阻塞父主路径

**子 Agent 在独立 goroutine 中运行，父 Agent 的主路径不得长期同步阻塞等待子 Agent 完成。**

1. **同步路径**（父 `eventCh == nil`）：`DefaultSpawner.Spawn()` 在当前 goroutine 中阻塞等待子 `RunLoop` 完成，直到子返回 `SpawnResult`。此路径下父 Agent 在一轮 ReAct 步骤中原地等待子结果。
2. **异步路径**（父 `eventCh != nil`）：`spawn_tool.go` handler 调用 `loop.AddAsyncSpawn()` 后启动 goroutine 运行 `sp.Spawn()`，立即返回占位 `SpawnResult`，父 LLM 继续推理。
   - 父 Agent 在每一轮无 tool_calls 时调用 `WaitAsyncSpawns(ctx)` 等待所有后台子 Agent 完成
   - 调用 `FlushAsyncSpawnResults()` 取出子结果，注入为 user message 让父 LLM 总结
   - 父 Agent 生命周期结束时自动等待未完成的异步 spawn

### 对应实现

`buildSpawnSubagentVarTool` 的 `eventCh` 分支（`pkg/agent/spawn_tool.go`），`LoopState.AddAsyncSpawn()` / `DoneAsyncSpawn()` / `WaitAsyncSpawns()` / `FlushAsyncSpawnResults()`（`pkg/agent/loop_state.go`）。

---

## §5 嵌套 spawn 授权

**嵌套 spawn（子 Agent 再派生子 Agent）默认禁止，由父 Agent 在 `SpawnSpec.AllowChildSpawn` 中显式逐级授权。**

1. **默认值**：`SpawnSpec.AllowChildSpawn` 默认为 `false`。
2. **非授权行为**：子 Agent 继承父 Agent 的 `ChildAgentBuilder`，但 `buildSpawnSubagentVarTool()` 在 `AllowChildSpawn == false` 时不注册 `spawn_subagent` 工具到子 Agent 的工具表。子 Agent 的 LLM 即使想 spawn 也没有对应的工具可以调用。
3. **授权行为**：`AllowChildSpawn == true` 时子 Agent 有权注册 `spawn_subagent` 工具，但依然受全局 `MaxDepth` 约束（默认 2，即 root + 一层子 Agent；深度=3 时拒绝）。
4. **非传递性**：`AllowChildSpawn` 不传递继承——父授权子可以 spawn，但子的子是否需要授权由子的 `SpawnSpec.AllowChildSpawn` 决定。

### 对应实现

`buildSpawnSubagentVarTool()` 的 `SpawnEnabled` / `ChildAgentBuilder` 检查（`pkg/agent/spawn_tool.go:#L54-L56`），`SpawnSpec.AllowChildSpawn` 字段（`pkg/runtime/exchange/exchange.go`）。

---

## §6 子 LLM 强制非流式

**子 Agent 的 ChatModel 在构造时被 `WrapNonStream` 包装，所有 LLM 调用强制 `stream=false`，与父配置解耦。**

1. **实现**：`DefaultSpawner.Spawn()` 在子 Agent 构造后立即执行：
   ```go
   if childTmpl.ChatModel != nil {
       childTmpl.ChatModel = model.WrapNonStream(childTmpl.ChatModel)
   }
   ```
2. **不可配置**：`SpawnSpec.AllowStream` 字段存在但不对外开放，JSON Schema 中标记为 `json:"-"`。v0.9.x 始终强制非流式。
3. **原因**：
   - 子 Agent 的事件流不进入父端，父端没有流式消费的必要
   - 强制非流式降低网络开销和 token 消耗
   - 降低子 Agent 的事件通道负载（单条非流式消息 vs 多条流式 delta）

### 对应实现

`DefaultSpawner.Spawn()` 中的 `WrapNonStream` 调用（`pkg/agent/spawn.go:#L83-L85`），`model.WrapNonStream`（`pkg/model/nonstream.go`）。

---

## §7 子工具集与 ReAct 聚合

**子 Agent 的工具集与父同源，通过双名单过滤；子 ReAct 中间步骤不回填父端事件流。**

### 7.1 工具集来源

子 Agent 的工具集继承父 Agent 的工具构建路径：MCP 工具 + 内置工具（`var_set`、`execute_shell_script`、`load_skill` 等）+ 技能工具。`spawn_subagent` 工具根据 `AllowChildSpawn` 决定是否注册到子 Agent。

### 7.2 双名单过滤

| 名单 | 语义 | 默认值 |
|------|------|--------|
| `SpawnSpec.ToolAllowlist` | 仅允许列表中的工具名 | nil（= 不限制）|
| `SpawnSpec.ToolBlocklist` | 禁止列表中的工具名 | nil（= 不限制）|

**过滤顺序**：白先黑后。工具名按白名单放行 → 再从白名单结果中剔除黑名单。`spawn_subagent` 工具本身不参与双名单过滤（始终可用或不可用由 `AllowChildSpawn` 决定）。

**glob 通配支持**：名单条目支持 `*` / `?` 通配符，使用 `path/filepath.Match` 匹配。例如 `mcp:foo/*` 匹配所有以 `mcp:foo/` 开头的工具名。

### 7.3 ReAct 中间步骤隔离

子 Agent 在 ReAct 循环中的中间步骤**不**回填到父端：

| 步骤类型 | 父端可见性 | 处理方式 |
|---------|-----------|---------|
| `EventAnswer`（流式文本） | 🚫 不可见 | 在本地事件循环中丢弃 |
| `EventCallLLMStart/End` | 🚫 不可见 | 丢弃 |
| `EventToolCallStart/End` | 🚫 仅本地记录 | 按调用顺序聚合为 `ChildToolCalls`，存入 `SpawnResult.ChildToolCalls` |
| `EventStart` / `EventQueryEnd` | ✅ 可见 | 转发为 `spawn_start` / `spawn_end` |

### 7.4 ChildToolCall 聚合

子 Agent 每次工具调用的名称、参数、输出被依次记录为 `exchange.ChildToolCall`：

```go
type ChildToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Output    string `json:"output,omitempty"`
	IsError   bool   `json:"is_error"`
}
```

聚合后的 `[]ChildToolCall` 存入 `SpawnResult.ChildToolCalls`，供父 Agent 决定是否外发。

### 对应实现

`DefaultSpawner.Spawn()` 事件循环中的 tool_call 记录逻辑（`pkg/agent/spawn.go:#L187-L202`），`SpawnSpec.ToolAllowlist` / `ToolBlocklist`（`pkg/runtime/exchange/exchange.go`）。

---

## 变更记录

| 日期 | 说明 |
|------|------|
| 2026-05-01 | 补充实稿：与实际代码对齐，覆盖 §1–§7 全部运行时规则，引用 v0.9.8 事件拆分后的 `spawn_start` / `spawn_end`。 |

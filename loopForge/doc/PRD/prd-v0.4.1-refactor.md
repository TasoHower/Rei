# loopForge 产品需求文档 — v0.4.1 包拆分 + 命名统一 + 结构化日志

> **版本**：v0.4.1  
> **日期**：2026-04-16  
> **里程碑**：包拆分 + 命名统一 + 结构化日志  
> **状态**：已完成  
> **关联文档**：[progress-v0.4.1.md](../log/progress-v0.4.1.md)、[progress-v0.4.0.md](../log/progress-v0.4.0.md)

---

## 1. 概述

### 1.1 产品定位

**v0.4.1** 是 loopForge 的**代码质量与工程化改进版本**，解决 v0.4.0 遗留的三个结构性问题：单包过载、命名混淆、日志缺失。通过包拆分、命名统一和结构化日志，提升代码可维护性和可观测性。

### 1.2 核心价值

1. **职责分离** — Agent 定义与 Runner 编排器分离，各司其职
2. **命名清晰** — 接口、结构体、包名统一且无歧义
3. **开箱可观测** — 结构化日志（slog）默认开启，无需配置
4. **Go 风格** — 遵循 Go 命名惯例（`-able` 接口、单一职责包）

### 1.3 与前后版本的关系

| 版本 | 关系说明 |
|------|----------|
| **v0.4.0** | Agent 架构统一（Transfer 能力下沉）；v0.4.1 在此基础上进行包拆分和命名统一 |
| **v0.5.0** | Variable 共享变量；v0.4.1 的代码结构为 v0.5.0 的 `pkg/variable` 独立包提供范例 |

---

## 2. 需求范围

### 2.1 本版本交付（v0.4.1）

#### P0（必须实现）

1. **包拆分 — Agent 与 Runner 分离**
   - `pkg/agent` 保留：接口 + 完整实现 + transfer 能力
   - `pkg/runner` 独立：只有编排器（取一个 Agent 跑起来）
   - Transfer 函数导出（供 runner 包跨包调用）

2. **命名统一**
   - 接口 `Agent` → `Runnable`（单方法 `Run` 的契约）
   - 结构体 `RunnerAgent` → `Agent`（用户创建和配置的主体）
   - 构造函数 `NewRunnerAgent()` → `New()`
   - Option 类型 `RunnerOption` → `Option`
   - 文件名去掉 `runner_` 前缀

3. **结构化日志**
   - `pkg/log` 独立包（Logger 接口全局复用）
   - 默认真实 logger（不传 `WithLogger` 时绑定 `slog.Default()`）
   - `*slog.Logger` 直接满足 `Logger` 接口（零适配）

#### P1（可选增强）

- 文档完善（包文档、示例代码）
- 代码审查清单

### 2.2 不在本版本范围

1. **功能新增** — 本版本仅重构，不新增功能
2. **API 破坏性变更** — 保持向后兼容（通过类型别名等）
3. **性能优化** — 性能优化留待后续版本

---

## 3. 用户故事

### 3.1 作为开发者，我希望...

**US-1**：能够清晰地理解包职责  
**验收标准**：
- `pkg/agent` 只包含 Agent 定义和实现
- `pkg/runner` 只包含编排器
- `pkg/transfer` 只包含 Transfer 编排
- 每个包的文件数 < 10 个

**US-2**：能够使用符合 Go 惯例的命名  
**验收标准**：
- 接口名遵循 `-able` 惯例（如 `Runnable`）
- 结构体使用具体名称（如 `Agent`）
- 构造函数简洁（如 `New()`）
- Option 类型统一（如 `Option`）

**US-3**：能够开箱即用地使用结构化日志  
**验收标准**：
- 不传 logger 时默认输出到 `slog.Default()`
- logger 接口与 slog 完全一致（零适配）
- 可自定义 logger（通过 `WithLogger` Option）

**US-4**：能够跨包调用 Transfer 函数  
**验收标准**：
- Transfer 相关函数全部导出（大写开头）
- 函数命名清晰（如 `TransferToolPrefix`、`BuildTransferTools`）
- 文档完整（godoc 注释）

### 3.2 作为 Agent，我希望...

**US-5**：能够在清晰的代码结构中运行  
**验收标准**：
- Agent 定义集中在 `pkg/agent`
- Runner 编排逻辑在 `pkg/runner`
- Transfer 逻辑在 `pkg/transfer`
- 无循环 import

---

## 4. 功能需求

### 4.1 包拆分

#### 4.1.1 Agent 保留内容

**`pkg/agent/`**：
- `interface.go` — `Runnable` 接口
- `agent.go` — `Agent` 结构体（原 `runner.go`）
- `options.go` — `New()` + `Option` + `With*`（原 `runner_options.go`）
- `loop.go` — `RunLoop`（原 `runner_loop.go`）
- `helpers.go` — `bindModel` 等辅助（原 `runner_helpers.go`）
- `stream.go` — `consumeStream`（原 `runner_stream.go`）
- `tools.go` — `executeToolCalls`（原 `runner_tools.go`）
- `transfer.go` — Transfer 能力（函数全部导出）

#### 4.1.2 Runner 独立内容

**`pkg/runner/`**：
- `runner.go` — `Runner` + `NewRunner` + `WithMaxTransfers` + `runTransferLoop`
- `doc.go` — 包文档

#### 4.1.3 Transfer 函数导出

| 旧名（未导出） | 新名（导出） | 说明 |
|----------------|-------------|------|
| `transferToolPrefix` | `TransferToolPrefix` | Transfer 工具前缀 |
| `buildTransferTools` | `BuildTransferTools` | 构建 Transfer 工具列表 |
| `buildTransferPrompt` | `BuildTransferPrompt` | 构建 Transfer prompt |
| `isTransferTool` | `IsTransferTool` | 判断是否为 Transfer 工具 |
| `targetAgent` | `TargetAgent` | 提取目标 Agent 名 |
| `extractReason` | `ExtractReason` | 提取 Transfer 原因 |

### 4.2 命名统一

#### 4.2.1 类型重命名

| 旧名 | 新名 | 说明 |
|------|------|------|
| `agent.Agent` (interface) | `agent.Runnable` | 单方法 `Run` 的契约 |
| `agent.RunnerAgent` (struct) | `agent.Agent` | 用户创建和配置的主体 |
| `agent.NewRunnerAgent()` | `agent.New()` | 构造函数 |
| `agent.RunnerOption` | `agent.Option` | 配置选项类型 |

#### 4.2.2 文件名清理

| 旧文件名 | 新文件名 |
|----------|----------|
| `runner.go` | `agent.go` |
| `runner_options.go` | `options.go` |
| `runner_loop.go` | `loop.go` |
| `runner_helpers.go` | `helpers.go` |
| `runner_stream.go` | `stream.go` |
| `runner_tools.go` | `tools.go` |

### 4.3 结构化日志

#### 4.3.1 Logger 接口

```go
// Logger is the minimal interface for structured logging.
// *slog.Logger satisfies this interface directly.
type Logger interface {
    Info(msg string, keysAndValues ...any)
    Error(msg string, keysAndValues ...any)
    Debug(msg string, keysAndValues ...any)
}
```

#### 4.3.2 默认 Logger

```go
// NewRunnerAgent 默认行为：
func New(opts ...Option) *Agent {
    a := &Agent{
        // ...
        logger: slog.Default(), // 默认绑定 slog.Default()
    }
    // ...
    return a
}
```

#### 4.3.3 自定义 Logger

```go
// 通过 WithLogger Option 自定义：
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

agent := agent.New(
    agent.WithLogger(logger),
)
```

### 4.4 设计决策

#### 4.4.1 Transfer 留在 `pkg/agent`

**理由**：
- Transfer 是 Agent 的固有能力（handoff 注册、transfer tool 生成）
- 不应随 Runner 搬走
- 保持 Agent 的完整性

#### 4.4.2 Runner 独立为 `pkg/runner`

**理由**：
- Runner 只是编排器——取一个 Agent 跑起来
- 职责单一，与 Agent 定义解耦
- 便于未来扩展（如支持多种编排策略）

#### 4.4.3 接口 `Agent` → `Runnable`

**理由**：
- 单方法 `Run` 遵循 Go `-able` 命名惯例（如 `io.Reader`/`fmt.Stringer`）
- 释放 `Agent` 给结构体（更自然的命名）
- 符合 Go 社区惯例

#### 4.4.4 默认真实 logger

**理由**：
- 不传 `WithLogger` 时绑定 `slog.Default()`
- 开箱即有可观测性
- 避免静默丢弃日志

#### 4.4.5 `*slog.Logger` 直接满足 `Logger`

**理由**：
- 接口签名 `(msg string, keysAndValues ...any)` 与 slog 完全一致
- 零适配成本
- 无需包装层

---

## 5. 技术需求

### 5.1 包依赖关系

```
pkg/agent
    ├── pkg/model（ChatModel 抽象）
    ├── pkg/runtime/event（事件类型）
    ├── pkg/runtime/outcome（结果类型）
    └── pkg/log（可选，默认使用 slog）

pkg/runner
    ├── pkg/agent（使用 Agent）
    ├── pkg/transfer（Transfer 编排）
    └── pkg/log（可选）

pkg/transfer
    ├── pkg/agent（使用 Agent）
    └── pkg/log（可选）
```

### 5.2 导出 API

#### 5.2.1 pkg/agent

```go
// 类型
type Runnable interface {
    Run(ctx context.Context, req *Request) (<-chan *event.RuntimeEvent, error)
}

type Agent struct {
    // ...
}

// 构造函数
func New(chatModel model.ToolCallingChatModel, opts ...Option) *Agent

// Option
type Option func(*Agent)
func WithName(name string) Option
func WithSystemInstructions(instructions string) Option
func WithTools(tools []*model.ToolInfo) Option
func WithLogger(logger Logger) Option
// ...

// Transfer 函数（导出）
func TransferToolPrefix() string
func BuildTransferTools(current *Agent, allAgents []*Agent) []*model.ToolInfo
func BuildTransferPrompt(current *Agent, target *Agent) string
func IsTransferTool(tool *model.ToolInfo) bool
func TargetAgent(toolCall *model.ToolCall) string
func ExtractReason(toolCall *model.ToolCall) string
```

#### 5.2.2 pkg/runner

```go
// 类型
type Runner struct {
    // ...
}

// 构造函数
func NewRunner(entry *agent.Agent, opts ...Option) *Runner

// Option
type Option func(*Runner)
func WithMaxTransfers(max int) Option
func WithLogger(logger Logger) Option
```

#### 5.2.3 pkg/transfer

```go
// 类型
type Orchestrator struct {
    // ...
}

// 构造函数
func NewOrchestrator(entry *agent.Agent, registry *Registry, opts ...Option) *Orchestrator

// Registry
type Registry struct {
    // ...
}
func NewRegistry() *Registry
func (r *Registry) Register(config *AgentConfig)
func (r *Registry) Get(name string) *AgentConfig
func (r *Registry) Names() []string
func (r *Registry) TransferTargetsFor(name string) []string
func (r *Registry) Validate() error
```

### 5.3 向后兼容性

#### 5.3.1 类型别名（可选）

```go
// 如需保持向后兼容：
type RunnerAgent = Agent  // 旧名指向新类型
type RunnerOption = Option  // 旧名指向新类型

// 构造函数别名：
func NewRunnerAgent(chatModel model.ToolCallingChatModel, opts ...RunnerOption) *Agent {
    return New(chatModel, opts...)
}
```

---

## 6. 验收标准

### 6.1 功能验收

- [ ] `pkg/agent` 只包含 Agent 定义和实现
- [ ] `pkg/runner` 只包含编排器
- [ ] `pkg/transfer` 只包含 Transfer 编排
- [ ] 无循环 import
- [ ] 所有 Transfer 函数可跨包调用

### 6.2 命名验收

- [ ] 接口名遵循 `-able` 惯例
- [ ] 结构体使用具体名称
- [ ] 构造函数简洁
- [ ] Option 类型统一
- [ ] 文件名无 `runner_` 前缀

### 6.3 日志验收

- [ ] 默认输出到 `slog.Default()`
- [ ] 可自定义 logger
- [ ] Logger 接口与 slog 完全一致
- [ ] Runner 编排过程有日志输出

### 6.4 代码质量验收

- [ ] 每个包的文件数 < 10 个
- [ ] 导出函数有 godoc 注释
- [ ] 无重复代码
- [ ] 通过 `go vet` 和 `staticcheck`

---

## 7. 风险与待决

| 项 | 说明 | 缓解措施 |
|----|------|----------|
| **破坏性变更** | 类型重命名可能导致现有代码编译失败 | 提供类型别名、迁移指南 |
| **包导入路径变更** | 调用方需更新 import 路径 | 文档说明、示例代码更新 |
| **学习成本** | 新命名需要适应期 | 命名符合 Go 惯例、文档清晰 |
| **重构风险** | 大规模重构可能引入 bug | 充分测试、代码审查 |

---

## 8. 参考文档

- [progress-v0.4.1.md](../log/progress-v0.4.1.md) — 版本进度日志
- [progress-v0.4.0.md](../log/progress-v0.4.0.md) — 前置版本
- [Effective Go](https://golang.org/doc/effective_go) — Go 编程惯例
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments) — Go 代码审查建议

---

## 变更日志

| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-16 | v0.4.1 | 初始版本 |

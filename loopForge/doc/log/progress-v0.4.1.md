# loopForge 项目进度日志 — v0.4.1

> **版本**：v0.4.1  
> **日期**：2026-04-16  
> **里程碑**：包拆分 + 命名统一 + 结构化日志  
> **上一版本**：v0.4.0（Agent 架构统一 — Transfer 能力下沉、Runner 合并至 pkg/agent）

---

## 本版本目标

**解决 v0.4.0 遗留的三个结构性问题**：

1. **单包过载**：`pkg/agent/` 既定义接口契约（`Agent`、`Planner`），又承载全部具体实现（`RunnerAgent`、`Runner`、循环/流/工具/转发），导致包职责模糊、文件过多（15 个）。
2. **命名混淆**：结构体 `RunnerAgent` 与新增的 `pkg/runner` 包名极易混淆；接口 `Agent` 占用了最自然的结构体名称。
3. **日志缺失**：Runner 编排过程（transfer loop、错误、转发）无任何可观测输出，排障全靠猜。

---

## 设计决策

| 决策 | 说明 |
|------|------|
| **Transfer 留在 `pkg/agent`** | Transfer 是 Agent 的固有能力（handoff 注册、transfer tool 生成），不应随 Runner 搬走 |
| **Runner 独立为 `pkg/runner`** | Runner 只是编排器——取一个 Agent 跑起来。职责单一，与 Agent 定义解耦 |
| **接口 `Agent` → `Runnable`** | 单方法 `Run` 遵循 Go `-able` 命名惯例（如 `io.Reader`/`fmt.Stringer`），释放 `Agent` 给结构体 |
| **`pkg/log` 独立包** | Logger 接口全局复用（Runner、未来 Agent 也可用），不绑定在 runner 包内 |
| **默认真实 logger** | 不传 `WithLogger` 时绑定 `slog.Default()`，而非静默丢弃。开箱即有可观测性 |
| **`*slog.Logger` 直接满足 `Logger`** | 接口签名 `(msg string, keysAndValues ...any)` 与 slog 完全一致，零适配 |

---

## 交付清单

### Step 1：包拆分 — Agent 与 Runner 分离

**Agent 保留**：接口 + 完整实现 + transfer 能力

- [x] `pkg/agent/interface.go` — `Runnable` 接口
- [x] `pkg/agent/agent.go` — `Agent` 结构体（原 `runner.go`）
- [x] `pkg/agent/options.go` — `New()` + `Option` + `With*`（原 `runner_options.go`）
- [x] `pkg/agent/loop.go` — `RunLoop`（原 `runner_loop.go`）
- [x] `pkg/agent/helpers.go` — `bindModel` 等辅助（原 `runner_helpers.go`）
- [x] `pkg/agent/stream.go` — `consumeStream`（原 `runner_stream.go`）
- [x] `pkg/agent/tools.go` — `executeToolCalls`（原 `runner_tools.go`）
- [x] `pkg/agent/transfer.go` — Transfer 能力（函数全部导出）

**Runner 搬出**：只有编排器

- [x] `pkg/runner/runner.go` — `Runner` + `NewRunner` + `WithMaxTransfers` + `runTransferLoop`
- [x] `pkg/runner/doc.go`

**Transfer 函数导出**（供 runner 包跨包调用）：

| 旧名（未导出） | 新名（导出） |
|----------------|-------------|
| `transferToolPrefix` | `TransferToolPrefix` |
| `buildTransferTools` | `BuildTransferTools` |
| `buildTransferPrompt` | `BuildTransferPrompt` |
| `isTransferTool` | `IsTransferTool` |
| `targetAgent` | `TargetAgent` |
| `extractReason` | `ExtractReason` |

### Step 2：命名统一

| 旧名 | 新名 | 说明 |
|------|------|------|
| `agent.Agent` (interface) | `agent.Runnable` | 单方法 `Run` 的契约 |
| `agent.RunnerAgent` (struct) | `agent.Agent` | 用户创建和配置的主体 |
| `agent.NewRunnerAgent()` | `agent.New()` | 构造函数 |
| `agent.RunnerOption` | `agent.Option` | 配置选项类型 |

文件名同步清理（去掉 `runner_` 前缀）：

| 旧文件名 | 新文件名 |
|----------|----------|
| `runner.go` | `agent.go` |
| `runner_options.go` | `options.go` |
| `runner_loop.go` | `loop.go` |
| `runner_helpers.go` | `helpers.go` |
| `runner_stream.go` | `stream.go` |
| `runner_tools.go` | `tools.go` |
| `runner_agent_tool_test.go` | `agent_tool_test.go` |

### Step 3：`pkg/log` — 结构化日志包

- [x] `Logger` 接口：`Debug/Info/Warn/Error(msg string, keysAndValues ...any)`
- [x] `Default()` — 返回 `slog.Default()`（`*slog.Logger` 直接满足接口）
- [x] `Nop()` — 静默 logger
- [x] `FromSugared()` — 适配 `*zap.SugaredLogger`（零 zap 依赖，通过内部接口隐式匹配）

### Step 4：Runner 集成 Logger

- [x] `Runner.logger` 字段，默认 `log.Default()`
- [x] `WithLogger(log.Logger)` 选项
- [x] 关键编排点日志覆盖：

| 级别 | 日志点 |
|------|--------|
| `Debug` | run 启动（single/transfer 模式）、每个 agent 开始执行 |
| `Info` | transfer loop 启动、每次 agent 转发、loop 正常结束 |
| `Warn` | 超过 max transfers 限制 |
| `Error` | entry agent 为 nil、无效转发目标 |

### Step 5：test-server 集成 Logger

- [x] `main()` 使用 `slog.Default()` 作为全局 logger
- [x] `handleChat` 接收 `log.Logger`，请求进出打日志
- [x] `buildTransferAgent` 传递 logger 给 Runner
- [x] 关闭 hertz 默认日志噪音（`hlog.SetSilentMode`）

### Step 6：外部调用方适配

- [x] `cmd/agentdemo/main.go` — `agent.New()`、`agent.Runnable`、`runner.NewRunner`
- [x] `test-server/main.go` — 同上 + logger 集成
- [x] `internal/engine/engine.go` — `agent.Runnable`
- [x] `internal/engine/planner.go` — 无变更

### Step 7：测试

- [x] `pkg/agent/` 5 个测试全部通过（clone 3 + tool loop 2）
- [x] `pkg/runner/` 5 个测试全部通过（transfer 4 + 并发 1）
- [x] `go build ./...` 全量编译通过
- [x] `test-server` 编译通过

---

## 重构前后对比

### 包结构

```
Before (v0.4.0)                              After (v0.4.1)
─────────────                                ─────────────
pkg/agent/     (15 个文件，全部堆一起)         pkg/agent/     (13 个文件，Agent + 能力)
├── interface.go    Agent 接口                ├── interface.go    Runnable 接口
├── runner.go       RunnerAgent 结构体        ├── agent.go        Agent 结构体
├── runner_options.go                         ├── options.go      New() + Option
├── runner_loop.go                            ├── loop.go         RunLoop
├── runner_helpers.go                         ├── helpers.go
├── runner_stream.go                          ├── stream.go
├── runner_tools.go                           ├── tools.go
├── transfer.go     未导出函数                ├── transfer.go     导出函数
├── planner.go                                ├── planner.go
├── run.go          Runner 编排器             │
├── run_test.go     Runner 测试              │
├── ...                                       └── ...
                                              
（无 runner 包）                               pkg/runner/    (4 个文件，纯编排)
                                              ├── doc.go
                                              ├── runner.go       Runner + WithLogger
                                              ├── runner_test.go
                                              └── helpers_test.go
                                              
（无 log 包）                                  pkg/log/       (1 个文件)
                                              └── log.go          Logger + Default/Nop/FromSugared
```

### API 变化

```go
// Before (v0.4.0)
triage := agent.NewRunnerAgent(chat, agent.WithName("triage"), ...)
r := agent.NewRunner(triage, agent.WithMaxTransfers(5))
func streamAndPrint(ctx context.Context, ag agent.Agent, cfg demoConfig)

// After (v0.4.1)
triage := agent.New(chat, agent.WithName("triage"), ...)
r := runner.NewRunner(triage, runner.WithMaxTransfers(5), runner.WithLogger(log.Default()))
func streamAndPrint(ctx context.Context, ag agent.Runnable, cfg demoConfig)
```

### Logger 用法

```go
// 默认 — slog 输出到 stderr（开箱即用）
r := runner.NewRunner(entry)

// zap — 零外部依赖适配
r := runner.NewRunner(entry, runner.WithLogger(log.FromSugared(zapLogger.Sugar())))

// 静默
r := runner.NewRunner(entry, runner.WithLogger(log.Nop()))

// 自定义实现
type myLogger struct{}
func (myLogger) Debug(msg string, kv ...any) { ... }
func (myLogger) Info(msg string, kv ...any)  { ... }
func (myLogger) Warn(msg string, kv ...any)  { ... }
func (myLogger) Error(msg string, kv ...any) { ... }
```

### Logger 实际输出示例

```
2026/04/16 13:04:06 INFO transfer loop started run_id=test-transfer entry_agent=triage max_transfers=5
2026/04/16 13:04:06 INFO agent transfer run_id=test-transfer from=triage to=expert reason="user needs math help"
2026/04/16 13:04:06 INFO transfer loop completed run_id=test-transfer last_agent=expert total_transfers=1
2026/04/16 13:04:06 WARN max transfers exceeded run_id=test-loop limit=3
```

---

## 依赖关系图

```
pkg/log
  └── log/slog（标准库）

pkg/agent
  ├── pkg/model
  ├── pkg/tool
  └── pkg/runtime/{event,outcome,request}

pkg/runner
  ├── pkg/agent    （Agent 结构体 + Transfer 能力）
  ├── pkg/log      （Logger 接口）
  ├── pkg/model
  └── pkg/runtime/{event,outcome,request}

cmd/agentdemo
  ├── pkg/agent
  └── pkg/runner

test-server
  ├── pkg/agent
  ├── pkg/log
  └── pkg/runner
```

无循环依赖。`pkg/agent` 不依赖 `pkg/runner` 或 `pkg/log`。

---

## 文件变更总览

| 路径 | 操作 | 说明 |
|------|------|------|
| `pkg/agent/agent.go` | **新建** | Agent 结构体（原 `runner.go` 重命名 + RunnerAgent→Agent） |
| `pkg/agent/options.go` | **新建** | New() + Option（原 `runner_options.go` 重命名 + 重命名） |
| `pkg/agent/loop.go` | **重命名** | 原 `runner_loop.go`，receiver *RunnerAgent→*Agent |
| `pkg/agent/helpers.go` | **重命名** | 原 `runner_helpers.go`，receiver 同上 |
| `pkg/agent/stream.go` | **重命名** | 原 `runner_stream.go` |
| `pkg/agent/tools.go` | **重命名** | 原 `runner_tools.go` |
| `pkg/agent/interface.go` | 修改 | Agent interface → Runnable interface |
| `pkg/agent/transfer.go` | 修改 | 6 个函数/常量导出 + receiver 重命名 |
| `pkg/agent/doc.go` | 修改 | 更新包文档 |
| `pkg/agent/agent_tool_test.go` | **新建** | 原 `runner_agent_tool_test.go`，适配新 API |
| `pkg/agent/clone_test.go` | 修改 | RunnerAgent→Agent |
| `pkg/agent/run_helpers_test.go` | 修改 | TransferToolPrefix 大写 |
| `pkg/agent/runner.go` | **删除** | 搬至 agent.go |
| `pkg/agent/runner_options.go` | **删除** | 搬至 options.go |
| `pkg/agent/runner_loop.go` | **删除** | 搬至 loop.go |
| `pkg/agent/runner_helpers.go` | **删除** | 搬至 helpers.go |
| `pkg/agent/runner_stream.go` | **删除** | 搬至 stream.go |
| `pkg/agent/runner_tools.go` | **删除** | 搬至 tools.go |
| `pkg/agent/runner_agent_tool_test.go` | **删除** | 搬至 agent_tool_test.go |
| `pkg/agent/run.go` | **删除** | Runner 搬至 pkg/runner |
| `pkg/agent/run_test.go` | **删除** | Runner 测试搬至 pkg/runner |
| `pkg/runner/doc.go` | **新建** | 包文档 |
| `pkg/runner/runner.go` | **新建** | Runner 编排器 + Logger 集成 |
| `pkg/runner/runner_test.go` | **新建** | Runner 测试（5 用例） |
| `pkg/runner/helpers_test.go` | **新建** | 测试 mock + 辅助函数 |
| `pkg/log/log.go` | **新建** | Logger 接口 + Default/Nop/FromSugared |
| `cmd/agentdemo/main.go` | 修改 | 适配新 API + import runner |
| `test-server/main.go` | 修改 | 适配新 API + logger 集成 |
| `internal/engine/engine.go` | 修改 | agent.Agent → agent.Runnable |

---

## 下一步计划

### v0.5.0 — Agent 运行时增强

| 优先级 | 方向 | 说明 |
|--------|------|------|
| **P0** | **Agent Logger** | `pkg/agent` 也接入 `log.Logger`，RunLoop 内的 LLM 调用、工具执行、错误路径全部可观测 |
| **P0** | **Guardrails（护栏）** | 输入/输出校验钩子（敏感词过滤、格式约束、长度限制），RunLoop 前后执行 |
| **P0** | **Context / Memory** | 短期对话历史管理（滑动窗口 + token 预算），为多轮对话铺路 |
| **P1** | **Spawn（子任务）** | 父子递归 Agent 调用，`SpawnStart`/`SpawnEnd` 事件，MaxDepth 控制 |
| **P1** | **MCP Client** | `internal/mcp` 实现 stdio / streamable-http transport，`tools/list` → ToolRegistry 自动注册 |
| **P2** | **Cost Tracing** | token usage 采集 + Run 树聚合 + 成本估算 |
| **P2** | **Reflection / Retry** | 工具调用失败后模型自反思重试 |

---

## 提交记录（开发日志）

| 日期 | 说明 |
|------|------|
| 2026-04-16 | **包拆分**：`pkg/agent`（Agent + 能力）与 `pkg/runner`（Runner 编排器）分离。Transfer 留在 agent 包，函数全部导出。Runner 独立为纯编排层。 |
| 2026-04-16 | **命名统一**：`RunnerAgent` → `Agent`、`Agent` 接口 → `Runnable`、`NewRunnerAgent` → `New`、`RunnerOption` → `Option`。文件名去掉 `runner_` 前缀。 |
| 2026-04-16 | **结构化日志**：新建 `pkg/log` 包（Logger 接口 + slog 默认 + zap 适配）。Runner 默认绑定 `log.Default()`，关键编排点 Debug/Info/Warn/Error 全覆盖。 |
| 2026-04-16 | **test-server 集成 logger**：请求进出日志、Runner logger 传递、hertz 默认日志静默。 |
| 2026-04-16 | 版本日志初始化。10 个测试全部通过，全量编译通过。 |

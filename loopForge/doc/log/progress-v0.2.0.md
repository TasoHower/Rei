# loopForge 项目进度日志 — v0.2.0

> **版本**：v0.2.0  
> **日期**：2026-04-15  
> **里程碑**：流式事件驱动 — Agent.Run 返回 `<-chan *RuntimeEvent`，类型安全 Payload，多工具链式调用 demo  
> **上一版本**：v0.1.0（可运行基座）

---

## 本版本目标

将 Agent 从 **同步请求-响应** 模型升级为 **流式事件驱动** 模型：Run 返回 channel，所有中间过程（token、tool 调用、step）和最终结果（done）均通过 `RuntimeEvent` 流式输出。消除 `Payload any`，引入 sealed `EventPayload` interface 实现编译期类型安全。

---

## 交付清单

### 核心架构变更

- [x] **`Agent.Run` 流式化**：签名从 `(*outcome.RuntimeOutcome, error)` 改为 `<-chan *event.RuntimeEvent`；错误通过 `RuntimeEventError` 事件传递，不再通过 Go error 返回
- [x] **`EventPayload` sealed interface**：替代 `Payload any`；私有 `eventPayload()` 方法形成 sealed 约束，外部包无法实现
- [x] **`Emit` 构造器**：从 payload 自动推导 `Type`，消除手动拼装的冗余与不一致风险
- [x] **类型安全访问器**：`RuntimeEvent` 上新增 `.Token()` / `.ToolStart()` / `.ToolEnd()` / `.Done()` / `.Error()` / `.SpawnStart()` / `.SpawnEnd()` / `.StepEvent()` 方法，调用方无需手写类型断言

### Payload 字段增强

- [x] **`ToolStartPayload`**：新增 `Arguments` 字段，携带模型给出的原始 JSON 入参
- [x] **`ToolEndPayload`**：新增 `Output`（工具返回值/错误信息）和 `IsError`（工具是否报错）
- [x] **`StepPayload`**：新增具体类型，替代之前的 `nil` payload
- [x] **`RunMetrics` 扩展**：新增 `Model`（模型标识）、`TotalTokens`（总 token）、`Steps`（执行步数）

### Agent 配置

- [x] **`RunnerAgent.ModelName`**：新增字段 + `WithModelName` option，解决 metrics 中模型名丢失问题
- [x] **`modelName` 取值优先级**：`a.ModelName` → `req.Options.Model` 覆盖

### Demo

- [x] **多工具链式调用 demo**：四个算术工具 `add` / `subtract` / `multiply` / `divide`
- [x] **默认 prompt**：需要跨 3 步链式调用（add → multiply → subtract），覆盖多轮 tool-call 完整流程
- [x] **事件流可视化输出**：`── step N ──` 分隔，`→` / `←` 标记工具调用方向

### 测试

- [x] **`runner_agent_tool_test.go`**：更新为从 channel 消费事件，验证 `DonePayload`、`ToolStart`、`ToolEnd` 出现在事件流中

---

## 变更文件

| 路径 | 变更 |
|------|------|
| `pkg/agent/interface.go` | `Run` 返回 `<-chan *event.RuntimeEvent` |
| `pkg/agent/runner_agent.go` | goroutine + channel 流式发射，`runLoop` 重写 |
| `pkg/agent/runner_options.go` | 新增 `WithModelName` |
| `pkg/runtime/event/event.go` | `EventPayload` interface、sealed marker、`Emit` 构造器、类型安全访问器、`StepPayload` |
| `pkg/runtime/outcome/outcome.go` | `RunMetrics` 扩展 `Model` / `TotalTokens` / `Steps` |
| `pkg/agent/runner_agent_tool_test.go` | 适配流式 channel 消费 |
| `cmd/agentdemo/main.go` | 四工具链式 demo |
| `start.sh` | 清理残留环境变量 |

---

## 未包含（后续版本）

见下方 **路线图 TODO**。

---

## 路线图 TODO

### P0 — 核心能力（下一里程碑）

- [ ] **Multi-Agent spawn / join**：父子 Run 递归调用，`SpawnSpec` → `SpawnResult` 全流程，`SpawnStart` / `SpawnEnd` 事件，MaxDepth 控制
- [ ] **Tool whitelist / blacklist**：per-agent / per-spawn 的工具权限收敛，与 `ToolRegistry` 集成
- [ ] **Skill registry**：`SKILL.md` 指令注入 + MCP 能力发现统一注册，支持动态启停
- [ ] **Memory（short / long）**：短期 `[]Message` 滑动窗口 + 摘要；长期通过 MCP（SeRagLF）或本地 KV 持久化
- [ ] **MCP client**：`internal/mcp` 实现 stdio / streamable-http transport，`tools/list` 发现 → `ToolRegistry` 自动注册，`tools/call` 调用转发

### P1 — 增强与可观测

- [ ] **Self-RAG Planner**：模型自主决策是否检索，通过 MCP retrieve tool 实现 RAG-in-the-loop
- [ ] **Reflection / retry**：工具调用失败后模型自反思并重试，可配置 retry 策略与最大次数
- [ ] **Cost tracing**：从模型 adapter 层采集 usage（input/output tokens），沿 Run 树聚合到 `RunMetrics`，填充 `TotalCostUSD`
- [ ] **Span 可视化**：OpenTelemetry-style span 埋点（Run → Step → ToolCall），输出到 JSON / Jaeger / Dev UI

---

## 提交记录（开发日志）

| 日期 | 说明 |
|------|------|
| 2026-04-15 | `Agent.Run` 流式化（返回 `<-chan *RuntimeEvent`）；`EventPayload` sealed interface 替代 `any`；`Emit` 构造器 + 类型安全访问器；`ToolStartPayload` / `ToolEndPayload` 字段增强；`RunMetrics` 扩展；`WithModelName`；多工具链式 demo；测试适配。 |

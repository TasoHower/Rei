# Rei

**Go 语言实现的 Agent Development Kit（ADK）** —— 把「模型调用 · 工具执行 · 上下文压缩 · 审批 · 崩溃恢复」做成一层可嵌入的运行时，让宿主只负责驱动会话、消费事件。

```
module github.com/tasohower/rei   ·   Go 1.25   ·   零第三方依赖   ·   MIT
```

---

## 为什么需要 Rei

自己写一个 agent 循环，第一版通常 200 行就能跑起来。真正花时间的是后面这些事：

| 问题 | 朴素实现的后果 | Rei 的做法 |
|---|---|---|
| 上游 429 / 流中断 | 整轮失败，用户重发 | 错误语义 + 指数退避 + 首字/停滞双看门狗 |
| 上下文变长 | 一次请求打爆窗口 | 用量驱动的压缩，近窗原文逐字保留 |
| 危险工具 | 无人可拦 | 审批 FIFO，只有队首可决定，超时即拒绝 |
| 慢工具 / panic 工具 | 拖死整轮 / 打掉运行 | 分桶并发 + 单工具超时 + panic 收敛为错误结果 |
| 进程崩溃 | 对话全丢 | revision + 校验和的原子检查点 |
| 成本失控 | 月底才知道 | µUSD 整数记账 + 运行级预算 |

Rei 把这些做成**一层库**，而不是一个服务：没有服务端、没有账号、没有遥测。

## 设计边界

**Rei 负责**：一次运行内的推理循环、工具协议与执行、上下文压缩、审批、事件契约、会话容器与持久化、
子 agent（同步只读 / 后台任务）、MCP 客户端、多协议模型接入、事件 → trace 投影、Plan 模式工具与状态。

**Rei 不负责**（应由宿主实现）：HTTP/CLI 入口、多租户与鉴权、UI、IM 网关、定时调度、hooks、插件市场。
这些能力在宿主里做，才不会把内核绑死在某个产品上。

## 架构

```
┌── 宿主 ────────────────────────────────────────────────┐
│  CLI · Web 服务 · 机器人 · IDE 插件                     │
│  驱动 Session.Ask / Session.Run，消费 events.Event      │
└───────────────▲────────────────────────────────────────┘
                │ 命令 ⇄ 事件（进程内 Go API）
┌───────────────┴── Rei 内核 ────────────────────────────┐
│  session/   长时容器：输入队列 · 历史 · 用量 · 审批 · 持久化 │
│  agents/    运行引擎：ReAct 循环 · 重试 · 压缩 · 预算护栏   │
│  tools/     工具协议 + 执行引擎（分桶/超时/panic/截断）     │
│  provider/  模型接入：OpenAI 兼容 + Anthropic Messages      │
│  compress/  压缩策略：触发判断 · LLM 摘要 · 近窗保留        │
│  subagent/  子 agent：同步只读 · 后台任务 · journal 事件树  │
│  mcp/       MCP 客户端：stdio / http / sse + 工具适配        │
│  trace/     事件 → span 投影：只取元数据，详情按 id 回查     │
│  plan/      Plan 状态机 + plan_submit / plan_update          │
│  todo/      会话待办（随检查点落盘，压缩不影响）              │
│  skills/    SKILL.md 注册表 + load_skill                      │
│  metrics/   跨会话调用账本（JSONL + 聚合）                    │
│  events/    对外唯一契约（只依赖 core）                     │
│  core/      零依赖共享类型：消息 · 工具调用 · 用量 · 成本     │
└────────────────────────────────────────────────────────┘
```

依赖方向严格单向：`core → events/provider/tools → compress/agents/subagent/mcp/trace/plan → session → 宿主`。

三个对象承载全部状态：

```
Session        长时容器 —— 跨 Run 的状态都在这里
 └─ AgentLoop  运行引擎 —— 配置不可变，可复用、可并发
     └─ RunRequest / RunResult  单次运行 —— 用完即弃
```

## 能力清单

| 能力 | 说明 | 位置 |
|---|---|---|
| ReAct 循环 | 多轮「调模型 → 执行工具 → 回填结果」，轮次护栏 | `agents/loop.go` |
| 流式输出 | 正文/思考增量事件，增量不落盘 | `agents/stream.go` |
| 错误语义与重试 | `rate_limit / upstream / context_exceeded / permanent / canceled`，指数退避 + 抖动 | `provider/errors.go`、`provider/backoff.go` |
| 双看门狗 | 首字节超时（默认 90s）与流中段停滞（默认 3min） | `agents/stream.go` |
| 上下文压缩 | 真实用量优先、LLM 结构化摘要、近窗原文保留、失败保留原上下文 | `compress/` |
| 工具执行引擎 | 只读并行 / 写串行、单工具超时、panic 兜底、输出头尾截断 | `tools/engine.go` |
| 内置工具 | `read_file / write_file / edit_file / glob / grep / bash` | `tools/builtin/` |
| 审批（HITL） | 单一 FIFO、跨调用共享、只有队首可决定、超时视为拒绝 | `session/approval.go` |
| 事件契约 | 28 种事件，统一信封（run_id / parent_run_id / depth / index / time） | `events/events.go`、`events/tasks.go`、`events/plan.go` |
| 会话容器 | 输入队列背压、单会话串行、优雅关闭 | `session/session.go` |
| 持久化 | revision 单调、sha256 校验、tmp→fsync→rename→fsync(dir)、0600 | `session/store.go` |
| 成本护栏 | 整数 µUSD 记账、价表折算、运行级预算 | `core/usage.go`、`agents/loop.go` |
| 可中断 | `Session.Abort` 取消在跑运行；`Shutdown` 等它收尾后发 `session_closed` | `session/session.go` |
| 逐轮检查点 | 每个 ReAct 轮次结束回调，崩溃最多丢一轮 | `agents/loop.go`、`session/session.go` |
| 运行中插话 | `RunRequest.Steering` 在轮次边界注入新输入 | `agents/loop.go` |
| 审批策略 | `ReadOnlyPolicy` / `ToolsPolicy`：只读放行、写才问人；参数可人工改写 | `events/events.go`、`session/session.go` |
| 只读强制 | `WithReadOnlyMode` 由内核拒绝非只读工具 | `agents/loop.go` |
| 续写与护栏 | 输出截断自动续写、重复调用纠偏、目标对齐提醒 | `agents/loop.go` |
| 工具用量归集 | `Result.Usage` 并入运行账本（子 agent/内部摘要不再绕过预算） | `tools/engine.go`、`agents/loop.go` |
| 图片闸门 | 单次结果最多 4 张、每张 3 MiB | `tools/engine.go` |
| 跨会话账本 | 每次模型调用一行 JSONL，按会话聚合 | `metrics/` |
| 待办 | 随检查点落盘、不参与压缩、跨重启存活 | `todo/` |
| 技能注册表 | `SKILL.md` 渐进披露 + `load_skill` 工具 | `skills/` |
| 项目指令 | `AGENTS.md` / `CLAUDE.md` 分层注入系统提示 | `agents/context.go` |
| 联网与脚本 | `http_get`（SSRF 防护）、`run_python`（系统 python3） | `tools/builtin/net.go`、`tools/builtin/python.go` |
| 路径containment | 工具路径限制在工区内，越界直接拒绝 | `tools/builtin/builtin.go` |
| 命令护栏 | 破坏性命令静态拒绝 + 环境变量白名单 | `tools/builtin/bash.go` |
| 同步子 agent | `explore` 工具：嵌套子运行，事件带 `parent_run_id` / `depth`，深度上限 2 | `subagent/subagent.go` |
| 后台任务 | `agent_spawn / agent_send / agent_interrupt / task_output`，journal 落盘，结果回填主会话 | `subagent/registry.go`、`session/session.go` |
| MCP 客户端 | stdio / http / sse 三种传输，惰性连接 + 失败冷却，工具名 `mcp__<server>__<tool>` | `mcp/` |
| Anthropic Messages | 流式 SSE、thinking signature、tool_use 拼装、cache usage 映射 | `provider/anthropic/` |
| Trace 投影 | 5 类 span（run / llm / tool / compress / plan），只取元数据，按 call_id / span id 回查 | `trace/trace.go` |
| Plan 模式 | `plan_submit / plan_update` + 4 个 plan 事件 + 可注入提示的计划存储 | `plan/` |

**刻意没有做**：hooks、定时任务、IM 网关、插件市场、真沙箱。原因见「设计边界」——它们属于宿主或产品形态。

## 快速开始

### 1. 接一个模型

```go
prov, err := provider.NewOpenAI(provider.OpenAIConfig{
    BaseURL: "https://api.deepseek.com/v1", // 任何 OpenAI 兼容网关
    APIKey:  os.Getenv("REI_API_KEY"),
})
```

### 2. 组装引擎

```go
engine := tools.NewEngine(tools.WithToolTimeout(2 * time.Minute))
if err := builtin.RegisterWorkspace(engine, "."); err != nil {
    panic(err)
}

loop, err := agents.NewAgentLoop(prov,
    agents.WithModel("deepseek-chat"),
    agents.WithToolEngine(engine),
    agents.WithMaxIterations(16),
    agents.WithPricing(core.Pricing{Input: 270_000, Output: 1_100_000}), // 每百万 token 的 µUSD
    agents.WithMaxCost(500_000),                                          // 单次运行 0.5 美元上限
    agents.WithCompressor(compress.LLM{
        Provider:   prov,
        Model:      "deepseek-chat",
        Trigger:    compress.Trigger{ContextWindow: 64_000},
        KeepRecent: 8,
    }),
)
```

### 3. 跑一个会话

```go
store, _ := session.NewFileStore("./.rei-sessions")
s, _ := session.New("demo", loop, session.WithStore(store))
defer s.Shutdown(context.Background(), nil)

_ = s.Ask(context.Background(), core.NewUserTextMessage("统计仓库里有多少个 Go 文件"))

err = s.Run(context.Background(), func(ctx context.Context, e events.Event) {
    switch ev := e.(type) {
    case *events.ContentChunk:
        fmt.Print(ev.Text)
    case *events.ToolRunStart:
        fmt.Printf("\n[工具] %s\n", ev.Name)
    case *events.AgentEnd:
        fmt.Printf("\n结束：%s，成本 %dµUSD\n", ev.FinishReason, ev.Usage.Cost.Total())
    }
})
```

完整的可运行示例见 [`examples/quickstart`](examples/quickstart/main.go)：

```bash
export REI_API_KEY=sk-...
go run ./examples/quickstart "统计本仓库有多少个 Go 文件"
```

### 4. 接入审批

```go
// 会话本身就是 Approver：AgentLoop 每次要跑工具前都会先问它。
go func() {
    for ids := s.PendingApprovals(); ; ids = s.PendingApprovals() {
        if len(ids) > 0 {
            _ = s.ResolveApproval(ids[0], true, "允许") // 也可以拒绝或改参数
        }
        time.Sleep(100 * time.Millisecond)
    }
}()
```

宿主应当把这个循环换成真正的 UI 交互；`PendingApprovals` 返回的是**队首有序**列表，只有第一个可以被解决。

## 进阶装配

### 子 agent：同步只读 + 后台任务

```go
// 同步只读：子运行只拿到只读工具集，事件自动带 parent_run_id / depth
ro := subagent.ReadOnlyEngine(engine)
exploreLoop, _ := agents.NewAgentLoop(prov,
    agents.WithModel(model), agents.WithToolEngine(ro), agents.WithMaxIterations(8))
explore, _ := subagent.NewExploreTool(exploreLoop)
_ = engine.Register(explore)

// 后台任务：跨 Run 存活，journal 落盘，终态自动回填主会话
reg := subagent.NewRegistry(subagent.WithJournalDir("./.rei/tasks"))
defer reg.Close()
_ = subagent.Register(engine, reg) // agent_spawn / agent_send / agent_interrupt / task_output

sess, _ := session.New("demo", loop, session.WithTasks(reg))
```

事件树不需要额外索引：`task_started` / 子运行 `agent_start` 都带 `parent_run_id` 与
`depth`，投影端按这两个字段直接还原。

### MCP

```go
mgr := mcp.NewManager(mcp.Config{Servers: map[string]mcp.ServerConfig{
    "fs":   {Type: "stdio", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "."}},
    "docs": {Type: "http", URL: "https://gateway.example/mcp", Headers: map[string]string{"Authorization": "Bearer ..."}},
}})
defer mgr.Close()

n, err := mgr.RegisterTools(ctx, engine) // 单台失败按服务器隔离，返回已注册数量
```

工具名固定为 `mcp__<server>__<tool>`；只有服务端声明 `readOnlyHint` 的工具才会被标记为可并行。

### Anthropic Messages

```go
ant, _ := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
loop, _ := agents.NewAgentLoop(ant,
    agents.WithModel("claude-sonnet-4-5"), agents.WithToolEngine(engine))
```

thinking 增量与 `signature` 分别落到 `Message.Reasoning` 与 `Message.ReasoningSignature`，
回传时原样带回（跨模型不签名）。

### Trace

```go
rec := trace.NewRecorder(2000)
_ = sess.Run(ctx, rec.Handle) // 同一份事件流：驱动宿主，也进 recorder

res := rec.Build(50) // 纯函数投影，最多 50 条 trace
for _, sum := range res.Summaries {
    fmt.Println(sum.ID, sum.WallMS, sum.Tools, sum.Errors)
}
if d, ok := res.Detail(trace.KindTool, "call-1"); ok { // 按 call_id 回查正文
    fmt.Println(d.Payload.(*events.ToolRunEnd).Content)
}
```

### Plan 模式

```go
store := plan.NewStore()
_ = plan.Register(engine, store) // plan_submit / plan_update

// PromptFragment 每轮会变，因此下一轮用最新快照重建 loop（配置不可变但构造很轻）
loop, _ := agents.NewAgentLoop(prov,
    agents.WithModel(model), agents.WithToolEngine(engine),
    agents.WithSystemPromptExtra(store.PromptFragment()))
```

### 可中断、插话与待办

```go
// 中断在跑运行：Run 返回 session.ErrAborted，事件里 finish_reason=abort。
_ = sess.Abort()

// 运行中插话：Ask 的新消息会在下一个轮次边界并入上下文（Steering）。
_ = sess.Ask(ctx, core.NewUserTextMessage("改成先跑测试"))

// 待办与技能：随检查点落盘，重启后仍在；系统提示只放技能名与描述。
todos := todo.NewStore()
skillsReg, _ := skills.LoadDirs("./skills", "~/.rei/skills")
loop, _ := agents.NewAgentLoop(prov,
    agents.WithModel(model),
    agents.WithToolEngine(engine),
    agents.WithProjectInstructions("."), // AGENTS.md
    agents.WithSystemPromptExtra(skillsReg.PromptFragment()+todos.PromptFragment()),
)
_ = todo.Register(engine, todos)
_ = skillsReg.RegisterTool(engine)
sess, _ := session.New("demo", loop, session.WithTodo(todos))
```

### 账本

```go
store, _ := metrics.NewStore("./.rei/metrics.jsonl")
rec := &metrics.Recorder{Store: store, SessionID: sess.ID()}
_ = sess.Run(ctx, func(c context.Context, e events.Event) {
    rec.Handle(c, e)   // 只关心 llm_end / compress_end
    render(c, e)
})
fmt.Println(store.Aggregate(sess.ID()).CostMicro) // µUSD
```

## 核心 API

| 类型 | 职责 | 关键方法 |
|---|---|---|
| `core.Message` | 唯一会话消息模型（user / assistant / tool / system） | `Validate`、`Text` |
| `core.Usage` / `Cost` | 用量与 µUSD 成本 | `Add`、`PricedUsage` |
| `events.Event` | 全部可观测步骤 | `Type`、`Envelope` |
| `tools.Tool` | 一个可调用能力 | `Name`、`Parameters`、`CanParallel`、`Call` |
| `tools.Engine` | 工具注册与执行 | `Register`、`Execute`、`Specs` |
| `provider.Provider` | 一条模型流 | `Stream` |
| `agents.AgentLoop` | 运行引擎 | `RunStream` |
| `compress.Compressor` | 压缩策略 | `Decide`、`Compress` |
| `session.Session` | 长时容器 | `Ask`、`Run`、`ResolveApproval`、`Shutdown` |
| `session.Store` | 持久化后端 | `Save`、`Load` |

### 扩展点

| 想改什么 | 实现什么 | 说明 |
|---|---|---|
| 换模型协议 | `provider.Provider` | 只需实现 `Stream`；自带 OpenAI 兼容实现 |
| 换压缩策略 | `compress.Compressor` | `Decide` 决定时机，`Compress` 决定产物；失败必须返回 error |
| 换存储 | `session.Store` | Web 宿主建议实现 DB 版；文件版见 `FileStore` |
| 加工具 | `tools.Tool` | `CanParallel=true` 表示只读、可并发 |
| 换审批交互 | `events.Approver` | 或直接用 `Session` 的 FIFO |
| 加工件提示 | `agents.WithSystemPromptExtra` | 与基础契约分层拼接，字节稳定 |

## 事件模型

一次运行的事件顺序（真实顺序，不是示意图）：

```
agent_start
  └─ compress_start → compress_end            （需要压缩时）
  └─ llm_start → content_chunk / reasoning_chunk → llm_end
       └─ tool_run_start → tool_approval_requested → tool_approval_resolved → tool_run_end
  └─ …（回到 llm_start，直到没有工具调用）
  └─ session_budget_exceeded                   （预算耗尽）
agent_end                                      （finish_reason + 聚合用量 + 时长）
```

子 agent 与 Plan 的插入位置：

```
tool_run_start（explore / agent_spawn）
  └─ agent_start(parent_run_id, depth=1) → … → agent_end     （子运行的完整事件）
  └─ task_started → task_message* → task_end
  └─ task_result_delivered                                  （结果回填主会话）
  └─ plan_generated / plan_step_started / plan_step_completed
```

失败也是事件：`llm_error`（含是否可重试）、`session_run_error`、`session_persist_error`。没有静默失败。
事件共 **28 种**，封闭集合；新增类型属于 API 变更。

## 持久化语义

- **恢复粒度 = 一个 Run**：每轮结束提交一次检查点（`Messages + Usage + revision`）。
- **同一内容幂等**：内容不变时重放不会推高 `revision`；内容变化才递增。
- **原子提交**：临时文件 → `fsync` → `rename` → `fsync(dir)`；文件权限 `0600`。
- **校验和**：先对「不含 checksum 的 JSON」求 sha256，再写入；读取时校验，篡改/写坏直接报 `ErrChecksum`。
- **冷启动**：会话不存在时返回空快照而不是错误。
- **坏历史不启动**：恢复时校验 `tool_calls ↔ tool 回执` 配对，不合法直接失败——带着坏历史继续跑比启动失败更糟。

## 测试

```bash
go test ./...          # 全部单元测试
go vet ./...           # 静态检查
go test -race ./...    # 竞态检测（推荐在 CI 里跑）
```

| 包 | 覆盖重点 |
|---|---|
| `core` | 消息不变量、工具调用配对、成本折算、缓存计价 |
| `events` | 事件名稳定性（公开契约）、信封传递 |
| `provider` | SSE 文本/思考/工具调用拼装、429 与上下文超限分类、Retry-After、自定义头 |
| `tools` | 并发确实并发、串行冲刷、panic 收敛、超时、头尾截断、重名注册 |
| `tools/builtin` | 读写改、路径越界拒绝、glob/grep、bash 退出码与破坏性命令拒绝、环境隔离 |
| `compress` | 触发条件（真实用量优先）、近窗保留、不切断工具调用对、摘要失败保留原上下文 |
| `agents` | 工具→回答全链路、审批拒绝不执行、可重试恢复、不可重试终止、轮次/预算护栏、首字超时、事件顺序 |
| `session` | 队列背压、持久化与恢复、审批 FIFO（仅队首）、超时即拒绝、关闭后拒绝新工作、持久化失败可见、任务结果回填 |
| `subagent` | 子运行事件树（parent_run_id / depth）、深度护栏、后台任务 journal、中断、只读引擎白名单 |
| `mcp` | HTTP / SSE / stdio 三种传输、initialize 握手、工具注册与调用、错误透传、失败冷却、配置校验 |
| `provider/anthropic` | 请求映射（system / tool_result / thinking / image）、SSE 事件序列、usage 与 stop_reason 映射、429 分类 |
| `trace` | span 树结构、只取元数据、按 call_id 回查详情、limit/HasMore、记录器容量 |
| `plan` | 提交校验、状态迁移、英文提示片段、plan 工具事件、`plan_updated` |
| `todo` | 增删改查、恢复后 id 不冲突、英文提示片段、工具调用 |
| `skills` | front matter 解析、渐进披露、`load_skill`、缺失目录 |
| `metrics` | 事件聚合、JSONL 落盘、会话过滤 |
| `provider/responses` | 文本/思考增量、函数调用拼装、请求映射、429 分类 |

## 目录结构

```
.
├── core/               消息 · 工具调用 · 用量 · 成本（零依赖）
├── events/             事件契约（Type / Base / Handler / Approver）
├── provider/           Provider 接口 + OpenAI 兼容流式实现 + 错误分类 + 退避
├── tools/              工具协议 + 执行引擎
│   └── builtin/        工区工具（读写改 / glob / grep / bash）
├── compress/           触发判断 + 近窗压缩 + LLM 摘要压缩
├── agents/             AgentLoop（ReAct / 重试 / 压缩 / 审批 / 预算）
├── subagent/           同步只读子 agent + 后台任务注册表 + journal
├── mcp/                MCP 客户端（配置 / 传输 / JSON-RPC / 工具适配 / 连接管理）
├── trace/              事件 → span 投影 + 详情回查 + 内存记录器
├── plan/               Plan 状态机 + plan_submit / plan_update
├── todo/               会话待办（随快照持久化）
├── skills/             SKILL.md 注册表 + load_skill
├── metrics/            调用账本（JSONL + 聚合）
├── provider/anthropic/ Anthropic Messages 流式实现
├── session/            会话容器 + 审批 FIFO + 持久化 + 任务结果回填
└── examples/quickstart 可运行示例
```

## 语言约定

- **注释、包文档、README、面向宿主的错误信息：中文。**
- **内置 prompt 一律英文**：基础系统提示、压缩摘要提示、压缩转写标签、工具 `Description` 与参数 schema 说明、工具结果里的状态文案（如 `User denied this tool call`、`... [truncated N bytes] ...`）。原因：这些字符串会原样进入模型上下文，英文在不同厂商的分词与指令微调分布上最稳定，也避免中英混排在前缀缓存上产生额外分片。
- 这条约定由测试守护：`agents/prompt_test.go`、`compress/prompt_test.go`、`tools/builtin/prompt_test.go` 会扫描内置 prompt 中的非 ASCII 字符。

## 设计取舍

1. **事件是公开契约**：新增/修改事件类型等于 API 变更；改事件名字符串等于破坏宿主。
2. **压缩失败保留原上下文**：绝不截断式降级——截断会静默丢信息，失败至少是可见的。
3. **重试只在「还没吐字」时发生**：已经向宿主发过正文增量再重试，会造成重复输出。
4. **审批串行且只认队首**：乱序决定会让人看到的顺序与实际放行顺序不一致，审计失效。
5. **成本用整数 µUSD**：浮点累加在长会话里会产生可见误差。
6. **bash 是护栏不是沙箱**：静态拒绝 + 环境白名单只能降低误操作概率；不可信代码请放进容器。

## 路线图

已完成（`feat/adk-extract` 分支）：

- [x] 子 agent（同步只读 / 后台任务，带 `parent_run_id` 事件树）
- [x] MCP 客户端（stdio / http / sse）
- [x] Anthropic Messages 协议实现
- [x] 事件 → trace 投影（span 只取元数据，详情按 `call_id` 回查）
- [x] `Plan` 模式事件与工具

第二轮（本轮完成）：

- [x] 可中断：`Abort` + `Shutdown` 取消在跑运行
- [x] 逐轮检查点、运行中插话、输出续写
- [x] 审批策略（只读放行/写审批）、只读模式内核强制、审批参数改写
- [x] 工具用量归集、图片闸门、重复调用与目标对齐护栏
- [x] 压缩：token 预算近窗、文件清单、任务锚点、质量重试
- [x] 可观测性：metrics 账本、wait span、retries、request id 回查、MetaBytes、上下文构成
- [x] 持久化：flock、`.bak` 回退、待办随快照
- [x] Provider：厂商预设与价表、OpenAI Responses 协议
- [x] 生态：`ask_user`、`todo`、`skills`、`AGENTS.md`、`http_get`（SSRF）、`run_python`
- [x] 子 agent：`agent_resume`、终态定向投递
- [x] MCP：单调用超时、断线重连、工具列表缓存

仍未做（有意）：

- [ ] 子 agent `promote`（慢工具摘后台）：需要批量控制器，收益与复杂度不匹配
- [ ] MCP resources / prompts / sampling、配置热重载
- [ ] Seatbelt / bwrap 真沙箱（当前 bash 只有静态护栏）
- [ ] `git_worktree`（宿主按需自己封装）

## 许可证

MIT。

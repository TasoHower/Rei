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

**Rei 负责**：一次运行内的推理循环、工具协议与执行、上下文压缩、审批、事件契约、会话容器与持久化。

**Rei 不负责**（应由宿主实现）：HTTP/CLI 入口、多租户与鉴权、UI、IM 网关、定时调度、子 agent 编排、MCP 客户端。这些能力在宿主里做，才不会把内核绑死在某个产品上。

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
│  provider/  模型接入：流式协议 · 错误分类 · 退避            │
│  compress/  压缩策略：触发判断 · LLM 摘要 · 近窗保留        │
│  events/    对外唯一契约（只依赖 core）                    │
│  core/      零依赖共享类型：消息 · 工具调用 · 用量 · 成本    │
└────────────────────────────────────────────────────────┘
```

依赖方向严格单向：`core → events/provider/tools → compress/agents → session → 宿主`。

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
| 事件契约 | 20 种事件，统一信封（run_id / index / time） | `events/events.go` |
| 会话容器 | 输入队列背压、单会话串行、优雅关闭 | `session/session.go` |
| 持久化 | revision 单调、sha256 校验、tmp→fsync→rename→fsync(dir)、0600 | `session/store.go` |
| 成本护栏 | 整数 µUSD 记账、价表折算、运行级预算 | `core/usage.go`、`agents/loop.go` |
| 路径containment | 工具路径限制在工区内，越界直接拒绝 | `tools/builtin/builtin.go` |
| 命令护栏 | 破坏性命令静态拒绝 + 环境变量白名单 | `tools/builtin/bash.go` |

**刻意没有做**：子 agent / MCP / hooks / 定时任务 / IM / 沙箱。原因见「设计边界」——它们要么属于宿主，要么需要产品形态才能定义清楚。

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

失败也是事件：`llm_error`（含是否可重试）、`session_run_error`、`session_persist_error`。没有静默失败。

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
| `session` | 队列背压、持久化与恢复、审批 FIFO（仅队首）、超时即拒绝、关闭后拒绝新工作、持久化失败可见 |

## 与 hai-harness 的关系

Rei 的运行时机制抽取自 [`hai-harness`](https://github.com/seven7628/hai-harness)（MIT）的内核，并做了三处有意收窄：

| 维度 | hai-harness | Rei |
|---|---|---|
| 形态 | 完整 harness + 桌面端参考宿主 | 纯库，宿主自备 |
| 事件 | 接口式 29 类型 / 48 struct | 20 类型 + 统一信封 |
| 持久化 | `.jsonl` + `.lock` + `.record.json` + 块 sidecar | 单文件/单键快照（便于换成 DB） |
| 依赖 | 20+（含三家模型 SDK） | **零第三方依赖**（仅标准库） |
| 范围 | 含子 agent / MCP / hooks / cron / IM / 插件 | 只保留运行时内核 |

事件名、语义与护栏策略尽量与 hai-harness 对齐，便于已有宿主迁移；但不追求 API 逐个同形。

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
├── session/            会话容器 + 审批 FIFO + 持久化
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

- [ ] 子 agent（同步只读 / 后台任务，带 `parent_run_id` 事件树）
- [ ] MCP 客户端（stdio / http / sse）
- [ ] Anthropic Messages 协议实现
- [ ] 事件 → trace 投影（span 只取元数据，详情按 `call_id` 回查）
- [ ] `Plan` 模式事件与工具

## 许可证

MIT。运行时机制抽取自 `hai-harness`（MIT，Copyright seven7628），保留其版权声明；本仓库实现为独立重写并做了收窄。

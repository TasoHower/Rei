# loopForge Budget（预算控制）

> 本文档描述 loopForge 的 **BudgetCounter 预算控制机制**——如何在 token 和成本两个维度上限制一次 Run 的消耗上限，以及为什么不同模型提供商的 ChatModel 实现会导致预算与实际成本之间存在偏差。
>
> **前置阅读**：[02-runner-core.md](02-runner-core.md) §5——Runner 在 `Run()` 中创建 BudgetCounter 注入到 RunState；[10-spawn.md](10-spawn.md) §4——engineSpawner 在子 Agent 完成时调用 BudgetCounter.Consume。

---

## 1. 概念

### 1.1 为什么需要预算控制

LLM API 是按 token 计费的。一个 Agent loop 可能执行数十步，每步调用一次 LLM，再加上 spawn 子 Agent 的嵌套调用——token 消耗可以很快累积到不可控的程度。预算控制的作用是在超出限制时**主动拦截**，而不是让调用方到大月底看到账单才意识到失控。

```go
// 一个简单的循环可能产生大量 token：
for step := range maxSteps {    // 最多 64 步
    ChatModel.Stream(ctx, msgs) // 每步一次 LLM 调用（含对话历史）
    executeToolCalls(...)        // 工具结果回注到消息列表 → 下一步历史更长
}
```

Spawn 子 Agent 在独立 goroutine 中调用——偏差可能来自子树。

```
Root Run
  ├── Agent A LLM 调用  × N steps
  ├── spawn Agent B（独立 goroutine）
  │     └── Agent B LLM 调用 × M steps
  └── spawn Agent C（独立 goroutine）
        └── Agent C LLM 调用 × K steps
```

BudgetCounter 通过在 RunState 中持有**全树共享**的计数器，让所有 spawn 子 Agent 的 token 消耗汇总到同一本账上。

### 1.2 精确性的边界

loopForge 的 BudgetCounter 在 token 和 USD 两个维度上控制，但它的计数精度**受限于 ChatModel 适配器的汇报质量**：

```
BudgetCounter 的输入来源：
  ChatModel 适配器 → Message.InputTokens / Message.OutputTokens
```

`Message.InputTokens` 和 `Message.OutputTokens` 由适配器在调用 LLM 后填充。**不同 ChatModel 实现可以从不同的渠道获取这些数值**：

- 官方 SDK（DeepSeek / 火山方舟）通常从 API 响应的 `usage` 字段直接读取——这是**最准确**的，服务端计费用的就是这些数字
- 自实现的 ChatModel 可能从响应头、本地估算、或完全不填充（0）——**精确性取决于实现者的选择**
- 某些模型提供商不返回 token 计数，或只在非流式响应中返回

**USD 成本估算同样受此影响**：`TotalCostUSD` 是基于 `InputTokens * input_price + OutputTokens * output_price` 推算的，如果基础 token 计数不准，成本估算也会不准。

**BudgetCounter 做的是在引擎层保证"计数器归口"**——所有子 Agent 的消耗汇总到同一个计数器，使用 `Mutex` 保证并发安全。但计数器里的数字是否准确，取决于**从哪个 ChatModel 适配器灌入的数据**。这不是引擎的问题——引擎无法知道厂商 API 最终按什么计费。调用方在使用自定义 ChatModel 时，应在 `Generate` / `Stream` 的返回值中正确填充 `Message.InputTokens` 和 `Message.OutputTokens`，否则预算控制的效果会被削弱。

---

## 2. BudgetCounter

`pkg/runtime/budget/budget.go`：

```go
type BudgetCounter struct {
	mu         sync.Mutex    // 线程安全
	TokensUsed int64          // 已消耗的 token 总数
	CostUSD    float64        // 已消耗的成本（美元）
	TokensMax  int64          // token 上限（0 = 不限制）
	CostUSDMax float64        // 成本上限（0 = 不限制）
}
```

### 2.1 构造

```go
func NewBudgetCounter(tokensMax int64, costUSDMax float64) *BudgetCounter
```

**0 值表示"不限制"**——`TokensMax = 0` 时不过问 token，`CostUSDMax = 0` 时不过问成本。

### 2.2 方法

| 方法 | 作用 |
|------|------|
| `Allow(tokens, costUSD)` | 检查能否容纳额外消耗——超过上限返回 false，不消耗 |
| `Consume(tokens, costUSD)` | 记录已消耗的 token 和成本 |
| `Used()` | 获取当前已使用量 |
| `Limits()` | 获取配置上限 |

### 2.3 使用模式：Allow + Consume 两步

`Allow` 和 `Consume` 分离，不锁定整个操作周期：

```go
// 在 Spawn 之前检查
if !runState.BudgetCounter.Allow(estimatedTokens, 0) {
    return nil, "budget_exceeded"
}
// Spawn 之后汇报实际用量
result, err := child.Spawn(...)
runState.BudgetCounter.Consume(result.Metrics.TotalTokens, result.Metrics.TotalCostUSD)
```

两步分离的原因是 LLM 调用的耗时不确定（数秒到数十秒），`Allow` 只做上限判断不锁定，`Consume` 在调用完成后将实际使用量归入总账。在此期间其他 goroutine 也可以继续调用 `Allow`。

---

## 3. 预算的配置

### 3.1 全局默认值

`internal/defaults/loop.go`：

```go
const (
	SpawnBudgetTokenTreeDefault = int64(0)    // 默认不限制 token
	SpawnBudgetUSDTreeDefault   = float64(0)  // 默认不限制成本
)
```

**默认不限制**——调用方需要主动启用。

### 3.2 通过 Runner 配置

```go
r := runner.NewRunner(entry,
	runner.WithBudgetLimits(100000, 0.5),  // token 上限 10 万，USD 上限 0.5
)
```

Runner 在 `Run()` 中构造 BudgetCounter：

```go
tokensMax := r.budgetTokens    // runner 配置
if tokensMax <= 0 {
    tokensMax = defaults.SpawnBudgetTokenTreeDefault  // 全局默认（0=不限制）
}
usdMax := r.budgetUSD
if usdMax <= 0 {
    usdMax = defaults.SpawnBudgetUSDTreeDefault
}
bc := budget.NewBudgetCounter(tokensMax, usdMax)
```

### 3.3 在 RunState 中传播

```go
runState := &engine.RunState{
    BudgetCounter: policy.BudgetCounter(),  // 全树共享
}
```

transfer 模式下，多个 Agent hop 共享同一个 `RunState`，因此 BudgetCounter 在跨 Agent 转移时保持连续。

---

## 4. 预算的归集路径

### 4.1 engineSpawner 中的归集

`internal/engine/spawn.go`：

```go
func (s *engineSpawner) Spawn(ctx, parent, spec) (*exchange.SpawnResult, error) {
    childCtx, childCancel := context.WithCancel(ctx)
    defer childCancel()

    result, err := s.inner.Spawn(childCtx, parent, spec)

    if result != nil && s.runState != nil && s.runState.BudgetCounter != nil {
        s.runState.BudgetCounter.Consume(result.Metrics.TotalTokens, 0)
    }
    // ...
}
```

子 Agent 的 `SpawnResult.Metrics.TotalTokens` 在 engineSpawner 中被汇总到根 Run 的 BudgetCounter。这样 Root 看到的 `TokensUsed` 包含了所有子 Agent 的消耗。

### 4.2 Runner 中的指标累积

transfer 模式下，Runner 的 `runTransferLoop` 在每个 Agent hop 后累积指标，但**这些指标目前不回流到 BudgetCounter**（BudgetCounter 主要在 spawn 场景中发挥作用）。

### 4.3 使用场景链

```
Runner.Run()
  │
  ├── 创建 BudgetCounter（根据 WithBudgetLimits 或 defaults）
  │
  ├── 注入到 RunState.BudgetCounter
  │
  ├── RunLoop 中的 LLM 调用：适配器填充 Message.InputTokens → 计入 RunMetrics
  │
  ├── executeToolCalls：不产生额外 token 消耗（工具执行在本地）
  │
  └── engineSpawner.Spawn()：
        └── BudgetCounter.Consume(result.Metrics.TotalTokens, 0)
              子 Agent 的 token 消耗汇总到根
```

---

## 5. 预算与实际账单的差异

| 因素 | 说明 |
|------|------|
| **ChatModel 适配器的 token 填充** | 适配器在 `Message` 中填入的 `InputTokens`/`OutputTokens` 可能来自 API 返回值、本地估算或 0——精度取决于实现 |
| **不同模型计费公式不同** | DeepSeek 按 token 计费，火山方舟可能按字符或包年——loopForge 的 `TotalCostUSD` 只是估算 |
| **上下文缓存 / 免费额度** | 某些厂商有免费额度或缓存命中不计费——引擎无法感知这些因素 |
| **非流式 vs 流式** | 非流式通常省去最后一条 `role=assistant` 消息的 token（不计入下一轮请求）；流式可能产生额外 token（chunk 分隔符等） |

**引擎不保证预算与实际账单一致**——这是所有 LLM 抽象层都面临的共同问题。BudgetCounter 的目标是提供一个**用户可控的软上限**，防止无限制的调用，而不是替代厂商的计费系统。

---

## 6. 完整数据流

```
Runner.Run()
  │
  ├── WithBudgetLimits(100000, 0.5)
  │
  ├── budget.NewBudgetCounter(100000, 0.5)
  │
  ├── RunState.BudgetCounter = bc
  │
  ├── [Agent A RunLoop]
  │     ├── step 0: LLM 调用 → Message{InputTokens=500, OutputTokens=200}
  │     ├── step 1: LLM 调用 → Message{InputTokens=800, OutputTokens=300}
  │     └── step 2: spawn_subagent → engineSpawner.Spawn()
  │
  ├── [Agent B (spawn child)]
  │     ├── RunLoop 执行（独立 goroutine）
  │     ├── SpawnResult.Metrics.TotalTokens = 3500
  │     └── engineSpawner: BudgetCounter.Consume(3500, 0)
  │                          └── bc.TokensUsed = 500+200+800+300+3500 = 5300
  │
  ├── [Agent A 继续]
  │     └── step 3: LLM 调用 → bc.Allow(estimated, 0) → 仍在预算内 → Generate
  │
  └── query_end: Outcome.Metrics.TotalTokens = 5300
```

---

## 7. 与代码目录的对应

| 文件 | 职责 |
|------|------|
| `pkg/runtime/budget/budget.go` | BudgetCounter（Allow / Consume / Used / Limits） |
| `internal/engine/spawn.go` | engineSpawner 中 BudgetCounter.Consume（子 Agent token 归集） |
| `internal/engine/runstate.go` | RunState.BudgetCounter 字段 |
| `internal/engine/policy.go` | LoopPolicy.BudgetCounter() |
| `internal/defaults/loop.go` | SpawnBudgetTokenTreeDefault / SpawnBudgetUSDTreeDefault |
| `pkg/runner/runner.go` | WithBudgetLimits、Run() 中创建 BudgetCounter |

---

## 8. 相关文档

| 文档 | 关系 |
|------|------|
| [02-runner-core.md](02-runner-core.md) §5 | Runner 创建 BudgetCounter 并注入 RunState |
| [10-spawn.md](10-spawn.md) §4 | engineSpawner 中 BudgetCounter.Consume |
| [11-chatmodel.md](11-chatmodel.md) §2 | ChatModel 适配器填充 Message.InputTokens/OutputTokens——这是预算的底层数据来源 |
| [00-abstractions.md](00-abstractions.md) §8 | RunMetrics 中的 TotalTokens / TotalCostUSD |

---

# 变更日志
| 日期 | 版本 | 变更说明 |
|------|------|----------|
| 2026-04-30 | v0.9.5 | 初稿：BudgetCounter 机制、Allow/Consume 两步模式、默认不限制、engineSpawner 归集路径、预算与实际账单的差异分析。 |

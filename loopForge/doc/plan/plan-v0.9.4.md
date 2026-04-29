# loopForge 实施计划 — v0.9.4（Spawn 子 Agent start/end 消息恢复）

> **对应版本进度**：`doc/log/progress-v0.9.4.md`  
> **本文件角色**：`rei-gated-workflow` **Step 2** 的任务拆分与执行顺序；**先于编码**成文。  
> **主目标**：恢复 spawn Agent 的 `start` / `end` 消息对 test-server 的可观测性，同时保持 spawn Agent 对父 Agent 的上下文隔离。

---

## 1. 背景与问题

### 1.1 现状

v0.9.3 中，spawn 子 Agent 的事件流对父端**完全不可见**。`DefaultSpawner.Spawn()`（[spawn.go:L134-L176](file:///Users/admin/Documents/hower/rei/loopForge/pkg/agent/spawn.go#L134-L176)）在本地事件循环中消费子 Agent 的所有事件（`start`、`answer`、`call_llm_*`、`tool_call_*`、`query_end`），仅提取：
- `tool_call_start`/`tool_call_end` → `ChildToolCall` 列表
- `query_end` → `RuntimeOutcome`（FinalText + Metrics）

然后返回单一 `SpawnResult` 结构体。父端（test-server）看到的仅仅是 `spawn_subagent` 作为一个普通工具调用的 `tool_call_start`/`tool_call_end` 事件对，**看不到子 Agent 的启动与结束**。

### 1.2 设计预期

`data-fusion.md` §3.1 已明确建议：
> "若需单独表达 spawn 生命周期，优先用 **`agent_transfer` + `meta_data`**"

`SpawnSpec.OutputCh`（[exchange.go:L77-L79](file:///Users/admin/Documents/hower/rei/loopForge/pkg/runtime/exchange/exchange.go#L77-L79)）字段已预留但从未被赋值使用，注释说明其用途正是"接收子 Agent 的 RuntimeEvent 并转发到父事件流"。

`AgentTransferPayload`（[event.go:L157-L168](file:///Users/admin/Documents/hower/rei/loopForge/pkg/runtime/event/event.go#L157-L168)）已具备 `Phase`（start/end）、`FromAgent`、`ToAgent`、`ChildRunID`、`Depth` 等字段，天然匹配 spawn 生命周期表达。

### 1.3 约束

- **必须保持上下文隔离**：子 Agent ReAct 中间步骤（answer 流式文本、call_llm_*、tool_call_*）仍然**不对父端暴露**（符合 `spawn-runtime-rules.md` §7）
- **只恢复 start/end**：仅将子 Agent 的启动与结束事件转发到父事件流

---

## 2. 关联文档

| 文档 | 作用 |
|------|------|
| `doc/log/progress-v0.9.4.md` | 版本目标、范围、交付清单 |
| `doc/log/progress-v0.9.3.md` | 上一版本基线 |
| `doc/design/spawn-runtime-rules.md` §7 | 子 ReAct 中间步骤不可见规则 |
| `doc/design/data-fusion.md` §3.1 | agent_transfer 用于 spawn 生命周期的设计建议 |

---

## 3. 技术方案

### 3.1 总体思路

利用已有的 `AgentTransferPayload`（Phase=start/end）表达 spawn 子 Agent 的生命周期事件，通过 `SpawnSpec.OutputCh` 通道转发到父 Agent 的事件流，最终由 test-server 的 SSE 处理器和前端渲染。

```
子 Agent RunLoop
  │
  ├─ EventStart ──→ DefaultSpawner 本地消费 ──→ OutputCh ──→ 父 eventCh ──→ test-server SSE
  │                                                │
  ├─ EventAnswer(Chat) ──→ 本地消费（隔离，不转发）     │
  ├─ EventCallLLMStart  ──→ 本地消费（隔离，不转发）     │
  ├─ EventCallLLMEnd    ──→ 本地消费（隔离，不转发）     │
  ├─ EventToolCallStart ──→ 本地消费 + 记录到 ChildToolCalls
  ├─ EventToolCallEnd   ──→ 本地消费 + 记录到 ChildToolCalls
  │                                                │
  └─ EventQueryEnd ──→ 本地消费（提取 Outcome）──→ OutputCh ──→ 父 eventCh ──→ test-server SSE
```

### 3.2 事件映射

| 子 Agent 事件 | 转发形式 | 说明 |
|--------------|---------|------|
| `EventStart` | `agent_transfer`（Phase=start） | 标明 FromAgent=parent, ToAgent=child, ChildRunID, Depth |
| `EventQueryEnd` | `agent_transfer`（Phase=end） | 附带 OK/status 和子 Agent 的 FinalText 摘要 |
| 所有其他事件 | 不转发 | 保持上下文隔离 |

---

## 4. 任务分阶段

### 阶段 A — DefaultSpawner 转发子 start/end 事件

**文件**：`loopForge/pkg/agent/spawn.go`

1. 在 `DefaultSpawner.Spawn()` 的事件循环中，当收到子 Agent 的 `EventStart` 事件时：
   - 若 `spec.OutputCh != nil`，构造 `AgentTransferPayload{Phase: TransferStart, FromAgent: parent.AgentRole, ToAgent: childTmpl.Name, ChildRunID: childRef.RunID, Depth: childRef.Depth}` 并通过 `spec.OutputCh` 非阻塞发送
   - 非阻塞发送避免子 Agent 事件循环被父端慢消费阻塞

2. 当收到子 Agent 的 `EventQueryEnd` 事件时（在提取 Outcome 之后）：
   - 若 `spec.OutputCh != nil`，构造 `AgentTransferPayload{Phase: TransferEnd, FromAgent: parent.AgentRole, ToAgent: childTmpl.Name, ChildRunID: childRef.RunID, Depth: childRef.Depth, OK: status==completed}` 并通过 `spec.OutputCh` 非阻塞发送

3. 所有中间事件（`EventAnswer`、`EventCallLLMStart`、`EventCallLLMEnd`、`EventToolCallStart`、`EventToolCallEnd`）保持现有逻辑不变（仅本地消费）

### 阶段 B — spawn_tool 传递 OutputCh

**文件**：`loopForge/pkg/agent/spawn_tool.go`

1. 在 `makeSpawnHandler` 中，构造 `SpawnSpec` 后、调用 `sp.Spawn()` 前，设置 `spec.OutputCh = eventCh`
   - 同步路径（`eventCh == nil`）：无需额外处理（OutputCh 为 nil 将跳过转发）
   - 异步路径（`eventCh != nil`）：子 start/end 事件将自动转发到父事件流

### 阶段 C — test-server 前端适配

**文件**：`test-server/static/index.html`

1. 在 `handleEvent` 中新增对 `agent_transfer` 事件的 spawn 子 Agent 渲染：
   - `Phase=start`：显示"子 Agent 启动"横幅（含子 Agent 名称、RunID）
   - `Phase=end`：显示"子 Agent 完成"横幅（含状态：成功/失败）

2. 与现有 transfer 模式的 `agent_transfer` 事件区分：
   - transfer 模式：FromAgent/ToAgent 为同级 Agent（如 triage → math_expert）
   - spawn 模式：通过 `ChildRunID` 和 `Depth` 字段区分（Depth > 0 表示子树）

### 阶段 D — 验证与文档

1. 运行 `go build ./...` 确认编译通过
2. 在 test-server 中测试 spawn 模式，确认 start/end 消息正确渲染
3. 新增 `doc/log/progress-v0.9.4.md` 版本进度日志
4. 更新本计划文档的变更记录

---

## 5. 主要文件/包影响

| 路径/包 | 类型 | 说明 |
|---------|------|------|
| `loopForge/pkg/agent/spawn.go` | 修改 | 事件循环中新增 start/end 转发逻辑（约 +20 行） |
| `loopForge/pkg/agent/spawn_tool.go` | 修改 | makeSpawnHandler 中设置 spec.OutputCh（约 +1 行） |
| `loopForge/pkg/runtime/exchange/exchange.go` | 无变更 | OutputCh 字段已存在，仅首次被实际使用 |
| `loopForge/pkg/runtime/event/event.go` | 无变更 | AgentTransferPayload 字段已满足需求 |
| `test-server/static/index.html` | 修改 | 新增 spawn agent_transfer 渲染逻辑（约 +30 行） |
| `test-server/main.go` | 可能微调 | 如 event_transfer 处理需额外日志区分 |

---

## 6. 风险与边界

1. **非阻塞发送**：`spec.OutputCh` 使用 `select { case ... <- ev: default: }` 非阻塞模式，避免父端通道满时阻塞子 Agent。如果父端消费慢，start/end 事件可能丢失（这是可接受的权衡——事件为可观测性增强，不影响功能正确性）。

2. **与现有 transfer 模式 event_transfer 的区分**：前端需要根据 `ChildRunID`/`Depth` 字段区分 spawn 子树事件和同级 transfer 事件。现有 transfer 模式的 `agent_transfer` 事件已使用相同的 `AgentTransferPayload`，需要在前端做条件渲染。

3. **向后兼容**：OutputCh 为 nil 时行为与当前完全一致（零影响），变更仅在使用 OutputCh 时生效。

---

## 7. 验收标准

1. test-server spawn 模式下，子 Agent 启动时前端能看到"子 Agent 启动"事件
2. test-server spawn 模式下，子 Agent 完成时前端能看到"子 Agent 完成"事件（含运行状态）
3. 子 Agent 的 ReAct 中间步骤（reasoning 文本、LLM 调用、工具调用中间步骤）不出现在父端事件流中
4. 现有 transfer 模式的 `agent_transfer` 事件渲染不受影响
5. 编译通过，现有测试不退化

---

## 8. 实现细节

### 8.1 `loopForge/pkg/agent/spawn.go` — DefaultSpawner 事件转发

#### 8.1.1 当前事件循环结构

```go
// spawn.go L133-L176
eventLoop:
for {
    select {
    case ev, ok := <-ch:        // 子事件到达
        if !ok { break eventLoop }
        if QueryEnd → 提取 last       // L142-149, continue
        if ToolCallStart → 记录       // L151-154
        if ToolCallEnd → 记录         // L156-167
    case <-childCtx.Done():      // 父取消
        break eventLoop          // L174
    }
}
// 构造 SpawnResult 返回          // L194-204
```

#### 8.1.2 三个插入点

**插入点 ①**：收到 `EventStart` 后，构造 `agent_transfer(TransferStart)` 非阻塞发送。

位置：[spawn.go:L133-L140](file:///Users/admin/Documents/hower/rei/loopForge/pkg/agent/spawn.go#L133-L140) — `case ev, ok := <-ch:` 分支，`QueryEnd` 判断**之前**。

```go
        // 将子 Agent 启动事件转发到父事件流（可观测性增强，不破坏隔离）
        if se := ev.Start(); se != nil && spec.OutputCh != nil {
            select {
            case spec.OutputCh <- &event.RuntimeEvent{
                Type:  event.EventAgentTransfer,
                RunID: parent.RunID,
                Step:  0,
                Payload: &event.AgentTransferPayload{
                    Phase:      event.TransferStart,
                    FromAgent:  parent.AgentRole,
                    ToAgent:    childTmpl.Name,
                    ChildRunID: childRef.RunID,
                    Depth:      childRef.Depth,
                },
            }:
            default:
            }
        }
```

**插入点 ②**：收到 `EventQueryEnd` 后，提取 `last` 之后、`continue` 之前。

位置：[spawn.go:L142-L149](file:///Users/admin/Documents/hower/rei/loopForge/pkg/agent/spawn.go#L142-L149) — `if qe := ev.QueryEnd(); qe != nil { ... continue }` 块内。

```go
        if qe := ev.QueryEnd(); qe != nil {
            if qe.Outcome != nil {
                last = qe.Outcome
            }
            // 将子 Agent 结束事件转发到父事件流
            if spec.OutputCh != nil {
                childOK := last != nil && last.Termination == outcome.TerminationCompleted
                select {
                case spec.OutputCh <- &event.RuntimeEvent{
                    Type:  event.EventAgentTransfer,
                    RunID: parent.RunID,
                    Step:  0,
                    Payload: &event.AgentTransferPayload{
                        Phase:      event.TransferEnd,
                        FromAgent:  parent.AgentRole,
                        ToAgent:    childTmpl.Name,
                        ChildRunID: childRef.RunID,
                        Depth:      childRef.Depth,
                        OK:         childOK,
                    },
                }:
                default:
                }
            }
            continue
        }
```

**插入点 ③**：`childCtx.Done()` 路径——父级取消导致子 Agent 未正常产出 `QueryEnd` 时，发送失败 end。

位置：[spawn.go:L168-L176](file:///Users/admin/Documents/hower/rei/loopForge/pkg/agent/spawn.go#L168-L176) — `}` 闭合事件循环后、`if last == nil` **之前**。

```go
    // 父级取消导致子 Agent 未正常结束：发送失败 end 事件
    if last == nil && spec.OutputCh != nil {
        select {
        case spec.OutputCh <- &event.RuntimeEvent{
            Type:  event.EventAgentTransfer,
            RunID: parent.RunID,
            Step:  0,
            Payload: &event.AgentTransferPayload{
                Phase:      event.TransferEnd,
                FromAgent:  parent.AgentRole,
                ToAgent:    childTmpl.Name,
                ChildRunID: childRef.RunID,
                Depth:      childRef.Depth,
                OK:         false,
            },
        }:
        default:
        }
    }
```

#### 8.1.3 设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 通道发送模式 | 非阻塞 `select { case <-ch: default: }` | 父端 eventCh 满时不阻塞子 Agent goroutine，事件可丢弃（仅为 UI 观测性） |
| RunID | 使用 `parent.RunID` | 事件通过父的 eventCh 流转到 test-server，前端按 RunID 关联 |
| Step | 固定为 0 | Step 来源于父 RunLoop 累加器，子无法感知；对 spawn 事件 Step 无实际含义 |
| `Continue` 逻辑保持不变 | 仍 `continue` 跳过本地 QueryEnd | 不向父端重复发送 QueryEnd，避免双重终止信号 |

#### 8.1.4 AgentTransferPayload 字段映射

| Payload 字段 | start 时值 | end 时值（成功） | end 时值（失败） |
|-------------|-----------|-----------------|-----------------|
| `Phase` | `TransferStart` | `TransferEnd` | `TransferEnd` |
| `FromAgent` | `parent.AgentRole` | `parent.AgentRole` | `parent.AgentRole` |
| `ToAgent` | `childTmpl.Name` | `childTmpl.Name` | `childTmpl.Name` |
| `ChildRunID` | `childRef.RunID` | `childRef.RunID` | `childRef.RunID` |
| `Depth` | `childRef.Depth` | `childRef.Depth` | `childRef.Depth` |
| `OK` | — | `true` | `false` |

### 8.2 `loopForge/pkg/agent/spawn_tool.go` — OutputCh 通道打通

#### 8.2.1 修改位置

[spawn_tool.go:L120-L126](file:///Users/admin/Documents/hower/rei/loopForge/pkg/agent/spawn_tool.go#L120-L126) — `makeSpawnHandler` 闭包中 `spec` 构造块。

#### 8.2.2 变更代码

**当前**：
```go
    spec := &exchange.SpawnSpec{
        Task:            a.Task,
        SystemAddendum:  a.SystemAddendum,
        SkillIDs:        a.SkillIDs,
        ModelOverride:   a.Model,
        AllowChildSpawn: a.AllowChildSpawn,
    }
```

**修改为**（仅增一行 `OutputCh: eventCh`）：
```go
    spec := &exchange.SpawnSpec{
        Task:            a.Task,
        SystemAddendum:  a.SystemAddendum,
        SkillIDs:        a.SkillIDs,
        ModelOverride:   a.Model,
        AllowChildSpawn: a.AllowChildSpawn,
        OutputCh:        eventCh,
    }
```

#### 8.2.3 设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 设置在 `spec` 构造处（两条路径之前） | 一次设置 | 同步/异步两条路径都在调用 `sp.Spawn()` 前拿到相同的 `spec`；nil 的 `eventCh` 赋给 `OutputCh` 后安全（nil channel 发送永远走 `default`） |
| 无需在异步 goroutine 闭包内单独设置 | 闭包捕获 `spec` 指针 | goroutine 启动时 `spec.OutputCh` 已设置完毕 |

### 8.3 `test-server/static/index.html` — 前端 spawn 事件渲染

#### 8.3.1 CSS 变更

在 `<style>` 块末尾（`</style>` 之前）新增：

```css
    /* ── Spawn banner（子树级联） ── */
    .sys-info.sys-spawn { border-left-color: var(--accent); }
    .sys-tag.spawn   { background: rgba(108,92,231,0.12); color: var(--accent-light); }
```

复用了已有的 CSS 变量 `--accent` (#6c5ce7) 和 `--accent-light` (#a29bfe)，与 transfer 的绿色 `--success` 形成视觉区分。

#### 8.3.2 JS 变更

修改 `handleEvent` 中 `agent_transfer` case（[index.html:L1054-L1061](file:///Users/admin/Documents/hower/rei/test-server/static/index.html#L1054-L1061)）：

**当前**：
```javascript
        case 'agent_transfer': {
            sealAgentBubble();
            const from = data.from_agent || '';
            const to = data.to_agent || '';
            const reason = data.reason || '';
            addTransferBanner(from, to, reason);
            break;
        }
```

**修改为**：
```javascript
        case 'agent_transfer': {
            sealAgentBubble();
            if ((data.phase === 'start' || data.phase === 'end') && (data.depth || 0) > 0) {
                const isStart = data.phase === 'start';
                const childName = data.to_agent || 'child';
                const childID = data.child_run_id || '';
                if (isStart) {
                    addSysInfo('spawn', 'SPAWN',
                        childName + ' · depth=' + (data.depth || 0) +
                        (childID ? ' · ' + childID : ''));
                } else {
                    const ok = data.ok !== false;
                    addSysInfo('spawn', ok ? 'SPAWN ✓' : 'SPAWN ✗',
                        childName + (ok ? ' completed' : ' failed') +
                        (childID ? ' · ' + childID : ''));
                }
            } else {
                const from = data.from_agent || '';
                const to = data.to_agent || '';
                const reason = data.reason || '';
                addTransferBanner(from, to, reason);
            }
            break;
        }
```

#### 8.3.3 区分逻辑

| 条件 | 分支 |
|------|------|
| `data.phase ∈ {start, end}` **且** `data.depth > 0` | spawn 子树事件 → 渲染 `SPAWN` / `SPAWN ✓` / `SPAWN ✗` 横幅 |
| 其他 | 同级 transfer 事件 → 渲染原有 `from → to` 横幅 |

用 `phase` + `depth` 双条件判断而非仅 `depth`，避免未来 transfer 模式也携带 depth 时的误判。

#### 8.3.4 渲染效果

| 事件 | 左边框颜色 | 标签 | 详情内容 |
|------|----------|------|---------|
| spawn start | 紫色 `--accent` | `SPAWN` | `spawn-child · depth=1 · sse-session-spawn-171000` |
| spawn end (OK) | 紫色 `--accent` | `SPAWN ✓` | `spawn-child completed · sse-session-spawn-171000` |
| spawn end (FAIL) | 紫色 `--accent` | `SPAWN ✗` | `spawn-child failed · sse-session-spawn-171000` |

### 8.4 test-server SSE 序列化（无需变更）

`runtimeEventToSSE()` 中已有 `agent_transfer` 的序列化分支（[main.go:L617-L619](file:///Users/admin/Documents/hower/rei/test-server/main.go#L617-L619)）：

```go
    case event.EventAgentTransfer:
        if p := ev.AgentTransfer(); p != nil {
            out.Data = p
        }
```

`AgentTransferPayload` 的所有字段均有 `json` tag，`SSEEvent.Data` 是 `any` 类型直接赋值，JSON 序列化自动生效。**无需修改 main.go**。

### 8.5 数据流全貌（变更后）

```
┌─────────────────────────────────────────────────────────────────┐
│ test-server handleChat                                          │
│                                                                 │
│ ch := ag.Run(ctx, req)  ← 父 Agent 的 eventCh                  │
│                                                                 │
│ for ev := range ch {                                            │
│   switch ev.Type {                                              │
│   case EventToolCallStart: ...  ← spawn_subagent 工具调用       │
│   case EventAgentTransfer:    ← ★ 新增：spawn start/end         │
│     → runtimeEventToSSE → agent_transfer JSON → SSE → 前端      │
│   case EventAnswer: ...                                         │
│   case EventQueryEnd: ...    ← 父 Agent 结束                    │
│   }                                                             │
│ }                                                               │
└──────────────┬──────────────────────────────────────────────────┘
               │ eventCh (chan *RuntimeEvent, cap 256)
┌──────────────▼──────────────────────────────────────────────────┐
│ 父 Agent RunLoop                                                │
│   Step N: LLM 调用 spawn_subagent → ToolCallHandler             │
│     → spec.OutputCh = eventCh  ← 打通通道                       │
│     → sp.Spawn(ctx, parent, spec)                               │
│       → DefaultSpawner.Spawn():                                 │
│           ┌────────────────────────────────────┐                │
│           │ 子 Agent RunLoop (独立 goroutine)   │                │
│           │   EventStart  → spec.OutputCh ★新增 │                │
│           │   EventAnswer → 本地消费 (隔离)      │                │
│           │   CallLLM*    → 本地消费 (隔离)      │                │
│           │   ToolCall*   → 本地消费 + 记录      │                │
│           │   QueryEnd    → spec.OutputCh ★新增 │                │
│           └────────────────────────────────────┘                │
│     → 返回 SpawnResult（数据结果，给父 LLM）                      │
└─────────────────────────────────────────────────────────────────┘
```

### 8.6 执行环境与前置条件

| 项目 | 说明 |
|------|------|
| **Go 版本** | go 1.21+（与当前项目一致） |
| **依赖变更** | 无。仅使用已有的 `event`、`exchange`、`outcome` 包 |
| **编译命令** | `cd loopForge && go build ./...` + `cd ../test-server && go build ./...` |
| **启动命令** | `cd test-server && go run .`（地址默认 `:8191`） |
| **前置条件** | 无数据库/配置变更；spawn 模式已在 test-server 中可用 |
| **向后兼容** | `spec.OutputCh` 为 nil 时逻辑与当前完全一致 |

### 8.7 预期执行结果

test-server spawn 模式中发送"计算 7×8+3"→ 前端事件流出现：

```
[call_llm_start]    父 LLM 开始推理
[call_llm_end]      父 LLM 返回 tool_calls
[tool_call_start]   spawn_subagent{"task":"计算 7*8+3"}
[agent_transfer]    phase=start, from=spawn-parent, to=spawn-child, depth=1 ★新增
[agent_transfer]    phase=end, from=spawn-parent, to=spawn-child, ok=true    ★新增
[tool_call_end]     spawn_subagent output
[call_llm_start]    父 LLM 基于子结果总结
[answer]            最终回答
[query_end]         完成
```

**子 Agent 内部事件（answer / call_llm_* / tool_call_*）不出现在上述事件流中。**

---

## 9. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-04-29 | 初稿：v0.9.4 实施计划，目标为恢复 spawn 子 Agent start/end 消息的可观测性。 |
| 2026-04-29 | 补充 §8 实现细节：完整代码片段、插入点位置、设计决策、数据流图、执行环境、预期结果。 |
| 2026-04-29 | 代码实施完成，编译通过，单元测试全部 ok。 |

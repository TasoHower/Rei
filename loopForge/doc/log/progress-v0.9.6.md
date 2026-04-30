# loopForge 项目进度日志 — v0.9.6

> **版本**：v0.9.6  
> **日期**：2026-04-30（文档初始化）  
> **里程碑**：全量移除 agent-sdk-go 依赖，使用 OpenAI SDK 重新实现 ChatModel 兜底适配器  
> **上一版本**：[v0.9.5](progress-v0.9.5.md)（决策与设计文档修订）  
> **配套实施计划**：`doc/plan/plan-v0.9.6.md`

---

## 本版本目标

1. **新增 OpenAI SDK 兜底适配器**：使用 `github.com/openai/openai-go` 实现 `OpenAIChatModel`（`pkg/model/adapters/openai/`），完全替代 agent-sdk-go 的通用模型调用能力。
2. **新增 Runner 装配**：`pkg/runner/openai_default.go` 实现 `OPENAI_API_KEY` 等环境变量的自动装配，作为 Lark/DeepSeek 之后的最后兜底。
3. **全量移除 agent-sdk-go**：删除 `go.mod` 中 `github.com/agentizen/agent-sdk-go v0.17.0` 依赖，删除 `pkg/model/adapters/agentsdk/` 整个包（4 个文件）。
4. **文档全面更新**：修订 `doc/design/11-chatmodel.md`、`doc/design/00-abstractions.md`、`doc/decision/sdk-selection.md`、根目录 `README.md`。

---

## 关键变更清单

### Go 代码新增

| 文件 | 说明 |
|------|------|
| `pkg/model/adapters/openai/client.go` | `OpenAIChatModel` — 实现 `ToolCallingChatModel`（Generate + Stream + WithTools） |
| `pkg/model/adapters/openai/convert.go` | 消息转换：loopForge Message ↔ OpenAI API 格式、ToolInfo → OpenAI Tool |
| `pkg/model/adapters/openai/stream.go` | 流式处理：`chanStreamReader`、`toolCallAccumulator`、`runChatCompletionStream` |
| `pkg/runner/openai_default.go` | `ApplyOpenAIFromConfig` — 环境变量自动装配（OPENAI_API_KEY 等） |

### Go 代码删除

| 文件 | 说明 |
|------|------|
| `pkg/model/adapters/agentsdk/chatmodel.go` | SDKChatModel（agent-sdk-go Provider + Model 适配） |
| `pkg/model/adapters/agentsdk/convert.go` | MessagesToSDKRequest / FromSDKResponse 消息转换 |
| `pkg/model/adapters/agentsdk/settings.go` | CallConfig → sdkmodel.Settings 映射 |
| `pkg/model/adapters/agentsdk/stream_reader.go` | chanStreamReader（MessageStreamReader 实现） |

### Go 代码修改

| 文件 | 说明 |
|------|------|
| `go.mod` | 新增 `github.com/openai/openai-go`，删除 `github.com/agentizen/agent-sdk-go` |
| `go.sum` | `go mod tidy` 自动更新 |
| `pkg/model/types/toolinfo.go` | 第 12 行注释 agent-sdk-go → OpenAI SDK |
| `pkg/runner/runner.go` | 在 `Run()` 中追加 `applyDefaultOpenAIIfNeeded` 调用（最后兜底装配） |

### 文档变更

| 文件 | 说明 |
|------|------|
| `doc/design/11-chatmodel.md` | §5.3 替换为 OpenAI SDK 适配器；§5.4 对比表更新；§5.5 agent-sdk-go 移除说明；§7 追加 OpenAI 装配；§9 文件表更新 |
| `doc/design/00-abstractions.md` | § 模型层 agentsdk → OpenAI SDK |
| `doc/decision/sdk-selection.md` | 更新结论描述，agent-sdk-go 标注"已完全移除" |
| `README.md`（根目录） | 第 16 行、第 107 行技术栈表更新 |
| `doc/log/progress-v0.9.6.md` | 本文件 |

---

## 执行流程

⚠ 本版本采用**逐个作业确认制**。详见 `doc/plan/plan-v0.9.6.md` §3。执行顺序：

```
作业 #1  go.mod 新增 openai-go          → 你确认 → 我实施
作业 #2  创建 openai/ 适配器包(3文件)     → 你确认 → 我实施
作业 #3  新增 runner/openai_default.go    → 你确认 → 我实施
作业 #4  go.mod 清理 agent-sdk-go        → 你确认 → 我实施
作业 #5  删除 agentsdk/ 包               → 你确认 → 我实施
作业 #6  toolinfo.go 注释更新            → 你确认 → 我实施
作业 #7  11-chatmodel.md 修订            → 你确认 → 我实施
作业 #8  00-abstractions.md 修订         → 你确认 → 我实施
作业 #9  sdk-selection.md 修订           → 你确认 → 我实施
作业 #10 README.md 修订                  → 你确认 → 我实施
作业 #11 progress-v0.9.6.md 创建         → 你确认 → 我实施
作业 #12 最终验证                        → 你确认 → 我实施
```

---

## 当前代码状态

| 作业 | 状态 |
|------|------|
| 新增 OpenAI SDK 适配器 (openai-go + 3 文件) | ✅ 已完成 |
| 新增 Runner 装配 (openai_default.go) | ✅ 已完成 |
| 删除 agent-sdk-go (go.mod + agentsdk/ 4 文件) | ✅ 已完成 |
| 文档修订 × 4 + toolinfo.go 注释 | ✅ 已完成 |
| 最终验证 (build/vet/搜索) | ✅ 全部通过 |

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-30 | 新增 v0.9.6 进度文档，初始化阶段覆盖全量移除 agent-sdk-go 并以 OpenAI SDK 填充兜底的计划。 |

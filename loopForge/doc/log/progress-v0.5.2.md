# loopForge 项目进度日志 — v0.5.2

> **版本**：v0.5.2  
> **日期**：2026-04-16  
> **里程碑**：**Lark 模型适配器**（原 Doubao/方舟 OpenAI 兼容路径命名统一为 **Lark**）+ **火山引擎官方 Go SDK**（`github.com/volcengine/volcengine-go-sdk`）接入 Ark 对话能力  
> **上一版本**：v0.5.1（VarStore `{{}}` + SystemPromptBuilder）

---

## 本版本目标

1. **命名与包结构**：将现有 **`pkg/model/adapters/doubao`** 迁移为 **`pkg/model/adapters/lark`**（或经评审后的最终包名，如 `volcark`，但对外文档与用户心智统一为 **Lark 模型**）。类型与构造函数由 `ArkChatModel` / `NewArkChatModel` 等调整为 **Lark** 语义（例如 `LarkChatModel`、`NewLarkChatModel`，具体以 API 评审为准），并全仓替换 import 与 demo（`cmd/agentdemo`、`cmd/doubaodemo`、`cmd/doubaodemo` 目录与 `loopforged` 内 Doubao 命名）。
2. **官方 SDK**：不再通过 **`agent-sdk-go` 的 OpenAI Provider** 调用方舟 OpenAI 兼容 HTTP；改为使用 **`github.com/volcengine/volcengine-go-sdk`** 中与 **Ark / LLM 对话**相关的官方客户端（通常为 **`service/arkruntime`** 下的 Chat Completions、流式响应等，以仓库当前版本 API 为准），实现 `pkg/model` 的 **`ToolCallingChatModel`**。
3. **能力对齐**：在官方 SDK 上保持与 v0.5.1 一致的上层行为：**流式输出**、**工具调用**（含 schema 与 tool 消息回灌）、**CallOption**（temperature、max_tokens、model 等）映射到 SDK 请求字段；token 用量若 SDK 暴露则继续填入 `model.Message` / metrics。
4. **配置与环境变量**：文档与示例中将 **`DOUBAO_*` / 纯「Doubao」文案** 迁移为 **`LARK_*` 或 `ARK_*`**（二选一或兼容两者，见交付清单）；默认 Base URL、Region、鉴权方式与 **官方 SDK 示例**一致。
5. **依赖与体积**：`go.mod` 增加 `volcengine-go-sdk`；评估 **`doubao` 适配器路径删除后**，是否仍需要 **`agent-sdk-go` 仅服务于 `pkg/model/adapters/agentsdk`**——若 Lark 实现 **不依赖** `SDKChatModel`，可收缩依赖或保留 agentsdk 供其他 Provider 使用（需单独结论）。

---

## 核心概念

### 为何要换 SDK

- 当前 `doubao` 包本质是 **OpenAI 兼容 BaseURL + agent-sdk-go `openai.Provider`**，属于间接路径。
- **官方 `volcengine-go-sdk`** 提供 **Ark 运行时（arkruntime）** 等一等公民 API，便于对齐签名、重试、区域与后续火山侧能力（以实际模块为准）。

### 与 `pkg/model` 边界

- **不变**：`ToolCallingChatModel`、`types.Message`、Agent loop、Runner。
- **变**：`lark` 包内部实现：请求/响应/流 **从 agent-sdk-go 转写为 volcengine-go-sdk 调用**；若流式协议与 OpenAI 兼容层字段名不同，在 **adapter 内**完成映射，不泄漏到 `pkg/agent`。

### 模块关系（示意）

```
  loopForge/pkg/model (ToolCallingChatModel)
           │
           ▼
  pkg/model/adapters/lark   ← 基于 volcengine-go-sdk / arkruntime
           │
           ▼
  火山引擎 Ark（Lark LLM 推理端点）
```

---

## 交付清单

- [x] `pkg/model/adapters/lark/` — 基于 `github.com/volcengine/volcengine-go-sdk` / `arkruntime` 的 `ToolCallingChatModel`（`Generate` + `Stream`，tools + `StreamOptions.IncludeUsage`）。
- [x] 删除 `pkg/model/adapters/doubao/`，全仓改为 `lark`。
- [x] `cmd/agentdemo`、`test-server`、`cmd/doubaodemo`、`cmd/loopforged/*` — **LARK_*** 为主，`DOUBAO_*` / `ARK_*` 兼容。
- [x] `go.mod` — 直接依赖 `volcengine-go-sdk`；`agent-sdk-go` 仍被 `pkg/model/adapters/agentsdk` 与 `loopforged` mock 等使用。
- [ ] 单元或集成测试：针对 `lark` 包的 mock HTTP（可选）或录制响应。
- [ ] 文档：`doc/decision/sdk-selection.md` 补充 Lark 段落（可选）。
- [x] 验收：`doc/acceptance/v0.5.2-acceptance.md`。

---

## 风险与待决

| 项 | 说明 |
|----|------|
| API 差异 | 官方 SDK 的流式 chunk、tool_calls 字段与当前 OpenAI 兼容层可能不一致，需在 adapter 内做完整映射与回归。 |
| 鉴权 | 确认使用 **API Key** 还是 **AK/SK + Region**；与现有 `DOUBAO_API_KEY` 用户迁移策略。 |
| 包名 | `lark` 与飞书产品线名称可能混淆；若团队更倾向 `volcark` / `ark`，可在实现前定稿。 |

---

## 变更日志

| 日期 | 说明 |
|------|------|
| 2026-04-16 | 初稿：v0.5.2 小版本规划（Lark 命名 + volcengine-go-sdk）。 |
| 2026-04-16 | 落地：`pkg/model/adapters/lark`，移除 `doubao`；demo / test-server / loopforged 环境与文档更新。 |

# loopForge 项目进度日志 — v0.0.1

> **版本**：v0.0.1  
> **日期**：2026-04-15  
> **里程碑**：设计文档基线（文档先行，无实现代码）  
> **上一版本**：—

---

## 本版本目标

建立 **Multi-Agent 引擎** 的设计与契约文档：原生 **Agent loop**（`agent-sdk-go`）、**MCP** 能力接入、**动态 spawn**、**Skills**、**可调试 UI** 对标思路、与现网 **runner 事件类型** 对齐；明确 **不包含** Graph 编排内核与引擎内落库/Push。

---

## 交付物（文档）

| 路径 | 内容 |
|------|------|
| `doc/design/multi-agent-engine.md` | 能力设计、loop/spawn/skills/MCP、Dev UI、阶段性目标、验收 |
| `doc/design/architecture.md` | 系统拓扑、模块边界、与 SeRagLF 分工 |
| `doc/design/abstractions.md` | MVP 最小内核、`Runtime*`、Agent 交换、Tool、MCP 契约 |
| `doc/design/data-fusion.md` | `EventMessage` / `EventMessageType` 与三层 id **类型语义**对齐（不含 Push） |
| `doc/decision/sdk-selection.md` | agent-sdk-go 选型、与 Eino 差异 |

---

## 未包含（后续版本）

- Go 模块与 `go.mod`、可运行二进制
- 单测与 CI
- 与 SeRagLF MCP 的联调实现

---

## 备注

本版本 **仅文档**，便于评审架构与数据契约后再开工实现。

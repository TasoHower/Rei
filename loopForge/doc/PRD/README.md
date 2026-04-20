# loopForge 产品需求文档（PRD）目录

本目录存放 loopForge 各版本的产品需求文档（Product Requirements Document）。

## 文档结构

每个 PRD 文档遵循以下命名规范：

```
prd-v{major}.{minor}.{patch}-{feature-name}.md
```

例如：
- `prd-v0.7.0-skills.md` — v0.7.0 Skills 功能
- `prd-v0.6.0-mcp.md` — v0.6.0 MCP 客户端与工具接入
- `prd-v0.5.0-variable.md` — v0.5.0 Variable 共享变量

## PRD 文档结构

每个 PRD 文档包含以下章节：

1. **概述** — 产品定位、核心价值、与前后版本的关系
2. **需求范围** — 本版本交付（P0/P1）、不在本版本范围
3. **用户故事** — 作为开发者/Agent，我希望...（含验收标准）
4. **功能需求** — 详细功能描述、数据结构、接口设计、示例代码
5. **技术需求** — 并发安全、错误处理、观测性等
6. **验收标准** — 功能/性能/兼容性验收
7. **风险与待决** — 已知风险及缓解措施
8. **参考文档** — 关联的进度日志、设计文档、验收文档

## 版本演进

查看完整的版本演进流程：[**evolution.md**](./evolution.md)

演进总览：
- **v0.1.0** — 可运行基座
- **v0.2.0** — 流式事件驱动
- **v0.3.0** — Agent Transfer（Multi-Agent）
- **v0.4.0** — Agent 架构统一
- **v0.4.1** — 包拆分 + 命名统一 + 结构化日志
- **v0.5.0** — Variable 共享变量
- **v0.6.0** — MCP 客户端与工具接入
- **v0.7.0** — Skills 技能包

## 版本对应关系

| PRD 文档 | 进度日志 | 验收文档 | 设计文档 |
|----------|----------|----------|----------|
| [prd-v0.7.0-skills.md](./prd-v0.7.0-skills.md) | [progress-v0.7.0.md](../log/progress-v0.7.0.md) | [v0.7.0-acceptance.md](../acceptance/v0.7.0-acceptance.md) | [multi-agent-engine.md#3.6](../design/multi-agent-engine.md) |
| [prd-v0.6.0-mcp.md](./prd-v0.6.0-mcp.md) | [progress-v0.6.0.md](../log/progress-v0.6.0.md) | [v0.6.0-acceptance.md](../acceptance/v0.6.0-acceptance.md) | [mcp-tool-unification.md](../design/mcp-tool-unification.md) |
| [prd-v0.5.0-variable.md](./prd-v0.5.0-variable.md) | [progress-v0.5.0.md](../log/progress-v0.5.0.md)<br>[progress-v0.5.1.md](../log/progress-v0.5.1.md)<br>[progress-v0.5.2.md](../log/progress-v0.5.2.md) | [v0.5.0-acceptance.md](../acceptance/v0.5.0-acceptance.md)<br>[v0.5.1-acceptance.md](../acceptance/v0.5.1-acceptance.md)<br>[v0.5.2-acceptance.md](../acceptance/v0.5.2-acceptance.md) | — |
| [prd-v0.4.1-refactor.md](./prd-v0.4.1-refactor.md) | [progress-v0.4.1.md](../log/progress-v0.4.1.md) | — | — |
| [prd-v0.4.0-agent-unify.md](./prd-v0.4.0-agent-unify.md) | [progress-v0.4.0.md](../log/progress-v0.4.0.md) | — | — |
| [prd-v0.3.0-transfer.md](./prd-v0.3.0-transfer.md) | [progress-v0.3.0.md](../log/progress-v0.3.0.md) | — | — |
| [prd-v0.2.0-streaming.md](./prd-v0.2.0-streaming.md) | [progress-v0.2.0.md](../log/progress-v0.2.0.md) | — | — |
| [prd-v0.1.0-baseline.md](./prd-v0.1.0-baseline.md) | [progress-v0.1.0.md](../log/progress-v0.1.0.md) | — | — |

## 与进度日志的区别

| 维度 | PRD 文档 | 进度日志（`doc/log/`） |
|------|----------|----------------------|
| **目标读者** | 产品经理、开发者、测试人员 | 开发者、技术决策者 |
| **内容重点** | 产品需求、用户故事、功能描述、验收标准 | 技术决策、代码改造计划、接口预留 |
| **写作时机** | 版本开发前（需求冻结后） | 版本开发过程中（持续更新） |
| **更新频率** | 版本发布前基本不变 | 随开发进度持续更新 |
| **详细程度** | 详细的产品功能和验收标准 | 详细的技术实现方案 |

## 使用指南

### 产品经理

1. 在版本规划阶段编写 PRD 文档
2. 明确用户故事和验收标准
3. 与开发团队对齐需求范围
4. 版本发布前更新状态为「已完成」

### 开发者

1. 开发前阅读 PRD 文档了解需求
2. 参考进度日志了解技术实现方案
3. 实现功能时对照验收标准
4. 如有需求变更，更新 PRD 文档

### 测试人员

1. 根据 PRD 文档编写测试用例
2. 对照验收标准进行验收测试
3. 记录测试结果并反馈

## 文档状态

每个 PRD 文档在 front matter 中标注状态：

- **计划稿** — 需求尚未冻结，可能变更
- **评审中** — 需求已提交评审，等待反馈
- **已确认** — 需求已冻结，进入开发
- **已完成** — 功能已开发完成，验收通过
- **已废弃** — 需求已取消

## 版本命名

遵循语义化版本号（Semantic Versioning）：

- **Major**（主版本号）：不兼容的重大变更
- **Minor**（次版本号）：向后兼容的功能性新增
- **Patch**（修订号）：向后兼容的问题修正

例如：`v0.7.0` 表示第 7 个 Minor 版本，第 0 个 Patch 版本。

## 相关文档

- [项目进度日志](../log/) — 各版本技术实现方案
- [验收文档](../acceptance/) — 各版本验收标准
- [设计文档](../design/) — 架构设计、技术方案
- [决策文档](../decision/) — 技术决策记录（ADR）

## 维护指南

### 新增 PRD 文档

1. 复制模板文件（如有）
2. 填写 front matter（版本、日期、里程碑、状态）
3. 编写各章节内容
4. 更新本 README 的「版本对应关系」表格

### 更新 PRD 文档

1. 确认变更内容（需求变更/状态更新）
2. 更新 front matter 状态
3. 在变更日志中记录变更

---

**最后更新**：2026-04-17

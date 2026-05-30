# 单能力 Tasks

## 实现目标

当前不直接实现独立笔记域；先固定番茄 note 的事实源、回填规则和未来文件范围，避免后续误迁移。

## 前置依赖

- 产品确认独立 note 域进入范围。
- 先明确首阶段 note 只支持哪类 source（建议仅 `pomodoro`）。

## 任务切片

### Task 1

- 目标：固定迁移规则并补数据判定。
- 文件：`docs/superpowers/specs/feature-audit/07-utilities/7.3-notes/`、`server/internal/handler/pomodoro.go`
- 改动：明确哪些 `time_entry.description` 视为用户显式 note，哪些属于默认文案。
- 完成定义：迁移脚本可据此筛出候选记录。
- 验证方式：设计评审通过，样本数据人工复核。

### Task 2

- 目标：设计独立 note 实体与接口。
- 文件：`server/pkg/db/queries/`、`server/internal/handler/`、`server/internal/service/`
- 改动：定义 note 表、source backlink、列表与详情接口。
- 完成定义：note entity 能表达 `source_type/source_ref`。
- 验证方式：schema 审核与接口测试计划通过。

### Task 3

- 目标：设计回填或双写方案。
- 文件：`server/internal/handler/pomodoro.go`、future migration runner、`apps/workspace/src/features/time-tracking/`
- 改动：确认是一次性 backfill，还是 note 域上线后短期双写。
- 完成定义：旧数据迁移不丢失，新增数据有稳定 source linkage。
- 验证方式：迁移演练与回滚方案通过。

### Task 4

- 目标：补 notes UI 与来源追踪。
- 文件：`apps/workspace/src/features/notes/` future 目录、`apps/workspace/src/router.tsx`
- 改动：新增列表、详情与从 pomodoro 来源跳转。
- 完成定义：用户能区分“独立笔记”与“时间记录描述”。
- 验证方式：前端测试与手动验收。

### Task 5

- 目标：回写文档。
- 文件：`docs/superpowers/specs/feature-audit/07-utilities/7.3-notes/spec.md`、`docs/superpowers/specs/feature-audit/07-utilities/overview.md`
- 改动：更新优先级、状态与迁移策略执行情况。
- 完成定义：文档与实现一致。
- 验证方式：人工复核。

## 执行顺序说明

先定迁移规则，再建 note 实体，再决定回填或双写，最后才是 UI。7.3 不允许从页面先行倒推数据边界。

## 回写要求

- 若未来进入实现，先更新本 `design.md` 中“低优先级 / 非当前阶段”状态。
- 回写范围只限 `07-utilities` 模块文档。

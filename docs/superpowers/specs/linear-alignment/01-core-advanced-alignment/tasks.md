# 单能力 Tasks

## 实现目标

按 P0→P1 顺序补齐 Linear Core + Advanced 对齐的关键能力，使主流程闭环、计划能力和复盘能力可用。

## 前置依赖

- 关闭未决策项：
  - 估算维度选型（points/minutes）
  - Roadmap 首版范围（项目内或跨项目）
  - Automation 规则 MVP 复杂度（单条件或多条件）
- 确认执行以 `design.md` 为唯一设计输入，不允许自行扩范围。

## 任务切片

### Task 1（P0）：Issue 归档生命周期

- 目标：将 issue 从物理删除改为归档生命周期，支持恢复与视图过滤。
- 文件：
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
  - `apps/workspace/src/features/issues/**`
  - `apps/workspace/src/shared/types/issue.ts`
- 改动：
  - 增加 archive/unarchive 行为及列表过滤契约。
  - 前端增加归档动作和归档视图入口。
- 完成定义：
  - 删除不再直接物理删除。
  - 用户可归档并恢复 issue。
- 验证方式：
  - 后端 handler/query 测试覆盖归档路径。
  - 前端页面交互和 E2E 覆盖归档与恢复路径。

### Task 2（P0）：Cycle 基础能力

- 目标：引入 cycle 对象并支持 issue 绑定 cycle。
- 文件：
  - `server/migrations/**`
  - `server/pkg/db/queries/**`
  - `server/internal/handler/**`
  - `apps/workspace/src/features/issues/**`
  - `apps/workspace/src/router.tsx`
- 改动：
  - 新增 cycle 数据模型、CRUD 接口、issue 关联字段与筛选。
  - 新增 cycle 入口页或子视图。
- 完成定义：
  - 可创建 cycle。
  - issue 可关联/取消关联 cycle。
- 验证方式：
  - Go 测试覆盖 cycle API。
  - 前端测试与 E2E 覆盖 cycle 绑定流程。

### Task 3（P1）：Triage + Automation 首版规则化

- 目标：补齐 inbox triage 动作，并把 automation 从模板开关升级到规则 MVP。
- 文件：
  - `apps/workspace/src/features/inbox/**`
  - `apps/workspace/src/features/automation/**`
  - `server/internal/automation/**`
  - `server/pkg/db/queries/automation.sql`
  - `server/internal/handler/automation.go`
- 改动：
  - triage 新增 snooze、批量处理、分流动作。
  - automation 新增最小规则结构（条件 + 动作）。
- 完成定义：
  - 用户可在 inbox 完成 triage 最小闭环。
  - 可创建并启用至少一种规则化 automation。
- 验证方式：
  - 前后端测试覆盖规则创建与触发。
  - E2E 覆盖 triage 主路径。

### Task 4（P1）：Estimation + Insights 基线

- 目标：补齐估算字段与任务流基础指标。
- 文件：
  - `apps/workspace/src/shared/types/issue.ts`
  - `server/pkg/db/queries/issue.sql`
  - `server/pkg/db/queries/**`（新增指标聚合）
  - `server/internal/handler/**`
  - `apps/workspace/src/features/**`（统计页面）
- 改动：
  - issue 支持估算字段。
  - 新增 throughput/lead-time/cycle-time 聚合接口与页面展示。
- 完成定义：
  - 估算数据可被录入和查询。
  - 统计页可展示任务流基础指标。
- 验证方式：
  - SQL/query/handler 测试验证指标口径。
  - 页面级测试验证筛选、空态和错误态。

### Task 5（P1/P2）：Roadmap 与协作体验增强

- 目标：补齐 roadmap 首版可视化与命令面板增强能力。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/projects/**`
  - `apps/workspace/src/features/layout/**`
  - `apps/workspace/src/features/search/**`
  - `server/internal/handler/project.go`
- 改动：
  - 新增 roadmap 入口与时间轴基础展示。
  - 在 Cmd+K 基础上增加统一命令动作集。
- 完成定义：
  - 用户可进入 roadmap 查看项目规划。
  - 命令面板可执行关键高频动作。
- 验证方式：
  - 前端交互测试覆盖 roadmap 与命令动作。
  - E2E 验证导航与关键命令路径。

## 执行顺序说明

先做 Task 1 和 Task 2 是因为它们决定主流程与数据模型基础。Task 3 和 Task 4 依赖前两项的实体与状态契约。Task 5 依赖前述能力稳定后再补体验层，避免返工。

## 回写要求

- 每个任务完成后更新：
  - `docs/superpowers/specs/linear-alignment/module-overview.md` 能力状态
  - `docs/superpowers/specs/linear-alignment/01-core-advanced-alignment/spec.md` 缺口状态
- 若实现中变更了设计边界，必须先更新 `design.md` 后再继续编码。
- 所有新增能力必须补充对应证据路径和符号名，保持可追溯。

# Planning Insights Spec

## 背景

Multica 已经有 issue、project、time entry、pomodoro 等执行数据，但下一里程碑需要让这些数据支持计划和复盘，而不是只作为操作记录存在。`docs/superpowers` 中的估算、roadmap、任务流指标和 analytics 缺口应合并成一个更小的 planning insights 基线。

## 范围

本能力定义：

- issue 估算字段。
- project health summary。
- 任务流指标：throughput、lead time、cycle time。
- 轻量 roadmap/timeline 只读视图。
- analytics API 和 workspace UI 入口。

不定义完整 cycle/iteration 系统，不定义企业级报表，不新增独立 roadmap card 实体。

## 当前状态

- 状态：部分完成。
- 已完成：project CRUD、project board、project progress、project time stats、team time stats、pomodoro stats。
- 缺失：issue estimate、任务流聚合、project health 统一 API、roadmap/timeline 入口。

## 证据

- `apps/workspace/src/shared/types/issue.ts` `Issue`：包含 priority、project_id、due_date、start_date、end_date，但没有 estimate 字段。
- `apps/workspace/src/shared/types/project.ts` `Project`：项目模型覆盖 title、description、status、lead，但没有 health、milestone、timeline 字段。
- `apps/workspace/src/features/projects/components/projects-page.tsx` `ProjectDetailPanel`：项目详情已有 progress 和 time stats 起点。
- `server/internal/handler/time_entry.go` `GetTeamTimeStats`：团队统计只返回 by_user/by_project 工时聚合。
- `server/pkg/db/queries/issue.sql` `ListIssues`：issue 查询用于列表和筛选，不提供 lead time、cycle time、throughput 聚合。
- `apps/workspace/src/router.tsx` `routeTree`：已有 `/projects`、`/team-time`、`/pomodoro`，没有 `/analytics` 或 `/roadmap`。

## 缺口

1. 计划缺口：没有 estimate，无法做容量和投入判断。
2. 统计缺口：现有统计偏工时，不覆盖任务流效率。
3. 项目缺口：项目详情能展示局部进度，但没有统一 health summary。
4. 路线图缺口：没有以项目和时间窗口组织 issue 的 timeline/roadmap。
5. 验证缺口：缺少服务端聚合契约，不能靠前端分页列表拼统计。

## 交接说明

执行 Agent 必须先实现服务端聚合契约，再接前端展示。禁止在前端基于当前页 issue list 拼全局统计。

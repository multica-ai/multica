# Planning Insights Research

## 调研目标

确认当前 issue、project、time entry、pomodoro 的规划和统计能力边界，找出下一里程碑最小 planning insights 可以复用的代码路径。

## 现状链路

当前计划与统计分散在三个面：

1. Projects：项目 CRUD、项目详情、project board、project progress、project time stats。
2. Time tracking：team time stats 和 my time 操作页。
3. Pomodoro：专注操作页和轻量统计摘要。

这些面都有局部数据，但还没有统一的 planning/analytics 入口。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | 没有 `estimate` / `estimated_minutes` / `story_points` 字段。 |
| `apps/workspace/src/shared/types/project.ts` | `Project` | 项目缺少 health、timeline、milestone 类规划字段。 |
| `apps/workspace/src/features/projects/components/projects-page.tsx` | `ProjectDetailPanel` | 项目详情已能承接 progress/time stats，是 project health 的起点。 |
| `apps/workspace/src/features/projects/components/project-progress.tsx` | `ProjectProgress` | 当前进度可由 issue 状态推导，但不是服务端 health 契约。 |
| `server/internal/handler/time_entry.go` | `GetTeamTimeStats` | 只聚合 by_user/by_project 工时，不含任务流指标。 |
| `server/pkg/db/queries/pomodoro.sql` | `GetPomodoroStats` | 番茄统计是 today/week/total_seconds 级别，不是 planning insight。 |
| `server/pkg/db/queries/issue.sql` | `ListIssues` | issue 查询仍以列表过滤为主，不提供 analytics buckets。 |
| `apps/workspace/src/router.tsx` | `routeTree` | 没有 analytics 或 roadmap 专属路由。 |

## 数据模型或状态流

可复用实体：

- `issue`: status、priority、project_id、start_date、end_date、archived_at、created_at、updated_at。
- `project`: status、lead、description。
- `time_entry`: duration、project/user 聚合。
- `pomodoro`: completed work sessions 和 total seconds。

缺少实体或字段：

- issue estimate。
- status transition timestamp 或可推导的 lifecycle event。
- project health summary。
- analytics bucket query。

## 边界条件

- lead time 和 cycle time 必须定义起止点。
- archived issues 是否参与统计必须显式定义。
- project health 不能只基于前端当前筛选结果。
- roadmap 第一版应以现有 issue dates/project dates 为基础，不创建独立 roadmap card。

## 未决问题

1. estimate 使用 story points、minutes，还是二者只选一种？
2. cycle time 起点是 `in_progress` 状态首次进入时间，还是 issue created_at？
3. roadmap 第一版是否需要项目级 date range，还是只用 issue start/end/due dates？

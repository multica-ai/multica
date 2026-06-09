# Planner Scheduler Timeboxing

## 目标与范围

本 change 设计 Multica 对齐 Super Productivity planning / scheduler / timeboxing 的下一阶段能力。范围覆盖：

- deadline visibility：今日到期、已逾期、未来 deadline 的统一提示与入口。
- calendar overlays：在工时日历中叠加任务 schedule / due 信息，形成计划与实际的同屏对照。
- daily-weekly planner：把现有 AI 明日计划从 Markdown 面板升级为可操作的日/周计划。
- timeboxing foundation：建立 planned time block 数据模型和交互契约，让任务可以被安排到具体时间块。

本 change 只创建设计文档，不实现代码。后续实现必须从对应能力包的 `design.md` 和 `tasks.md` 进入。

## 总体 ASCII 图

### 能力推进关系

```text
                    +----------------------+
                    | issue due/start/end  |
                    +----------+-----------+
                               |
                               v
                    +----------------------+
                    | deadline visibility  |  P0 / small
                    +----------+-----------+
                               |
                               v
+-------------------+   +----------------------+   +----------------------+
| actual time entry |-->| calendar overlays    |-->| timeboxing foundation|
| My Time Calendar  |   | planned vs actual UI |   | planned_time_block  |
+-------------------+   +----------+-----------+   +----------+-----------+
                               ^                              ^
                               |                              |
                    +----------+-----------+                  |
                    | daily / weekly plan  |------------------+
                    | daily_plan_item      |
                    +----------------------+
```

### 数据语义边界

```text
  issue
    | due/start/end
    v
  schedule visibility  -----------+
                                  |
  daily_plan + daily_plan_item ---+--> planned_time_block --> start timer
                                                           |
                                                           v
                                                     time_entry

  planned_time_block = 要做什么、计划什么时候做
  time_entry         = 实际做了什么、实际花了多久
```

### 页面入口关系

```text
  Today Page        -> DeadlineBanner -> issue detail
  My Time Page      -> DeadlineBanner -> issue detail
  My Time Calendar  -> TimeEntry events + Issue overlays + Planned blocks
  Planner / Day     -> Daily plan items -> Schedule timebox
  Planner / Week    -> Daily summaries  -> Planner / Day
```

## 能力列表

| 能力 | 当前状态 | 优先级 | 改动量 | 备注 |
| --- | --- | --- | --- | --- |
| Deadline visibility | 部分完成 | P0 | 小 | 已有 due/start/end 与 overdue 计算，缺统一 banner 和 planner deadline section |
| Calendar overlays | 部分完成 | P0 | 小到中 | 已有任务日历和工时日历，缺合并 overlay |
| Daily / weekly planner | 部分完成 | P1 | 中 | 已有 AI 明日计划草稿，缺结构化计划项与周视图 |
| Timeboxing foundation | 未完成 | P1 | 大 | 已有 time entry 拖拽日历，缺 planned block 模型 |

## 当前状态证据

### Deadline visibility

- 证据：`apps/workspace/src/shared/types/issue.ts` `Issue`。
  当前行为：Issue 已有 `due_date`、`start_date`、`end_date`。
  当前缺口：没有 deadline 专属字段、提醒策略、banner 状态或 dismiss 状态。
- 证据：`apps/workspace/src/features/issues/utils/workbench-view.ts` `deriveTodayIssues` / `deriveUpcomingIssues` / `isIssueScheduleOverdue`。
  当前行为：前端已能从 issue 日期推导 today、upcoming、overdue。
  当前缺口：这些工具函数只服务任务列表展示，没有形成跨页面 deadline banner。
- 证据：`server/pkg/db/queries/issue.sql` `ListIssues` / `ListArchivedIssues`。
  当前行为：后端列表查询支持 due/start/end 日期过滤。
  当前缺口：没有 deadline summary 聚合接口。

### Calendar overlays

- 证据：`apps/workspace/src/features/issues/components/IssueCalendarPage.tsx` `IssueCalendarPage` / `issueToEvent`。
  当前行为：`/calendar` 可以把带 `start_date` / `end_date` 的 issue 显示成全天事件。
  当前缺口：没有展示 `due_date`，也没有与工时日历合并。
- 证据：`apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` `MyTimeCalendarPage`。
  当前行为：`/my-time/calendar` 使用 `BigDnDCalendar` 展示 time entry，并支持拖拽和 resize 已完成工时。
  当前缺口：只展示 actual work，不展示 planned issue schedule / due overlay。
- 证据：`apps/workspace/src/components/ui/big-calendar.tsx` `BigCalendar` / `BigDnDCalendar`。
  当前行为：已有 react-big-calendar 封装，可以承载日/周/月/agenda 与 DnD。
  当前缺口：缺少 overlay event 类型与只读/可写事件分层。

### Daily / weekly planner

- 证据：`server/migrations/039_daily_plan.up.sql` `daily_plan`。
  当前行为：已有 `daily_plan` 表，包含 `plan_date`、`draft_content`、`top_issue_ids`、`status`。
  当前缺口：计划内容是 Markdown 文本，不是结构化计划项。
- 证据：`server/internal/service/daily_plan.go` `DailyPlanService.GeneratePlanDraft` / `ConfirmPlan`。
  当前行为：可生成和确认明日计划。
  当前缺口：服务只支持单日草稿，没有周计划、计划项编辑、计划项状态。
- 证据：`apps/workspace/src/features/daily-plan/components/DailyPlanPanel.tsx` `DailyPlanPanel`。
  当前行为：My Time 页展示明日计划面板，可生成/确认。
  当前缺口：计划不可作为可操作列表，也不能转成 timebox。

### Timeboxing foundation

- 证据：`apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` `handleEventDrop` / `handleEventResize`。
  当前行为：已完成 time entry 可以在日历里拖拽和 resize，并写回 `start_time` / `stop_time`。
  当前缺口：这是 actual time entry，不是 planned time block。
- 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-entry-actions.ts` `requestStart`。
  当前行为：可以从任务或描述启动实际计时。
  当前缺口：启动计时不关联计划时间块，也不能记录 planned vs actual。
- 证据：`server/pkg/db/queries/time_entry.sql` `CreateTimeEntry`。
  当前行为：time entry 记录实际工作开始/停止。
  当前缺口：没有 planned block 表，也没有 planned block 与 time entry 的关联。

## 外部功能基线

- Super Productivity 官方功能页把 schedule planner、time tracking、pomodoro、integrations 作为核心导航，并说明支持 Kanban、Eisenhower、compact lists、自定义布局、repeating tasks、time tracking、cross-platform sync。
- Super Productivity time boxing 页面明确描述：把任务拖入 schedule、分配 time boxes、日/周视图、estimated vs actual、overload warnings、focus timer 与 time tracking 数据闭环。
- Super Productivity v17.5 release notes 提到 Task Deadlines：task list badge、detail panel、approaching reminders、today banner、planner overdue section、context menu deadline。

参考来源：

- https://super-productivity.com/
- https://super-productivity.com/use-cases/time-boxing/
- https://super-productivity.com/guides/time-boxing-method/
- https://github.com/super-productivity/super-productivity/releases

## 非目标

- 不实现 Super Productivity 的完整外部 Calendar / CalDAV 同步。
- 不实现 GitHub/Jira/GitLab/OpenProject issue provider 导入。
- 不实现 Focus Mode 全屏专注页；它应归入后续 focus-mode-flowtime spec。
- 不实现 Pomodoro 规则变化；本 change 只要求 timebox 可选择启动现有 timer。
- 不引入个人本地离线数据库或跨端同步。
- 不把 daily plan Markdown 立即删除；迁移需要兼容现有 plan 数据。

## 优先级与推进顺序

1. 先做 deadline visibility。原因：已有 issue 日期字段和 overdue 工具函数，改动最小，能快速建立 planner 的紧迫感入口。
2. 再做 calendar overlays。原因：已有两个日历页面，先让 planned schedule 和 actual time 同屏，为后续 timebox 做视觉基线。
3. 再做 daily / weekly planner。原因：需要把 Markdown 草稿升级为结构化计划项，涉及数据模型但仍可独立验收。
4. 最后做 timeboxing foundation。原因：需要新 planned block 模型，并要和 issue、daily plan、time entry、timer 串联。

## 共享约束

- 所有数据必须以 `workspace_id` 为租户边界。
- 所有计划数据必须绑定当前 user；团队可见性要作为后续明确决策，不默认公开个人计划。
- 第一版必须复用现有 `issue`、`time_entry`、`daily_plan`、calendar 组件，不新建平行任务系统。
- planned block 与 actual time entry 必须分开建模，避免把“计划”误写成“已工作”。
- 前端展示可以先本地派生；跨页面 summary、timeboxing、weekly planner 必须有服务端契约。
- 执行时如果发现需要改变数据模型，必须先更新对应能力包的 `design.md` 和 `tasks.md`。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 直接把 time entry 当 timebox | 实际工时与计划混淆，统计不可用 | 新增 planned block 模型，time entry 只作为 actual |
| 日计划仍是 Markdown | 无法拖拽、排序、转 timebox | 增加结构化 plan item，保留 Markdown 作为生成摘要或历史兼容 |
| Calendar overlay 过早可编辑 | 用户可能误以为拖动 overlay 已排程 | 第一版 overlay 只读，timeboxing foundation 再启用拖拽 |
| Deadline 口径与 due/start/end 混杂 | UI 提示不稳定 | deadline visibility 先定义统一优先级：overdue due/end > today due/end > active window > upcoming |
| 周计划范围膨胀 | 需要复杂 capacity / recurrence | 第一版周计划只做按天分组和容量摘要，不做 recurring workload |

## 回写规则

- 每个能力实现后，更新对应 `spec.md` 的当前状态与缺口。
- 如果实现中新增或调整数据库表，更新对应 `research.md` 的数据模型链路。
- 如果 UI 交互与设计不同，先更新对应 `design.md` 的交互流程，再继续实现。
- 每个实现切片完成后，按 `tasks.md` 的验证方式记录实际验证命令和结果。

# Planner Scheduler Timeboxing

## 目标与范围

本 change 只保留不会引入平行执行对象的 schedule 可视化能力。范围覆盖：

- deadline visibility：今日到期、已逾期、未来 deadline 的统一提示与入口。
- calendar overlays：在工时日历中叠加任务 schedule / due 信息，形成计划与实际的同屏对照。

结构化 daily/weekly planner、结构化计划项、计划块和 timebox 执行模型已暂停，不能作为后续实现入口。若未来重新设计，必须先更新 product spec、tech spec 和运行时对象矩阵，并证明 `issue` 不能承载该语义。

本 change 只创建设计文档，不实现代码。后续实现只能从仍保留的对应能力包 `design.md` 和 `tasks.md` 进入。

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
+-------------------+   +----------------------+
| actual time entry |-->| calendar overlays    |
| My Time Calendar  |   | issue schedule UI    |
+-------------------+   +----------------------+
```

### 数据语义边界

```text
  issue
    | due/start/end
    v
  schedule visibility  -> issue overlays in calendar
  time_entry           -> actual work only
```

### 页面入口关系

```text
  Today Page        -> DeadlineBanner -> issue detail
  My Time Page      -> DeadlineBanner -> issue detail
  My Time Calendar  -> TimeEntry events + Issue overlays
```

## 能力列表

| 能力 | 当前状态 | 优先级 | 改动量 | 备注 |
| --- | --- | --- | --- | --- |
| Deadline visibility | 部分完成 | P0 | 小 | 已有 due/start/end 与 overdue 计算，缺统一 banner 和 planner deadline section |
| Calendar overlays | 部分完成 | P0 | 小到中 | 已有任务日历和工时日历，缺合并 overlay |
| Daily / weekly planner | 暂停 | - | - | 会引入平行执行对象，不能作为当前实现入口 |
| Timeboxing foundation | 暂停 | - | - | 计划块会承载执行状态，不能作为当前实现入口 |

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

### Paused structured planning and timeboxing

- 证据：`specs/issue-energy-loop/PRODUCT.md` `Product Hard Rules`。
  当前行为：`issue` 是当前版本唯一可执行对象，daily plan 只能作为 Markdown 草稿、AI 摘要或轻量记录存在。
  当前缺口：无可执行实现入口；旧 daily/weekly planner 和 timeboxing foundation 文档已删除，避免误导后续实现。
- 证据：`specs/issue-energy-loop/TECH.md` `Runtime Object Matrix`。
  当前行为：Focus、Time Entry、Daily Review 和 Daily Plan 都不能承载平行执行语义。
  当前缺口：若未来重新设计 timeboxing，必须先更新运行时对象矩阵。

## 外部功能基线

- Super Productivity 官方功能页把 schedule planner、time tracking、pomodoro、integrations 作为核心导航，并说明支持 Kanban、Eisenhower、compact lists、自定义布局、repeating tasks、time tracking、cross-platform sync。
- Super Productivity v17.5 release notes 提到 Task Deadlines：task list badge、detail panel、approaching reminders、today banner、planner overdue section、context menu deadline。

参考来源：

- https://super-productivity.com/
- https://github.com/super-productivity/super-productivity/releases

## 非目标

- 不实现 Super Productivity 的完整外部 Calendar / CalDAV 同步。
- 不实现 GitHub/Jira/GitLab/OpenProject issue provider 导入。
- 不实现 Focus Mode 全屏专注页；它应归入后续 focus-mode-flowtime spec。
- 不实现 Pomodoro 规则变化；本 change 不新增任何可启动 timer 的计划对象。
- 不引入个人本地离线数据库或跨端同步。
- 不把 daily plan Markdown 立即删除；迁移需要兼容现有 plan 数据。
- 不新增结构化计划项、计划块或任何能独立启动/完成执行的计划对象。

## 优先级与推进顺序

1. 先做 deadline visibility。原因：已有 issue 日期字段和 overdue 工具函数，改动最小，能快速建立 planner 的紧迫感入口。
2. 再做 calendar overlays。原因：已有两个日历页面，先让 issue schedule 和 actual time 同屏。
3. 结构化 planner 和 timeboxing foundation 暂停。原因：当前产品规则要求 issue 是唯一可执行对象。

## 共享约束

- 所有数据必须以 `workspace_id` 为租户边界。
- 所有计划数据必须绑定当前 user；团队可见性要作为后续明确决策，不默认公开个人计划。
- 第一版必须复用现有 `issue`、`time_entry`、`daily_plan`、calendar 组件，不新建平行任务系统或平行执行对象。
- `time_entry` 是 actual work 的主线，承载普通 timer、Pomodoro、Flowtime、Focus completion 等实际工作记录。
- `worklog` 是 legacy issue-bound duration model，不参与新的 planner、scheduler、timeboxing、Focus 或 Daily Review 主链路。
- 前端展示可以先本地派生；跨页面 summary 必须有服务端契约。
- 执行时如果发现需要改变数据模型，必须先更新对应能力包的 `design.md` 和 `tasks.md`。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 新增平行执行对象 | 会和 issue/focus/time entry 抢执行语义 | 暂停 structured planner 和 timeboxing foundation |
| 直接把 time entry 当 timebox | 实际工时与计划混淆，统计不可用 | 当前不做 timeboxing；time entry 只作为 actual |
| Calendar overlay 过早可编辑 | 用户可能误以为拖动 overlay 已排程 | 第一版 overlay 只读；任何可编辑排程都必须重新设计 |
| Deadline 口径与 due/start/end 混杂 | UI 提示不稳定 | deadline visibility 先定义统一优先级：overdue due/end > today due/end > active window > upcoming |

## 回写规则

- 每个能力实现后，更新对应 `spec.md` 的当前状态与缺口。
- 如果实现中新增或调整数据库表，更新对应 `research.md` 的数据模型链路。
- 如果 UI 交互与设计不同，先更新对应 `design.md` 的交互流程，再继续实现。
- 每个实现切片完成后，按 `tasks.md` 的验证方式记录实际验证命令和结果。

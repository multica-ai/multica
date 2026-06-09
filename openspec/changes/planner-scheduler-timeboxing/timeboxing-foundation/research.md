# Timeboxing Foundation Research

## 调研目标

确认现有 time tracking、calendar DnD、issue schedule 是否能支撑 planned time block，并识别需要新增的数据模型。

## 现状链路

1. 用户从 issue 或 My Time 页面启动 timer。
2. 前端调用 time entry API。
3. 后端创建 running time entry。
4. 用户停止后形成 actual record。
5. My Time Calendar 展示 actual record，并允许拖拽/resize 已完成 entry。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `server/pkg/db/queries/time_entry.sql` | `CreateTimeEntry` | 当前模型记录 actual time，不适合作为 planned block |
| `server/internal/handler/time_entry.go` | `CreateTimeEntry` / `SwitchTimeEntry` | API 语义是启动或创建工时 |
| `server/internal/service/time_entry.go` | `StartTimeEntry` | 服务层处理 running timer |
| `apps/workspace/src/features/time-tracking/hooks/use-time-entry-actions.ts` | `requestStart` | 前端启动 actual timer |
| `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` | `handleEventDrop` / `handleEventResize` | DnD 写回 actual entry |
| `apps/workspace/src/shared/types/time-entry.ts` | `TimeEntry` | TimeEntry 字段是 start/stop/duration，没有 planned state |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | Issue start/end 是 schedule window，不是日历时间块 |

## 数据模型或状态流

建议新增：

- `planned_time_block`
  - `workspace_id`
  - `user_id`
  - `issue_id`
  - `daily_plan_item_id`
  - `start_time`
  - `end_time`
  - `status`
  - `actual_time_entry_id`

状态：

- `planned`
- `active`
- `completed`
- `missed`
- `cancelled`

执行流：

1. 用户创建 planned block。
2. 用户点击 Start。
3. 系统创建 time entry，并把 block 标记 active。
4. 用户停止 timer。
5. block 关联 actual time entry 并标记 completed。

## 边界条件

- planned block 可以没有 issue，但第一版建议要求 issue 或 daily plan item 至少一个。
- block end 必须晚于 start。
- running block 同一用户同一时间只能一个。
- actual time entry 可以超过 planned end，不能自动截断。
- missed block 由前端或后台任务派生，第一版可只在 UI 派生，不落库。

## 未决问题

- 是否允许 overlapping planned blocks？
- 是否需要 buffer block？
- missed 状态是否持久化？
- actual time entry 是否允许关联多个 planned blocks？
- 是否需要 capacity 工作时间设置？

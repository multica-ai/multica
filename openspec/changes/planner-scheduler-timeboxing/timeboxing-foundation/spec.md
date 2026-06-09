# Timeboxing Foundation Spec

## 背景

Super Productivity timeboxing 的核心不是简单的工时记录，而是“先计划一个固定时间块，再执行并记录实际耗时”。Multica 当前已有可拖拽的 time entry calendar，但 time entry 表示实际工时。如果直接把 time entry 当 planned block，会导致 planned vs actual 无法区分。

## 范围

本能力覆盖：

- planned time block 数据模型。
- 从 issue 或 daily plan item 创建 timebox。
- 在 calendar 中拖拽/resize planned block。
- 从 planned block 启动实际 timer。
- planned vs actual 的最小对照。

## 当前状态

- Time entry 已支持实际工时创建、切换、拖拽/resize。
- My Time Calendar 已支持 DnD。
- Issue 有 start/end，但这些是任务 schedule window，不是具体 timebox。
- Daily plan 当前还没有结构化 items；建议依赖 `daily-weekly-planner`。

## 证据

- `server/pkg/db/queries/time_entry.sql` `CreateTimeEntry`：创建实际工时记录。
- `server/internal/service/time_entry.go` `StartTimeEntry` / `SwitchTimeEntry`：处理实际计时。
- `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` `handleEventDrop` / `handleEventResize`：可编辑 actual time entry。
- `apps/workspace/src/features/time-tracking/hooks/use-time-entry-actions.ts` `requestStart`：启动实际 timer。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：有 start/end/due schedule 字段，但没有 planned block。

## 缺口

- 没有 planned block 表。
- 没有 planned block 与 issue / daily plan item 的关联。
- 没有 planned block 到 actual time entry 的执行链路。
- 没有 planned vs actual 对照。
- 没有 overload warnings。

## 交接说明

执行 Agent 必须先确认 `daily-weekly-planner` 是否已提供 plan item。如果没有，可以先支持 issue-only planned blocks，但不能把 time entry 复用为 planned block。

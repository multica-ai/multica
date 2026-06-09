# Calendar Overlays Research

## 调研目标

确认当前两个 calendar 页面和 calendar wrapper 是否能承载 planned issue overlay，并识别不会破坏 time entry DnD 的实现边界。

## 现状链路

1. `IssueCalendarPage` 拉取 issue list。
2. `issueToEvent` 将 `start_date` / `end_date` 转换为 all-day event。
3. `MyTimeCalendarPage` 拉取 time entries。
4. `splitAtMidnight` 将跨天 time entry 拆成 calendar segments。
5. `BigDnDCalendar` 渲染 time entry，并允许非 running entry 拖拽/resize。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/issues/components/IssueCalendarPage.tsx` | `IssueCalendarPage` | 已有任务日历页面 |
| `apps/workspace/src/features/issues/components/IssueCalendarPage.tsx` | `issueToEvent` | 当前仅使用 start/end，不使用 due_date |
| `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` | `MyTimeCalendarPage` | 已有个人工时日历 |
| `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` | `handleEventDrop` / `handleEventResize` | 已有 DnD 写回 actual time entry |
| `apps/workspace/src/features/time-tracking/components/calendar/CalendarEventCard.tsx` | `CalendarEventCard` | actual time event 有专用视觉组件 |
| `apps/workspace/src/components/ui/big-calendar.tsx` | `BigDnDCalendar` | 已支持 DnD calendar |
| `apps/workspace/src/features/time-tracking/utils/calendar-events-builder.ts` | `splitAtMidnight` | actual time entry 已处理跨天展示 |

## 数据模型或状态流

第一版不新增数据库。

前端事件分层：

```ts
type PlannerCalendarEvent =
  | { kind: "time-entry"; resource: TimeEntry; editable: true }
  | { kind: "issue-window"; resource: Issue; editable: false }
  | { kind: "issue-due"; resource: Issue; editable: false };
```

显示规则：

- `time-entry`：保持现有视觉和交互。
- `issue-window`：由 start/end 生成，只读，低对比背景。
- `issue-due`：由 due_date 生成，只读，deadline marker。

## 边界条件

- Running time entry 继续不可拖拽。
- Issue overlay 第一版不可拖拽、不可 resize。
- 如果 issue 同时有 start/end 和 due_date，应同时显示 window 和 due marker，除非 due 与 end 同日且 UI 过密。
- Calendar event 点击要根据 `kind` 路由到 issue detail 或打开 time entry edit。

## 未决问题

- Overlay 默认是否开启，还是需要 toggle？
- My Time Calendar 是否只展示 assigned-to-me issue，还是 workspace 全部 issue？
- Due marker 在 day/week view 里显示为 timed marker 还是 all-day marker？
- Overlay 是否要区分 project color？

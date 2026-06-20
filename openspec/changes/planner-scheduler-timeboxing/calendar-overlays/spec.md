# Calendar Overlays Spec

## 背景

Multica 当前已有任务日历和工时日历，但二者分离：任务日历只看 scheduled issue，工时日历只看 actual time entries。Calendar overlays 的目标是在 My Time Calendar 中只读展示 issue schedule / deadline，让用户同屏看到 issue 时间窗口和实际投入。

## 范围

第一版覆盖：

- 在 My Time Calendar 中叠加只读 issue schedule events。
- Overlay 包含 issue start/end window 和 due marker。
- Overlay 可点击跳转 issue detail。
- 保持 time entry 拖拽/resize 行为不变。

## 当前状态

- `/calendar` 展示带 start/end 的 issue。
- `/my-time/calendar` 展示 time entry，并支持拖拽/resize。
- `BigCalendar` / `BigDnDCalendar` 已封装 react-big-calendar。

## 证据

- `apps/workspace/src/features/issues/components/IssueCalendarPage.tsx` `issueToEvent`：把 issue start/end 转成全天日历事件。
- `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` `events`：把 time entry 转成 calendar event。
- `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` `handleEventDrop` / `handleEventResize`：拖拽/resize 只写 time entry。
- `apps/workspace/src/components/ui/big-calendar.tsx` `BigDnDCalendar`：已有 DnD calendar wrapper。

## 缺口

- My Time Calendar 看不到 issue schedule。
- Issue Calendar 看不到 actual time。
- due_date 没有作为 calendar marker 出现。
- overlay event 和 editable event 没有类型分层。

## 交接说明

执行 Agent 应优先在 My Time Calendar 中实现只读 overlay，不要让 issue overlay 支持拖拽、启动、完成或承载任何执行状态。

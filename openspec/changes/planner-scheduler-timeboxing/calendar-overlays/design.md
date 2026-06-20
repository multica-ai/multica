# Calendar Overlays Design

## 目标

在个人工时日历中叠加任务 schedule / deadline overlay，让用户能同屏比较计划安排和实际投入，且不改变现有 time entry 编辑能力。

## 非目标

- 不实现拖拽 issue 排程。
- 不实现外部 calendar sync。
- 不新增任何计划块模型。
- 不改变 time entry 的创建、更新、删除接口。
- 不合并 `/calendar` 和 `/my-time/calendar` 路由。

## 当前架构基线

- My Time Calendar 已用 `BigDnDCalendar` 展示 actual time。
- Issue Calendar 已能展示 issue start/end。
- Issue list query 可按 workspace 获取 issue。
- Calendar wrapper 支持 event components 和 event prop getter。

### ASCII 图

```text
              +----------------+
              | time_entry API |
              +-------+--------+
                      |
                      v
              time-entry events
                      |
                      v
+-------------+  +-------------------+  +------------------+
| issue query |->| issue overlay     |->| BigDnDCalendar   |
| date window |  | issue-window/due  |  | My Time Calendar |
+-------------+  +-------------------+  +------------------+
                                               |
                                               +--> time-entry: editable
                                               +--> issue overlay: readonly
```

Event type split:

```text
PlannerCalendarEvent
  |
  +-- kind=time-entry   -> CalendarEventCard       -> drag/resize/edit
  +-- kind=issue-window -> IssueOverlayEventCard   -> click issue only
  +-- kind=issue-due    -> IssueOverlayEventCard   -> click issue only
```

## 缺口定义

当前用户需要在两个页面之间切换才能看到 issue 的 schedule/deadline 和自己的实际投入，无法在个人执行视图中判断当天时间分布。

## 方案与权衡

### 方案 A：在 Issue Calendar 中叠加 time entries

优点：保留 issue calendar 作为计划入口。缺点：Issue Calendar 当前是 all-day schedule 视角，不适合展示细粒度 actual time。

### 方案 B：在 My Time Calendar 中叠加 issue overlays

优点：My Time Calendar 已有 day/week 时间栅格和 DnD，适合同屏展示 issue schedule 和 actual time。缺点：需要在个人执行页加载 issue list。

### 方案 C：新建 Planner Calendar

优点：可专门设计。缺点：改动中等偏大，且会重复现有 calendar 能力。

## 推荐方案

采用方案 B。

第一版在 `MyTimeCalendarPage` 中新增 issue overlay query 和事件转换：

- `buildIssueOverlayEvents(issues, visibleWindow)`。
- `PlannerCalendarEvent` discriminated union。
- 根据 event kind 渲染不同组件。
- issue overlay 点击跳 issue detail。

## 数据模型或状态模型

第一版仅前端状态：

```ts
interface IssueOverlayEvent {
  id: string;
  kind: "issue-window" | "issue-due";
  title: string;
  start: Date;
  end: Date;
  allDay: boolean;
  resource: Issue;
}
```

`issue-window`：

- start = `issue.start_date ?? issue.end_date`
- end = `issue.end_date + 1 day` for all-day month view, or raw end for timed day/week if time component exists

`issue-due`：

- start = `issue.due_date`
- end = same day marker window

## 接口契约

第一版复用：

- `GET /api/issues?workspace_id=&limit=&start_from=&start_to=&end_from=&end_to=&due_from=&due_to=`

如当前 API client 参数不足，先扩展 `ListIssuesParams` 类型和调用，不新增服务端 handler。

## UI 或交互流程

- My Time Calendar 顶部增加 overlay toggle：
  - `Tasks` on/off
  - `Deadlines` on/off
- 默认开启 `Tasks` 和 `Deadlines`。
- time entry events 保持现有卡片。
- issue-window 用低对比色块或边框展示。
- issue-due 用小型 deadline marker。
- 点击 issue overlay 跳转 issue detail。
- time entry context menu 不对 issue overlay 出现。

## 权限、边界条件、异常路径

- 只展示当前 workspace 可见 issue。
- 第一版默认展示非 terminal issue。
- 如果 issue 日期不合法，跳过。
- 如果 issue overlay 与 time entry 重叠，time entry 视觉优先，overlay 低层级。
- issue overlay 不可拖拽；`draggableAccessor` 必须只允许 `kind === "time-entry"`。

## 实现约束

- 不要把 issue overlay 传给现有 `CalendarEventCard`。
- 事件 union 必须显式区分 `kind`。
- DnD handler 必须 guard event kind。
- Overlay builder 必须单元测试覆盖 start/end/due 组合。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| overlay 与 actual event 视觉冲突 | overlay 使用低层级、低对比、可关闭 |
| 用户误以为可拖拽排程 | cursor 和交互保持只读，DnD handler 明确拒绝 issue overlay |
| issue query 过大 | 使用 visible window 日期过滤 |
| 月视图 all-day 与 day/week timed 口径不一致 | 第一版优先 day/week 体验，month 只做概览 |

## 验收检查

- My Time Calendar 能显示 time entries 和 issue overlays。
- 关闭 Tasks toggle 后，issue-window 消失。
- 关闭 Deadlines toggle 后，issue-due 消失。
- 拖拽 time entry 仍能更新。
- issue overlay 不可拖拽，点击能跳 issue detail。

# Timeboxing Foundation Design

## 目标

建立 Multica 的 planned time block 基线，让用户可以把 issue 或 daily plan item 安排到具体时间段，在日历中调整，并从 timebox 启动实际计时，形成 planned vs actual 的最小闭环。

## 非目标

- 不实现完整外部 calendar sync。
- 不实现 recurrence timeboxes。
- 不实现团队排班。
- 不实现自动重新排程。
- 不替换现有 time entry 模型。

## 当前架构基线

- Time entry 表示 actual work。
- My Time Calendar 已支持 actual event 拖拽/resize。
- Issue 有 date window。
- Daily plan 可在后续提供 plan items。
- Pomodoro timer 和 normal timer 已存在。

### ASCII 图

Do not do this:

```text
planned work ----X----> time_entry

Reason:
time_entry means actual work. Reusing it for planned work corrupts
planned vs actual metrics and billing/reporting semantics.
```

Recommended model:

```text
issue
  |
  +--------------------------+
                             |
daily_plan_item              |
  |                          |
  +----------+---------------+
             |
             v
     planned_time_block
       | start_time
       | end_time
       | status=planned/active/completed/cancelled
       |
       +-- start ----------------+
                                  v
                              time_entry
                                  |
                                  v
                         actual duration
                                  |
                                  v
             planned vs actual comparison
```

Calendar event write paths:

```text
My Time Calendar
  |
  +-- actual time_entry event
  |      drag/resize -> PATCH /api/time-entries/:id
  |
  +-- planned_time_block event
         drag/resize -> PATCH /api/planned-time-blocks/:id
         start       -> POST  /api/planned-time-blocks/:id/start
```

State flow:

```text
planned
   |
   | user starts block
   v
active
   |
   | timer stopped / block completed
   v
completed

planned -- user cancels --> cancelled

UI-derived only:
planned + end_time < now -> missed
active  + now > end_time -> overrun
```

## 缺口定义

当前只能记录“已经做了什么”，不能表达“计划什么时候做”。Issue start/end 只能表达粗粒度 schedule window，不能表达 09:00-10:30 这种具体 timebox。

## 方案与权衡

### 方案 A：复用 time_entry，给它增加 planned 状态

优点：表少。缺点：actual 和 planned 混在一起，统计、计费、日历编辑都会变复杂。

### 方案 B：新增 `planned_time_block`

优点：语义清晰，planned vs actual 可对照。缺点：需要新表、新接口、新 UI。

### 方案 C：只在前端 localStorage 保存 timebox

优点：快。缺点：不能跨端，不能与 timer/plan 可靠联动。

## 推荐方案

采用方案 B。

新增 planned time block 作为独立模型。time entry 继续只代表实际工作。planned block 可以关联 issue 和 daily plan item，并可在 Start 时创建 actual time entry。

## 数据模型或状态模型

```sql
CREATE TABLE planned_time_block (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  user_id UUID NOT NULL,
  issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
  daily_plan_item_id UUID REFERENCES daily_plan_item(id) ON DELETE SET NULL,
  title_snapshot TEXT NOT NULL,
  start_time TIMESTAMPTZ NOT NULL,
  end_time TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'planned',
  actual_time_entry_id UUID REFERENCES time_entry(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (end_time > start_time)
);
```

`status`:

- `planned`
- `active`
- `completed`
- `cancelled`

UI 派生：

- `missed`: status planned 且 end_time < now。
- `overrun`: active 且 now > end_time。

## 接口契约

- `GET /api/planned-time-blocks?since=&until=`
- `POST /api/planned-time-blocks`
- `PATCH /api/planned-time-blocks/{id}`
- `DELETE /api/planned-time-blocks/{id}`
- `POST /api/planned-time-blocks/{id}/start`
- `POST /api/planned-time-blocks/{id}/complete`

`POST /start`：

- 如果已有 running time entry，沿用现有 switch confirmation 机制。
- 创建或切换到 actual time entry。
- 返回 `{ block, time_entry }`。

## UI 或交互流程

### 创建

- 从 Daily Planner item 菜单选择 `Schedule timebox`。
- 从 issue detail 选择 `Schedule timebox`。
- 在 My Time Calendar 空白区域创建 block。

### 日历

- Planned block 显示为可拖拽/resize 事件。
- Actual time entry 保持现有视觉。
- Planned block 点击打开 edit sheet。
- Active block 显示 Start/Stop 或 running 状态。

### 执行

- 用户点击 planned block 的 Start。
- 系统启动 timer，并把 block 标记 active。
- 用户停止 timer 后，block 标记 completed，关联 actual time entry。
- UI 显示 planned duration vs actual duration。

### Overload warnings

第一版只做当天 planned total：

- planned total > 8h 或超过用户设置工作时间时显示 warning。
- 如果还没有用户工作时间设置，默认 8h。

## 权限、边界条件、异常路径

- planned block 属于当前 user。
- workspace member 只能读写自己的 block。
- start/end 必须合法且 end > start。
- issue 被删除后保留 title snapshot。
- daily plan item 被删除后保留 block。
- 同时只能有一个 active block。
- Overlap 第一版允许，但 UI 显示 warning；不硬阻止。

## 实现约束

- 不把 planned block 写入 time_entry。
- DnD planned block 写 `planned_time_block`，DnD actual entry 写 `time_entry`。
- Start block 必须复用现有 time entry start/switch 语义，不能绕过 running timer 保护。
- 所有 block query 必须按 workspace_id 和 user_id 过滤。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| actual/planned 混淆 | 独立表、独立 event kind、独立 card |
| block start 与 running timer 冲突 | 复用 existing switch confirmation |
| 用户日历过载 | overlay toggle 和 overload warning |
| daily plan item 未实现 | 第一版支持 issue-only blocks，plan item 关联 nullable |

## 验收检查

- 用户可以从 issue 创建 planned block。
- 用户可以在 My Time Calendar 拖拽/resize planned block。
- 用户可以从 planned block 启动 timer。
- 停止 timer 后 planned block 显示 actual duration。
- actual time entry 行为不受影响。
- 同一天 planned total 超过默认容量时显示 warning。

# 单能力 Design

## 目标

把 break 从隐式 phase transition 升级为可恢复、可跳过、可分析的 Focus 子流程，同时保持工时统计干净：break 写 `focus_events`，不写 `time_entry`。

## 非目标

- 不把 break 算作工作时间。
- 不在 My Time 列表展示 break 为 time entry。
- 不做休息质量评价。
- 不接入桌面通知。

## 当前架构基线

- 当前入口：Pomodoro work complete 后自动进入 break phase。
- 当前核心逻辑：`server/internal/handler/pomodoro.go` `CompletePomodoro`
- 当前存储或状态：`pomodoro_sessions.phase`
- 当前 UI 或接口：`PomodoroTimer` 倒计时和 toast

### 代码证据

- `server/internal/handler/pomodoro.go` `CompletePomodoro`：说明 break 只是 next phase。
- `server/pkg/db/queries/pomodoro.sql` `GetPomodoroHistory`：说明 break 不进入 history。
- `server/pkg/db/queries/time_entry.sql` `SumTimeEntriesByUserInWorkspace`：说明 `time_entry` 会进入统计。

## 缺口定义

- 休息建议没有事件。
- 用户是否开始、跳过、完成休息没有记录。
- 页面刷新后 break flow 需要能恢复。
- break 统计不能污染 `time_entry`。

## 方案与权衡

### 方案 A：break 写入 `time_entry(type='break')`

- 做法：休息开始和完成写 `time_entry`。
- 优点：复用现有时间线。
- 风险：污染 My Time、team time、project stats，需要大量排除逻辑。

### 方案 B：break 写入 `focus_events`

- 做法：把 break 作为 Focus 事件记录，不进入工时表。
- 优点：统计口径清晰，后续可分析休息行为。
- 风险：需要新增事件表和聚合查询。

## 推荐方案

选择方案 B。

原因：
- break 是恢复行为，不是工作产出或工时。
- `time_entry` 已被团队和项目统计使用，不能混入休息。
- `focus_events` 可同时支持 break、pause、abandon、resistance reason 等行为分析。

## 数据模型或状态模型

新增 `focus_events`：

```text
id uuid pk
workspace_id uuid not null
user_id uuid not null
focus_session_id uuid null
event_type text not null
reason text null
note text null
duration_seconds int null
metadata jsonb not null default '{}'
created_at timestamptz not null
```

Break event types：

- `break_suggested`
- `break_started`
- `break_skipped`
- `break_completed`

`focus_sessions` break 状态：

- `phase = break_suggested`
- `phase = breaking`
- `suggested_break_seconds = N`
- `started_at` 在 `breaking` 时表示 break start time

## 接口契约

### 输入

- `POST /api/focus/break/start`
- `POST /api/focus/break/skip`
- `POST /api/focus/break/complete`

`skip` body：

```json
{
  "reason": "optional enum",
  "note": "optional text"
}
```

`reason` 首轮允许：

- `urgent_work`
- `not_needed`
- `interrupted`
- `other`

### 输出

```json
{
  "session": {
    "phase": "breaking",
    "suggested_break_seconds": 600,
    "started_at": "iso"
  },
  "event": {
    "event_type": "break_started"
  }
}
```

错误：

- `400 invalid_focus_phase`：当前不是 `break_suggested` 或 `breaking`。
- `404 focus_session_not_found`：没有当前 session。

## UI 或交互流程

1. Flowtime 或 Pomodoro focus 完成后，页面进入 `break_suggested`。
2. 用户看到建议休息时长和两个主操作：Start break、Skip.
3. 点击 Start break 后进入 break countdown。
4. 用户可以提前 Complete break。
5. 倒计时到 0 后自动 complete break。
6. 点击 Skip 时可选填写原因。

## 权限、边界条件、异常路径

- 只有当前 user 可操作自己的 break。
- skip reason 可空，不能阻塞用户。
- breaking 状态刷新页面后，根据 `started_at` 和 `suggested_break_seconds` 计算剩余时间。
- complete break 需要计算实际 break duration 并写入 event。

## 实现约束

- Break 不写 `time_entry`。
- Break 不显示在 My Time entry list。
- Break events 必须按 `workspace_id` 过滤。
- Break event 写入和 session phase 更新需要同一 transaction。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| break 事件和 session 不一致 | UI 无法恢复 | 事务内同时写 event 和 session |
| 用户跳过原因采集过重 | 用户反感 | 原因可选，skip 主动作不阻塞 |
| break 不在 My Time 可见 | 用户以为没记录 | Focus 页面后续展示 break behavior summary |

## 验收检查

1. 完成 Flowtime 后进入 `break_suggested`。
2. Start break 写入 `break_started`，session 进入 `breaking`。
3. Skip break 写入 `break_skipped`，不创建 `time_entry`。
4. Complete break 写入 `break_completed` 和实际休息时长。
5. 刷新页面后 breaking 状态可恢复剩余时间。

# 单能力 Design

## 目标

支持开放式 Flowtime 专注：用户开始后不受固定倒计时限制，完成时按实际专注时长写入历史，并获得首版动态休息建议。

## 非目标

- 不实现用户自定义算法。
- 不把 Flowtime 写入 `worklog`。
- 不做自动分心检测。
- 不在本能力中实现 break UI 的完整事件流程。

## 当前架构基线

- 当前入口：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`
- 当前核心逻辑：`server/internal/handler/pomodoro.go` `CompletePomodoro`
- 当前存储或状态：`time_entry`、`pomodoro_sessions`
- 当前 UI 或接口：`/api/pomodoro/complete`、My Time entry list

### 代码证据

- `server/internal/handler/pomodoro.go` `CompletePomodoro`：当前 Pomodoro completion 使用固定 duration。
- `server/internal/handler/time_entry.go` `StopTimeEntry`：普通 timer 可记录实际 duration。
- `server/pkg/db/queries/time_entry.sql` `CreateTimeEntry`：`type` 字段可承载来源。
- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `EntryRow`：前端已有按 `type` 显示标记的模式。

## 缺口定义

- Flowtime 没有开放式 session mode。
- 当前完成逻辑不能按实际专注时长写记录。
- 没有动态休息建议。
- 历史列表无法识别 Flowtime。

## 方案与权衡

### 方案 A：复用普通 live timer 作为 Flowtime

- 做法：Flowtime start 直接创建 ordinary running `time_entry`。
- 优点：后端改动少，实际时长天然准确。
- 风险：当前运行态缺少 commitment、reason、break suggestion 和 Focus phase；难以表达 break flow。

### 方案 B：使用 `focus_sessions` 管理运行态，完成时写 `time_entry(type='flowtime')`

- 做法：Flowtime 运行过程写 `focus_sessions`，完成时生成 stopped `time_entry`。
- 优点：运行态语义清晰，历史仍复用统一时间模型。
- 风险：需要实现实际 elapsed 计算和 completion 原子性。

## 推荐方案

选择方案 B。

原因：
- Flowtime 不只是 ordinary timer，它需要 commitment、break suggestion 和反拖延上下文。
- `time_entry` 仍然是历史记录来源，不会重复建历史表。
- Break flow 可以基于 `focus_sessions.phase = break_suggested` 继续推进。

## 数据模型或状态模型

沿用 `focus_sessions`：

- `mode = "flowtime"`
- `phase = "focusing" | "paused" | "break_suggested" | "completed" | "abandoned"`
- `elapsed_focus_seconds` 累计已经完成的 focus 段
- `started_at` 表示当前正在运行的 focus 段开始时间
- `suggested_break_seconds` 在 complete 后写入

`time_entry`：

- `type = "flowtime"`
- `duration_seconds = actual_focus_seconds`
- `start_time = focus session 首次 start 时间`
- `stop_time = complete 时间`
- `description = session.description || session.commitment_text`
- `issue_id = session.issue_id`

首版休息建议算法：

| 实际专注时长 | 建议休息 |
| --- | --- |
| `< 25m` | `5m` |
| `25m - 50m` | `10m` |
| `> 50m` | `15m` |

## 接口契约

### 输入

`POST /api/focus/complete` body：

```json
{
  "note": "optional override",
  "end_reason": "completed"
}
```

`end_reason` 首轮允许：

- `completed`
- `stopped_early`

### 输出

```json
{
  "session": {
    "mode": "flowtime",
    "phase": "break_suggested",
    "elapsed_focus_seconds": 2130,
    "suggested_break_seconds": 600
  },
  "time_entry": {
    "id": "uuid",
    "type": "flowtime",
    "duration_seconds": 2130
  },
  "next_action": "start_break"
}
```

错误：

- `400 invalid_focus_phase`：非 focusing/paused 不可 complete。
- `404 focus_session_not_found`：没有当前 session。

## UI 或交互流程

1. 用户在 `/focus` 选择 Flowtime。
2. 用户填写可选 issue、note、labels、commitment。
3. 用户点击 Start，页面显示正向计时。
4. 用户可 pause/resume。
5. 用户点击 Complete。
6. 系统保存 `time_entry(type='flowtime')`，展示建议休息。
7. 用户可进入 break flow 或跳过休息。

## 权限、边界条件、异常路径

- 只有 session owner 可以完成自己的 Flowtime。
- 完成失败时保留 session，不清空上下文。
- 如果 label 绑定失败，整个 complete 应失败并保留 session，避免时间记录和标签状态不一致。
- 小于 60 秒允许完成，但 UI 提示“短专注已记录”。

## 实现约束

- 不使用 `phase_duration_seconds` 作为 Flowtime duration。
- 不把 Flowtime 作为 ordinary running timer 直接暴露给 My Time running state。
- 不写 `worklog`。
- My Time 列表需要能识别 `type = "flowtime"`。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| actual duration 计算错误 | 历史记录失真 | 后端集中计算，测试覆盖 running 和 paused completion |
| completion 部分成功 | session 和 time_entry 不一致 | 后端使用 transaction |
| Flowtime 与普通 timer 统计重复 | 用户时间膨胀 | Focus start 先处理普通 timer 冲突 |

## 验收检查

1. Flowtime 可以开放式正向计时。
2. pause/resume 后完成，duration 包含所有 focus 段且不包含暂停时间。
3. 完成后创建 `time_entry(type='flowtime')`。
4. 完成后返回建议休息时长。
5. My Time 中能识别 Flowtime entry。
6. 完成失败不会丢失当前 session。

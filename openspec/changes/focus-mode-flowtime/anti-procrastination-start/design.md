# 单能力 Design

## 目标

通过 2 分钟启动、下一步承诺和轻量原因记录，降低用户进入 Focus 的阻力，并为后续分析保留结构化数据。

## 非目标

- 不做 AI 自动原因识别。
- 不做长表单。
- 不强制用户填写原因。
- 不做完整个人行为分析页面。

## 当前架构基线

- 当前入口：`MyTimePage` 普通 timer start bar 和 `PomodoroTimer` start。
- 当前核心逻辑：`StartPomodoro`、`PausePomodoro`、`ResetPomodoro`
- 当前存储或状态：`pomodoro_sessions` 和 `running_timer`
- 当前 UI 或接口：无原因采集接口。

### 代码证据

- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `StartTimerBar`：说明当前只有 description 输入。
- `server/internal/handler/pomodoro.go` `StartPomodoro`：说明 start 不接收原因。
- `server/internal/handler/pomodoro.go` `PausePomodoro`：说明 pause 不记录原因。
- `server/internal/handler/pomodoro.go` `ResetPomodoro`：说明 reset 不记录 abandon reason。

## 缺口定义

- 启动前没有下一步动作承诺。
- 需要 2 分钟启动时，没有单独模式。
- 启动阻力、暂停原因、放弃原因没有结构化记录。
- 原因采集如果设计过重，会损害反拖延目标。

## 方案与权衡

### 方案 A：只做 2 分钟按钮，不记录原因

- 做法：新增 Two-minute start preset，不采集原因。
- 优点：最轻。
- 风险：后续无法分析用户为什么卡住。

### 方案 B：2 分钟启动 + 可选原因采集

- 做法：启动时展示下一步承诺和可选阻力原因，暂停/放弃时可选原因。
- 优点：仍保持轻量，同时为后续分析留数据。
- 风险：UI 需要控制密度，不能像问卷。

## 推荐方案

选择方案 B。

原因：
- 用户明确需要原因填写方便后续分析。
- 原因可选，不阻塞 start。
- `focus_events` 可以统一承载启动、暂停、放弃原因。

## 数据模型或状态模型

沿用 `focus_sessions` 字段：

- `mode = "quick_start"`
- `preset = "two_minute_start"`
- `commitment_text`
- `status_reason`
- `reason_note`

写入 `focus_events`：

Event types：

- `focus_started`
- `focus_paused`
- `focus_resumed`
- `focus_abandoned`
- `quick_start_completed`

Reason enums：

`resistance_reason` / `pause_reason` / `abandon_reason` 共用首版枚举：

- `unclear_next_step`
- `too_large`
- `low_energy`
- `avoidance`
- `interruption`
- `blocked`
- `other`

## 接口契约

### 输入

`POST /api/focus/start` 扩展：

```json
{
  "mode": "quick_start",
  "preset": "two_minute_start",
  "commitment_text": "Open the CI log and find the failing test",
  "resistance_reason": "unclear_next_step",
  "resistance_note": "optional text"
}
```

`POST /api/focus/pause` body：

```json
{
  "reason": "interruption",
  "note": "optional text"
}
```

`POST /api/focus/abandon` body：

```json
{
  "reason": "too_large",
  "note": "optional text"
}
```

### 输出

Start 返回当前 session 和 start event：

```json
{
  "session": {
    "mode": "quick_start",
    "phase": "focusing",
    "preset": "two_minute_start",
    "commitment_text": "Open the CI log and find the failing test"
  },
  "event": {
    "event_type": "focus_started",
    "reason": "unclear_next_step"
  }
}
```

## UI 或交互流程

1. 用户进入 `/focus`。
2. 用户选择 `2 min start`。
3. UI 展示一个紧凑输入：
   - 下一步动作
   - 可选阻力原因 segmented/menu
   - 可选备注
4. 用户点击 Start，立即进入 2 分钟倒计时。
5. 2 分钟完成后：
   - 主 CTA：Continue Flowtime
   - 次 CTA：Complete and take break
6. 用户 pause 或 abandon 时，展示可选原因选择，不阻塞主操作。

## 权限、边界条件、异常路径

- 原因字段可空。
- `commitment_text` 可空，但 UI 应鼓励填写。
- `other` 不强制 note。
- 放弃时不写 `time_entry`，只写 `focus_abandoned`。
- 2 分钟启动完成后，如果用户选择 Continue Flowtime，不应产生两条 time entry；继续同一 session。

## 实现约束

- 不让原因填写成为 modal 强阻塞。
- 不在 My Time 普通 timer 中塞入完整原因表单。
- 不将 abandoned quick start 写入 `time_entry`。
- 原因枚举前后端必须一致。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 原因采集增加启动摩擦 | 违背反拖延目标 | 默认可跳过，输入紧凑，Start 始终可用 |
| 2 分钟完成后记录碎片化 | 产生很多无意义 time entry | Continue Flowtime 继续同一 session，不立即写 entry |
| 枚举太粗 | 分析价值有限 | 首轮保留 `other` 和 note，后续根据数据调整 |

## 验收检查

1. 用户可以选择 2 分钟启动。
2. 用户可以填写下一步承诺。
3. 用户可以选择或跳过启动阻力原因。
4. pause/abandon 可以填写原因且不阻塞操作。
5. abandoned quick start 不写 `time_entry`。
6. Continue Flowtime 不产生重复 time entry。

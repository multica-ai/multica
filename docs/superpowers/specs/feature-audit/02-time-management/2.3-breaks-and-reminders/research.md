# 2.3 休息与提醒调研

## 1. 调研目标

- 确认仓库内是否已有时间提醒实现。
- 梳理工时计时、番茄钟、通知偏好三者的现状边界。

## 2. 现状链路

- 搜索关键词：`reminder`、`remind`、`desktop notification`、`Notification(`、`idle`、`auto-pause`
- 搜索结果：未找到时间管理域的休息提醒、截止提醒或桌面提醒实现匹配。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` · `PomodoroTimer` | 番茄钟已有 `playSound`、阶段切换、自动开始配置，可作为提醒触发源。 |
| `apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` · `useSoundSystem` | 本地音效能力已存在，可复用为提醒 delivery。 |
| `apps/workspace/src/features/settings/components/notifications-tab.tsx` · `TYPE_GROUPS` | 通知偏好系统当前只覆盖 issue/comment/project/inbox 等类别，没有 time reminder。 |
| `server/migrations/034_notification_preference.up.sql` · `notification_preferences` | 仓库有 ntfy 通道偏好，但没有时间提醒专属类型。 |
| 搜索关键词 `reminder` / `remind` / `desktop notification` / `idle` / `auto-pause` | 未找到匹配，说明 2.3 需要从零设计触发与投递。 |

## 4. 数据模型或状态流

- 计时触发源：`useTimeTracking` 的 running timer。
- 番茄触发源：`PomodoroTimer` 的 phase 与 session。
- 通知投递能力：本地音效、浏览器 Notification API、现有通知偏好系统（仅作未来扩展位）。

## 5. 边界条件

- 浏览器桌面通知需要用户授权。
- 如果每个页面各自维护 reminder timer，会与 `GlobalTimerWidget` 和 `PomodoroTimer` 冲突。

## 6. 未决问题

1. 截止提醒是只提醒今天/明天，还是支持任意偏移量。
2. 是否在本阶段接入 ntfy 远端推送。

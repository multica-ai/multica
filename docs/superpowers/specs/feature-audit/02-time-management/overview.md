# 时间管理模块总览

## 1. 目标与范围

- 模块范围覆盖 `docs/superpowers/specs/feature-audit/02-time-management/`。
- 本轮补齐 2.1 工时追踪、2.3 休息与提醒的 `research.md`、`design.md`、`tasks.md`，并把 2.2 作为既有基线引用。

## 2. 能力列表

| 能力 | 当前状态 | 本轮动作 | 依赖 |
| --- | --- | --- | --- |
| 2.1 工时追踪 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 无 |
| 2.2 番茄钟 | 已有设计与实现基线 | 仅在 overview 中引用 | 2.1 的当前计时入口 |
| 2.3 休息与提醒 | 缺失 | 新增 `research.md` / `design.md` / `tasks.md` | 2.1、2.2、通知偏好 |

## 3. 当前状态证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` · `MyTimePage` | 时间管理域已包含当前计时、时间记录列表、番茄钟卡片。 |
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` · `useTimeTracking` | 当前工时主链路已经支持 start / stop / switch / current。 |
| `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` · `GlobalTimerWidget` | 当前计时状态已提升到全局浮层，说明提醒与自动暂停必须复用同一 running timer 源。 |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` · `PomodoroTimer` | 番茄钟已有音效、状态切换、自动开始配置，是 2.3 提醒系统的现成触发源。 |
| `server/migrations/036_time_entry.up.sql` · `time_entries` | 工时记录当前是“开始时间 + 结束时间”的区间模型，没有“暂停态”字段。 |
| `server/migrations/041_pomodoro_session.up.sql` · `pomodoro_sessions` | 番茄钟已有独立 session 表，说明 2.3 不能把提醒状态塞进 time entry。 |

## 4. 非目标

- 本轮不重写 2.2 番茄钟设计包。
- 本轮不实现跨设备 push 通知中心；若需复用通知系统，只定义接口与低优先级扩展位。

## 5. 优先级与推进顺序

1. **先锁定 2.1 的计时生命周期**：`useTimeTracking` 与 `GlobalTimerWidget` 当前共同依赖 running timer；2.3 的提醒触发必须建立在同一生命周期定义上。
2. **再做 2.3 的提醒总线**：break / reminder 触发源同时来自 running timer 与 pomodoro session，必须先有共享生命周期。
3. **最后补通知扩展位**：`apps/workspace/src/features/settings/components/notifications-tab.tsx` 现有类型组不含时间提醒，因此跨系统推送只能列为低优先级。

## 6. 共享约束

### 6.1 2.1 与 2.3 共用单一计时事实源

- `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` · `currentTimeEntryQuery` 与 `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` · `GlobalTimerWidget` 都依赖当前 running entry。
- 结论：自动暂停、休息提醒、到时音效都必须挂在同一 running timer 生命周期上，不能各自维护独立“计时中”状态。

### 6.2 提醒触发属于时间管理域，交付可复用通知能力

- `apps/workspace/src/features/settings/components/notifications-tab.tsx` · `TYPE_GROUPS` 与 `server/migrations/034_notification_preference.up.sql` · `notification_preferences` 说明仓库已有通用通知偏好与 ntfy 通道，但没有时间提醒类型。
- 结论：2.3 本轮应自管“何时触发提醒”，交付层优先复用浏览器通知、音效与 toast；远端推送列为低优先级扩展。

## 7. 风险与依赖

| 主题 | 风险 | 依赖 / 对策 |
| --- | --- | --- |
| 自动暂停 | `time_entries` 没有暂停态，若强行插入会改动历史统计。 | 推荐用“自动停止 + 可恢复草稿”语义。 |
| 提醒 | 计时提醒、番茄钟提醒、截止提醒若各自建计时器会互相抢占。 | 统一提醒注册表，所有触发源共用一个 scheduler。 |
| 通知 | 通知偏好系统当前没有 time reminder 类型。 | 本轮只定义适配接口，把 ntfy push 标为低优先级。 |

## 8. 回写规则

1. 2.1 或 2.3 若改变计时生命周期，必须先回写本 `overview.md`。
2. 提醒类型若扩展到通用通知中心，需要同时更新 `notifications-tab` 相关设计文档。

# 2.1 工时追踪调研

## 1. 调研目标

- 确认现有工时追踪主链路。
- 找出“自动暂停”缺口以及它与当前数据模型的冲突点。

## 2. 现状链路

1. `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` · `startTimeEntryMutation` / `stopTimeEntryMutation` / `switchTimeEntryMutation` 已支持开始、停止、切换。
2. `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` · `MyTimePage` 已展示当前计时与时间记录。
3. `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` · `GlobalTimerWidget` 在全局浮层展示当前计时状态。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` · `useTimeTracking` | 当前 API 已具备 start/stop/switch/current，但没有 idle 或 auto-pause 分支。 |
| `apps/workspace/src/shared/types/time-entry.ts` · `TimeEntry` | time entry 只有 `start_time` 与 `end_time`，没有 paused 状态字段。 |
| `server/migrations/036_time_entry.up.sql` · `time_entries` | 数据库模型是连续时间区间，不支持原地暂停恢复。 |
| `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` · `handleStop` | 全局浮层已提供统一停止入口，自动暂停最好复用停止语义。 |

## 4. 数据模型或状态流

- 当前 running timer 通过“`end_time is null` 的 time entry”表达。
- 因无 paused 字段，自动暂停若要保留统计准确性，更自然的方式是“自动停止当前段并记录恢复草稿”。

## 5. 边界条件

- 浏览器 idle 检测只在前端可感知，服务端当前没有驻留式 activity agent。
- 自动暂停必须避免误触发，尤其在用户看视频、读文档但未操作的场景。

## 6. 未决问题

1. idle 阈值是全局固定还是用户可配置。
2. 自动暂停后是否自动弹出恢复提示。

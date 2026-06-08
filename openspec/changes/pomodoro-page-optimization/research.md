# 单能力 Research

## 调研目标

1. `/pomodoro` 当前承担哪些职责，缺哪些职责
2. 番茄完成记录的现有状态链路是什么
3. 高频声音控制是否已有能力基础
4. 仓库里是否已有可复用的 issue picker 模式
5. 这次优化是否可以在不改后端接口的前提下完成

## 现状链路

1. 入口  
`apps/workspace/src/router.tsx` `pomodoroRoute` 把 `path: "pomodoro"` 直接映射到 `PomodoroPage`。

2. 页面结构  
`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage` 渲染 `PomodoroTimer variant="page"`、`PomodoroTodaySummary`、`PomodoroRecentSessions`。页面没有引入声音设置组件或 issue 关联面板。

3. 计时器状态  
`apps/workspace/src/features/time-tracking/hooks/use-pomodoro.ts` `usePomodoroQuery` 通过 React Query 轮询当前 session；`useStartPomodoroMutation`、`usePausePomodoroMutation`、`useCompletePomodoroMutation`、`useResetPomodoroMutation` 负责 session 状态切换。

4. 完成记录  
`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer` 维护 `completionFlow` 与 `noteInputValue`。倒计时到 0 时，work phase 会调用 `setCompletionFlow({ isWorkPhase: true, pendingLabelIds: [] })`，然后才允许用户补 `issue / note / labels`。最终由 `handleSubmitCompletion` 组装 `CompletePomodoroBody` 并调用 `fireComplete`。

5. 声音设置  
`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `usePomodoroSettings` 从 `localStorage` 维护 `sound_enabled`、`tick_enabled`、`volume`、`white_noise` 等配置。  
`apps/workspace/src/features/settings/components/pomodoro-settings-tab.tsx` `PomodoroSettingsTab` 是当前唯一的设置 UI。  
`apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` `useSoundSystem` 已支持 tick、start、complete、white noise 播放和音量同步。

6. 关联 issue 方式  
`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer` 在 page 与 compact 两个分支里都用 `window.prompt("Enter issue ID to link:")`。  
同一 feature 里，`apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` `IssuePicker` 已经展示了基于 `PropertyPicker` 的 issue 搜索选择模式，可复用。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `pomodoroRoute` | `/pomodoro` 是独立主入口 |
| `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` | `PomodoroPage` | 页面当前只承载计时器、今日摘要、历史 |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` | `PomodoroTimer` | `Quick capture` 的可编辑内容被 `completionFlow` 控制 |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` | `handleSubmitCompletion` | 完成提交仍通过 `CompletePomodoroBody` 传递 `issue_id`、`note`、`label_ids` |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro.ts` | `useCompletePomodoroMutation` | 前端完成流程不需要新增接口即可提交记录字段 |
| `apps/workspace/src/features/settings/components/pomodoro-settings-tab.tsx` | `PomodoroSettingsTab` | 高频声音设置目前只在 Settings 页 |
| `apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` | `useSoundSystem` | 声音能力层已经具备前移到 `/pomodoro` 的基础 |
| `apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` | `IssuePicker` | 仓库已有可搜索 issue picker 可复用 |
| `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` | `GlobalTimerWidget` | sidebar 与 `/pomodoro` page 版共用 `PomodoroTimer`，需控制范围避免双重重构 |

## 数据模型或状态流

- Session 状态：
  - 来源：`PomodoroSession`
  - 读取：`usePomodoroQuery`
  - 写入：`useStartPomodoroMutation` / `usePausePomodoroMutation` / `useCompletePomodoroMutation` / `useResetPomodoroMutation`
- 声音设置：
  - 来源：`PomodoroSettings`
  - 读取与写入：`usePomodoroSettings`
  - 应用：`useSoundSystem`
- 完成记录负载：
  - 来源：`CompletePomodoroBody`
  - 字段：`issue_id`、`note`、`label_ids`、`long_break_after`
  - 写入点：`api.completePomodoro`
- 当前缺失状态：
  - 没有“运行中草稿”模型
  - `issue / note / labels` 只在完成态临时存在

## 边界条件

- 权限边界：当前番茄 session 和 history 都依赖当前 workspace 上下文；`usePomodoroQuery` 与 `usePomodoroHistoryQuery` 在无 workspace 时禁用。
- 空状态：没有 active session 时，`PomodoroTimer` 使用 settings 计算 idle 显示时长。
- 错误路径：开始、暂停、完成、重置都走 mutation，当前 UI 只有 toast 错误提示，没有草稿恢复问题。
- 页面边界：`GlobalTimerWidget` 已明确禁止在 `/pomodoro` 路由同时渲染 compact timer，避免双实例。

## 未决问题

- 本轮是否需要把“当前轮次草稿”持久化到 `localStorage`，还是仅保留内存级 page state  
  当前推荐先做内存级 state，不把草稿持久化纳入首轮范围。

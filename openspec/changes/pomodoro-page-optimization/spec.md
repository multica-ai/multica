# 单能力 Spec

## 背景

当前 `/pomodoro` 已经是明确的产品入口，但它更像展示型倒计时页面，还没有成为完整的专注工作台。用户在使用中暴露出两个表层问题：

- 高频声音控制不在 `/pomodoro` 页面中
- 记录上下文必须等一个番茄结束后才能填写

继续沿着代码现状分析后，可以确认这不是两个孤立问题，而是同一个页面信息架构不足造成的结果：启动、运行、完成、补记和声音控制被拆散在不同页面和不同时间点。

## 范围

- 本次覆盖：
  - `/pomodoro` 页内高频声音控制
  - 运行中上下文草稿
  - issue picker 替换 `window.prompt`
  - 非阻塞式完成记录
  - 当前轮次摘要
- 本次不覆盖：
  - compact sidebar 版番茄交互重构
  - 新后端接口或 schema
  - 历史统计增强

## 当前状态

- `/pomodoro` 页面只展示计时器、今日摘要和历史，不承载声音配置。
- page 版 `Quick capture` 的真实输入控件只在番茄完成后出现。
- 关联 issue 依赖用户输入 issue ID。
- work phase 完成后，记录流程是显式补记流程，不够流畅。

## 证据

- `apps/workspace/src/router.tsx` `pomodoroRoute`：`/pomodoro` 是独立路由入口。
- `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`：页面当前只渲染 `PomodoroTimer`、`PomodoroTodaySummary`、`PomodoroRecentSessions`。
- `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`：`completionFlow` 为空时 page 版 `Quick capture` 只显示占位文案。
- `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`：`window.prompt("Enter issue ID to link:")` 仍用于 issue 绑定。
- `apps/workspace/src/features/settings/components/pomodoro-settings-tab.tsx` `PomodoroSettingsTab`：高频声音设置仅存在于设置页。
- `apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` `useSoundSystem`：声音能力已支持 tick、white noise、volume 调整。

## 缺口

1. `/pomodoro` 不是完整工作台  
用户在最常用页面里无法直接调整高频声音设置，也看不到当前轮次上下文。

2. 记录上下文建立过晚  
`issue / note / labels` 只能在完成后补填，导致记录质量受记忆和打断影响。

3. 关联 issue 交互不达标  
手输 issue ID 既慢又容易出错，与仓库现有交互风格不一致。

4. 完成路径阻塞节奏  
番茄结束后用户先面对补记动作，再进入休息或下一轮，流畅性不足。

## 交接说明

- 设计优先查看：
  - `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`
  - `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx`
  - `apps/workspace/src/features/settings/components/pomodoro-settings-tab.tsx`
  - `apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx`
- 执行阶段禁止自行扩大到 compact 版，除非先更新 `design.md`

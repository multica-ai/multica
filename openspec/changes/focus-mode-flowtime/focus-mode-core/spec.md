# 单能力 Spec

## 背景

Multica 已经有 Pomodoro 页面、普通 live timer、time entry 历史和 issue 绑定能力，但它们还没有形成统一的 Focus Mode。用户需要的是一个可以承载当前工作意图、专注上下文、专注模式和历史记录的主工作台，而不是只有固定 25 分钟倒计时。

## 范围

- 本次覆盖：
  - 新增 `/focus` 作为主入口
  - `/pomodoro` 保留并重定向到 `/focus`
  - 导航显示 Focus
  - 新增 `focus_sessions` 当前运行态模型
  - 定义 Focus 与普通 running timer 的互斥规则
  - Focus 上下文支持 issue、note、labels、mode、preset、commitment
- 本次不覆盖：
  - Flowtime 动态休息算法细节
  - break 事件完整实现
  - 复杂反拖延原因分析报表
  - legacy `worklog` 写入

## 当前状态

- `/pomodoro` 是独立路由，但产品概念仍是 Pomodoro。
- 普通 live timer 与 Pomodoro session 分属不同模型。
- Pomodoro completion 会写 `time_entry(type='pomodoro')`。
- issue picker、labels、声音设置已有可复用基础。

## 证据

- `apps/workspace/src/router.tsx` `pomodoroRoute`：当前只有 `/pomodoro` 路由，没有 `/focus`。
- `apps/workspace/src/features/layout/navigation.ts` `navigationGroups`：导航中 `/pomodoro` label 为 `Pomodoro`。
- `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`：当前组件同时承载 compact 和 page 番茄钟。
- `server/pkg/db/queries/pomodoro.sql` `GetPomodoroSession`：Pomodoro 当前运行态按 `user_id + workspace_id` 查询。
- `server/migrations/036_time_entry.up.sql` `running_timer`：普通 live timer 以 `user_id` 为主键。
- `server/internal/handler/pomodoro.go` `CompletePomodoro`：Pomodoro 完成后写入 `time_entry`。
- `apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` `IssuePicker`：已有可搜索 issue picker 模式。

## 缺口

1. 产品入口缺口  
现有入口叫 Pomodoro，无法容纳 Flowtime、break flow 和反拖延启动。

2. 当前状态模型缺口  
`pomodoro_sessions` 强绑定 Pomodoro phase，无法表达 Focus Mode 的 mode、preset、commitment、reason、suggested break。

3. 互斥规则缺口  
普通 `running_timer` 和 Pomodoro session 可以同时存在，Focus Mode 需要明确启动时如何处理已有计时。

4. 上下文缺口  
当前 Pomodoro 的 issue/note/label 多在完成后补记，不是当前 Focus session 的一等上下文。

## 交接说明

- 执行阶段优先读取：
  - `openspec/changes/focus-mode-flowtime/module-overview.md`
  - `openspec/changes/focus-mode-flowtime/focus-mode-core/design.md`
  - `openspec/changes/focus-mode-flowtime/focus-mode-core/tasks.md`
- 执行阶段不得把 Flowtime 细节直接塞进旧 `PomodoroTimer`，除非先更新 design。

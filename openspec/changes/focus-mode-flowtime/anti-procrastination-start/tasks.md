# 单能力 Tasks

## Reverse Sync 状态

本任务包在 2026-06-12 回写当前代码状态：Anti-Procrastination Start 已部分实现，不能再按“从零新增”执行。

已由当前代码完成或基本完成：

- `server/internal/handler/focus.go` `validFocusReason` 已支持 `unclear_next_step`、`too_large`、`low_energy`、`avoidance`、`interruption`、`blocked`、`urgent_work`、`not_needed`、`other`。
- `server/internal/handler/focus.go` `StartFocus` / `transitionFocusWithReason` 已支持 start/pause/abandon reason 写入。
- `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx` 已提供 `2 min start` mode、commitment、start friction、pause reason、abandon reason UI。

剩余缺口：

- Complete and take break 作为 quick start 后续动作仍未单独实现；当前完成态默认 Continue Flowtime。

## 实现目标

实现 2 分钟反拖延启动、下一步承诺和轻量原因记录，原因数据写入 `focus_events`。

## 前置依赖

- `focus-mode-core` 已实现 Focus start/pause/abandon。
- `break-flow` 或 core 已实现 `focus_events`。

## 任务切片

### Task 1：扩展原因枚举和事件写入

- 目标：支持 start/pause/abandon reason。
- 当前状态：已部分完成；见 `server/internal/handler/focus.go` `validFocusReason` / `transitionFocusWithReason`。
- 文件：
  - `server/internal/handler/focus.go`
  - `server/internal/service/focus.go`
  - `server/pkg/db/queries/focus_events.sql`
  - `apps/workspace/src/shared/types/`
- 完成定义：
  - reason 枚举包含 `unclear_next_step`、`too_large`、`low_energy`、`avoidance`、`interruption`、`blocked`、`other`。
  - start 写 `focus_started`。
  - pause 写 `focus_paused`。
  - abandon 写 `focus_abandoned`。
- 验证方式：
  - Go tests 覆盖合法/非法 reason。
  - `pnpm typecheck`。

### Task 2：实现 two-minute start preset

- 目标：支持 `mode=quick_start`、`preset=two_minute_start`。
- 当前状态：已完成；`quick_start` mode、真实 2 分钟倒计时和 completed event 已存在。
- 文件：
  - `server/internal/handler/focus.go`
  - `server/internal/service/focus.go`
  - `apps/workspace/src/features/time-tracking/components/FocusModePicker.tsx`
  - `apps/workspace/src/features/time-tracking/components/FocusSessionPanel.tsx`
- 完成定义：
  - 2 分钟启动使用倒计时。
  - 完成后进入 `quick_start_completed` 或等价 UI 状态。
  - 默认主操作是 Continue Flowtime。
- 验证方式：
  - Go tests 覆盖 start payload。
  - Vitest fake timers 覆盖 2 分钟完成 UI。

### Task 3：实现下一步承诺 UI

- 目标：启动前提供轻量 commitment 输入。
- 当前状态：已部分完成；见 `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`。
- 文件：
  - `apps/workspace/src/features/time-tracking/components/FocusContextEditor.tsx`
  - 或新增 `AntiProcrastinationStartPanel.tsx`
- 完成定义：
  - 用户可填写 `commitment_text`。
  - 输入不会阻塞 Start。
  - 运行中能看到 commitment 摘要。
- 验证方式：
  - 组件测试覆盖填写和提交。

### Task 4：实现原因选择 UI

- 目标：启动、暂停、放弃支持可选原因。
- 当前状态：已部分完成；见 `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`。
- 文件：
  - `apps/workspace/src/features/time-tracking/components/FocusReasonPicker.tsx`
  - `apps/workspace/src/features/time-tracking/components/FocusSessionPanel.tsx`
- 完成定义：
  - 启动阻力原因可选。
  - pause/abandon 原因可选。
  - `other` 支持可选 note。
  - Start/Pause/Abandon 主动作不因空原因被禁用。
- 验证方式：
  - 组件测试覆盖选择、跳过、other note。

### Task 5：处理 quick start 后续动作

- 目标：2 分钟结束后支持继续 Flowtime 或完成。
- 当前状态：已部分完成；Continue Flowtime 已完成，Complete and take break 仍待后续实现。
- 文件：
  - `server/internal/handler/focus.go`
  - `server/internal/service/focus.go`
  - `apps/workspace/src/features/time-tracking/components/FocusSessionPanel.tsx`
- 完成定义：
  - Continue Flowtime 不创建 `time_entry`，只把同一 session 转成 `mode=flowtime` 或继续 focus phase。
  - Complete and take break 创建一条短 focus entry，并进入 break suggestion。
- 验证方式：
  - Go tests 覆盖 continue 不写 entry、complete 写 entry。
  - 前端组件测试覆盖两个 CTA。

## 目标文件

- `server/internal/handler/focus.go`
- `server/internal/service/focus.go`
- `server/pkg/db/queries/focus_events.sql`
- `apps/workspace/src/features/time-tracking/components/`
- `apps/workspace/src/shared/types/`

## 验证方式

- `make test`
- `pnpm typecheck`
- `pnpm test`

## 回写要求

- 如果原因枚举调整，必须同步更新 `design.md`。
- 如果 quick start 完成后的默认 CTA 变化，必须更新 UI 流程和 tasks。

# 单能力 Tasks

## Reverse Sync 状态

本任务包在 2026-06-12 回写当前代码状态：Focus Mode Core 已部分实现，不能再按“从零新增”执行。

已由当前代码完成或基本完成：

- `server/migrations/045_focus_mode.up.sql` 已新增 `focus_sessions` 和 `focus_events`。
- `server/internal/handler/focus.go` 已提供 current/start/update/pause/resume/complete/abandon/break API 处理逻辑。
- `server/cmd/server/router.go` 已注册 `/api/focus/*` route group。
- `apps/workspace/src/router.tsx` 已注册 `/focus`，并将 `/pomodoro` 重定向到 `/focus`。
- `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx` 已提供 Focus 页面基础 UI。

剩余缺口：

- Focus 入口尚未贯穿 Issue Detail、Today、My Time、Inbox。
- Quick Start 尚未形成真实 2 分钟倒计时和 `quick_start_completed` 写入路径。
- Focus signals 尚未进入 Daily Review。
- 旧 `pomodoro_sessions` 和 `/api/pomodoro/*` 仍保留，后续不得作为新 Focus 主线扩展。

## 实现目标

新增 Focus Mode 主入口和当前状态模型，为 Pomodoro、Flowtime、break flow、反拖延启动提供统一基础。

## 前置依赖

- 用户已确认 `/focus` 为新主入口，`/pomodoro` 保留重定向。
- 用户已确认新增 `focus_sessions`，不继续扩展 `pomodoro_sessions`。
- 用户已确认 Focus 历史继续落 `time_entry`，不写 `worklog`。

## 任务切片

### Task 1：新增数据库模型

- 目标：新增 `focus_sessions` 当前状态表。
- 当前状态：已完成；见 `server/migrations/045_focus_mode.up.sql`。
- 文件：
  - `server/migrations/*_focus_sessions.up.sql`
  - `server/migrations/*_focus_sessions.down.sql`
  - `server/pkg/db/queries/focus.sql`
- 完成定义：
  - 表包含 workspace/user/mode/phase/preset/context/elapsed/suggested break 字段。
  - 每个 `user_id + workspace_id` 只有一条当前 Focus session。
  - sqlc queries 覆盖 get/upsert/update/delete 或 reset。
- 验证方式：
  - `make sqlc`
  - `make test` 中相关 handler/service 编译通过。

### Task 2：新增 Focus API

- 目标：实现 `/api/focus/*` 当前状态接口。
- 当前状态：已部分完成；见 `server/internal/handler/focus.go` 和 `server/cmd/server/router.go`。
- 文件：
  - `server/internal/handler/focus.go`
  - `server/cmd/server/router.go`
  - `server/pkg/protocol/events.go`
- 完成定义：
  - 支持 current/start/pause/resume/complete/abandon/update current。
  - 所有接口校验 workspace member。
  - 启动时处理 ordinary running timer 冲突。
  - 完成时写 `time_entry`，不写 `worklog`。
- 验证方式：
  - 新增 Go handler tests。
  - `make test`。

### Task 3：新增前端 Focus route 与 query hooks

- 目标：前端接入 `/focus`。
- 当前状态：已部分完成；见 `apps/workspace/src/router.tsx`、`apps/workspace/src/features/time-tracking/hooks/use-focus.ts`。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/shared/types/`
  - `apps/workspace/src/features/time-tracking/hooks/use-focus.ts`
- 完成定义：
  - 导航显示 Focus。
  - `/pomodoro` 重定向或渲染同一 Focus 页面。
  - React Query hook 覆盖 current/start/pause/resume/complete/abandon/update。
- 验证方式：
  - `pnpm typecheck`
  - route/page unit tests。

### Task 4：实现 Focus 页面基础 UI

- 目标：构建 Focus 工作台基础骨架。
- 当前状态：已部分完成；见 `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`。
- 文件：
  - `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`
  - `apps/workspace/src/features/time-tracking/components/FocusSessionPanel.tsx`
  - `apps/workspace/src/features/time-tracking/components/FocusContextEditor.tsx`
  - 必要时抽出 `FocusIssuePicker.tsx`
- 完成定义：
  - 可选择 mode/preset。
  - 可编辑 issue/note/labels/commitment。
  - 可启动、暂停、恢复、完成、放弃 Focus。
  - ordinary timer 冲突时有显式确认。
- 验证方式：
  - Vitest + Testing Library 覆盖 start/pause/complete/abandon。

### Task 5：全局状态入口迁移

- 目标：全局 pill 显示 Focus 状态。
- 当前状态：待确认；实现前必须先检查当前 app shell/header 是否已有 Focus 状态入口。
- 文件：
  - `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx`
  - 或新增 `FocusStatusPill.tsx`
  - app shell 挂载位置相关文件
- 完成定义：
  - Focus running 时 header 显示当前模式和时间。
  - 点击进入 `/focus`。
  - Pomodoro 旧状态不再独立显示为第二个 pill。
- 验证方式：
  - 组件测试覆盖 running/paused/break_suggested 状态。

## 目标文件

- `server/migrations/`
- `server/pkg/db/queries/focus.sql`
- `server/internal/handler/focus.go`
- `server/cmd/server/router.go`
- `apps/workspace/src/router.tsx`
- `apps/workspace/src/features/layout/navigation.ts`
- `apps/workspace/src/features/time-tracking/`
- `apps/workspace/src/shared/api/client.ts`
- `apps/workspace/src/shared/types/`

## 验证方式

- 后端：`make sqlc && make test`
- 前端：`pnpm typecheck && pnpm test`
- 完整验收如用户要求再运行：`make check`

## 回写要求

- 如果实现阶段发现 `focus_sessions` 字段需要变化，先更新 `design.md`。
- 如果 `/pomodoro` 无法做重定向，先更新 `design.md` 的 UI 流程。
- 如果普通 timer 冲突策略变化，先更新接口契约和 tasks。

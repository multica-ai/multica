# 单能力 Tasks

## 实现目标

实现 break flow 的持久化事件和 UI 操作，让建议休息、开始休息、跳过休息、完成休息都有可恢复和可分析的记录。

## 前置依赖

- `focus-mode-core` 已提供 `focus_sessions`。
- `flowtime-session` 已能在 complete 后写 `suggested_break_seconds`。

## 任务切片

### Task 1：新增 `focus_events`

- 目标：新增 Focus 行为事件表。
- 文件：
  - `server/migrations/*_focus_events.up.sql`
  - `server/migrations/*_focus_events.down.sql`
  - `server/pkg/db/queries/focus_events.sql`
- 完成定义：
  - 表支持 workspace/user/session/event_type/reason/note/duration/metadata。
  - 查询支持按 session 和按用户时间范围读取。
- 验证方式：
  - `make sqlc`
  - migration test 或 Go tests 编译通过。

### Task 2：写入 break suggested 事件

- 目标：Focus complete 产生建议休息时写事件。
- 文件：
  - `server/internal/handler/focus.go`
  - `server/internal/service/focus.go`
- 完成定义：
  - Flowtime complete 后写 `break_suggested`。
  - event metadata 包含 `suggested_break_seconds` 和 `focus_duration_seconds`。
- 验证方式：
  - Go test 覆盖 complete 后 event 写入。

### Task 3：实现 break API

- 目标：支持 start/skip/complete break。
- 文件：
  - `server/internal/handler/focus.go`
  - `server/cmd/server/router.go`
  - `server/pkg/db/queries/focus.sql`
  - `server/pkg/db/queries/focus_events.sql`
- 完成定义：
  - `/api/focus/break/start`
  - `/api/focus/break/skip`
  - `/api/focus/break/complete`
  - 所有 mutation 同时更新 session 和写 event。
- 验证方式：
  - Go tests 覆盖合法 phase、非法 phase、skip reason、duration 计算。

### Task 4：前端 break UI

- 目标：Focus 页面展示和操作 break flow。
- 文件：
  - `apps/workspace/src/features/time-tracking/components/FocusBreakPanel.tsx`
  - `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`
  - `apps/workspace/src/features/time-tracking/hooks/use-focus.ts`
- 完成定义：
  - `break_suggested` 显示建议休息和 Start/Skip。
  - `breaking` 显示倒计时和 Complete。
  - skip reason 可选填写。
  - 刷新后倒计时能根据 session 恢复。
- 验证方式：
  - Vitest fake timers。
  - 组件测试覆盖 start/skip/complete。

### Task 5：事件查询基础

- 目标：为后续分析提供读取能力。
- 文件：
  - `server/internal/handler/focus.go`
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/shared/types/`
- 完成定义：
  - 可按当前 session 读取 focus events。
  - 前端类型包含 break event。
- 验证方式：
  - Go handler test。
  - `pnpm typecheck`。

## 目标文件

- `server/migrations/`
- `server/pkg/db/queries/focus_events.sql`
- `server/internal/handler/focus.go`
- `server/internal/service/focus.go`
- `apps/workspace/src/features/time-tracking/components/FocusBreakPanel.tsx`
- `apps/workspace/src/features/time-tracking/hooks/use-focus.ts`

## 验证方式

- `make sqlc`
- `make test`
- `pnpm typecheck`
- `pnpm test`

## 回写要求

- 如果 break 被改为写入 `time_entry`，必须先更新本设计包。
- 如果新增 break reason 枚举，更新 `design.md` 和前后端类型。

# Timeboxing Foundation Tasks

## 实现目标

新增 planned time block 模型、API、日历交互，并串联现有 timer。

## 前置依赖

- 建议先完成 `calendar-overlays`，使 My Time Calendar 具备 planned/actual 分层展示。
- 如果要从 plan item 创建 timebox，需先完成 `daily-weekly-planner` 的 plan item。

## 任务切片

### Slice 1：数据库与 sqlc

- 目标：新增 `planned_time_block` 表。
- 目标文件：
  - `server/migrations/<next>_planned_time_block.up.sql`
  - `server/migrations/<next>_planned_time_block.down.sql`
  - `server/pkg/db/queries/planned_time_block.sql`
- 完成定义：
  - 支持 list/create/update/delete/start/complete 所需查询。
  - 所有查询按 workspace/user 过滤。
- 验证方式：
  - `make sqlc`
  - Go query/handler tests。

### Slice 2：后端 handler/service

- 目标：实现 planned block API。
- 目标文件：
  - `server/internal/handler/planned_time_block.go`
  - `server/internal/service/planned_time_block.go`
  - `server/cmd/server/router.go`
- 完成定义：
  - CRUD 接口可用。
  - start 接口创建或切换 time entry。
  - complete 接口关联 actual time entry。
- 验证方式：
  - `cd server && go test ./internal/handler/ -run PlannedTimeBlock`

### Slice 3：前端类型/API/hooks

- 目标：新增 planned block 前端契约。
- 目标文件：
  - `apps/workspace/src/shared/types/planned-time-block.ts`
  - `apps/workspace/src/shared/types/index.ts`
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/shared/query/keys.ts`
  - `apps/workspace/src/features/time-tracking/hooks/use-planned-time-blocks.ts`
- 完成定义：
  - 支持 visible window query。
  - 支持 create/update/delete/start/complete mutations。
- 验证方式：
  - hook tests。

### Slice 4：My Time Calendar planned block events

- 目标：在 My Time Calendar 展示和编辑 planned block。
- 目标文件：
  - `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`
  - `apps/workspace/src/features/time-tracking/components/calendar/PlannedTimeBlockCard.tsx`
  - `apps/workspace/src/features/time-tracking/components/PlannedTimeBlockEditSheet.tsx`
- 完成定义：
  - planned block 可拖拽/resize。
  - actual time entry 行为不变。
  - planned block 有独立 edit sheet。
- 验证方式：
  - 组件测试覆盖拖拽 guard 和 edit sheet。

### Slice 5：从 issue / plan item 创建 timebox

- 目标：提供创建入口。
- 目标文件：
  - `apps/workspace/src/features/issues/components/issue-detail.tsx`
  - `apps/workspace/src/features/daily-plan/components/*`
  - `apps/workspace/src/features/time-tracking/components/PlannedTimeBlockCreateSheet.tsx`
- 完成定义：
  - issue detail 可创建 block。
  - plan item 可创建 block，若 plan item 尚未实现则跳过该入口。
- 验证方式：
  - 组件测试。

### Slice 6：planned vs actual 与 overload warning

- 目标：展示 planned duration、actual duration、overload warning。
- 目标文件：
  - `apps/workspace/src/features/time-tracking/utils/timebox-summary.ts`
  - `apps/workspace/src/features/time-tracking/components/TimeboxCapacityBanner.tsx`
- 完成定义：
  - 当天 planned total 超过 8h 显示 warning。
  - completed block 显示 actual duration。
- 验证方式：
  - 工具函数测试和组件测试。

## 目标文件

- `server/migrations/`
- `server/pkg/db/queries/planned_time_block.sql`
- `server/internal/handler/planned_time_block.go`
- `server/internal/service/planned_time_block.go`
- `apps/workspace/src/features/time-tracking/`
- `apps/workspace/src/features/issues/components/issue-detail.tsx`
- `apps/workspace/src/features/daily-plan/`

## 验证方式

- `make sqlc`
- `cd server && go test ./internal/handler/ -run PlannedTimeBlock`
- `pnpm --filter @multica/workspace exec vitest run src/features/time-tracking`
- 如果用户明确要求完整验证，再运行 `make check`。

## 回写要求

- 如果 overlap 从允许改为禁止，更新 `design.md` 的边界条件。
- 如果 missed 状态落库，更新 `design.md` 的状态模型和接口契约。
- 如果 start block 改变 existing timer switch 行为，更新 `research.md` 的现状链路。

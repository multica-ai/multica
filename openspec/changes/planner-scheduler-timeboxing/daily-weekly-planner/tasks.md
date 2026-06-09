# Daily Weekly Planner Tasks

## 实现目标

新增结构化 daily plan items，并提供日/周计划器页面。

## 前置依赖

- 需要先完成或确认 daily plan API 当前稳定。
- 建议先完成 `deadline-visibility`，让 planner 可复用 deadline summary。

## 任务切片

### Slice 1：数据库与 sqlc

- 目标：新增 `daily_plan_item` 表与查询。
- 目标文件：
  - `server/migrations/<next>_daily_plan_items.up.sql`
  - `server/migrations/<next>_daily_plan_items.down.sql`
  - `server/pkg/db/queries/daily_plan_item.sql`
- 完成定义：
  - 支持 create/list/update/delete/reorder。
  - `sqlc` 生成代码成功。
- 验证方式：
  - `make sqlc`
  - 后端相关单元测试。

### Slice 2：后端 service / handler

- 目标：扩展 daily plan 响应并新增 item API。
- 目标文件：
  - `server/internal/service/daily_plan.go`
  - `server/internal/handler/daily_plan.go`
  - `server/cmd/server/router.go`
- 完成定义：
  - `DailyPlanResponse` 包含 items。
  - 可按 date 获取 daily plan。
  - 可获取 week plans。
  - 可 CRUD/reorder plan items。
- 验证方式：
  - `cd server && go test ./internal/handler/ -run DailyPlan`

### Slice 3：前端类型与 API

- 目标：增加 DailyPlanItem 类型和 API client 方法。
- 目标文件：
  - `apps/workspace/src/shared/types/daily.ts`
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/shared/query/keys.ts`
  - `apps/workspace/src/features/daily-plan/hooks/use-daily-plan.ts`
- 完成定义：
  - hooks 支持 today/by-date/week/items mutation。
  - query invalidation 正确。
- 验证方式：
  - 相关 hook 测试。

### Slice 4：Daily Planner 页面

- 目标：新增 `/planner` 日计划器。
- 目标文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/daily-plan/pages/DailyPlannerPage.tsx`
  - `apps/workspace/src/features/daily-plan/components/*`
- 完成定义：
  - 可展示、添加、排序、编辑 planned minutes。
  - 可确认计划。
  - 可跳 issue detail。
- 验证方式：
  - 页面组件测试。

### Slice 5：Weekly Planner 页面

- 目标：新增 `/planner/week` 周概览。
- 目标文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/daily-plan/pages/WeeklyPlannerPage.tsx`
- 完成定义：
  - 展示周一到周日。
  - 显示每天计划项数量、planned total、deadline count。
  - 点击日期进入 daily planner。
- 验证方式：
  - 页面组件测试。

## 目标文件

- `server/migrations/`
- `server/pkg/db/queries/daily_plan_item.sql`
- `server/internal/service/daily_plan.go`
- `server/internal/handler/daily_plan.go`
- `apps/workspace/src/features/daily-plan/`
- `apps/workspace/src/router.tsx`

## 验证方式

- `make sqlc`
- `cd server && go test ./internal/handler/ -run DailyPlan`
- `pnpm --filter @multica/workspace exec vitest run src/features/daily-plan`
- 如果用户明确要求完整验证，再运行 `make check`。

## 回写要求

- 如果新增 weekly table，必须先更新 `design.md` 的数据模型。
- 如果 plan item status 与 issue status 联动，必须先更新 `design.md` 的状态模型。

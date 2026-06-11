# 单能力 Tasks

## 实现目标

实现 Flowtime 开放式专注模式，完成时按实际时长写入 `time_entry(type='flowtime')` 并生成休息建议。

## 前置依赖

- `focus-mode-core` 已实现 `focus_sessions` 和 `/api/focus/*` 基础。
- `time_entry.type` 已允许 `flowtime`，或本任务中扩展约束。

## 任务切片

### Task 1：扩展 `time_entry.type`

- 目标：允许 `flowtime` 类型。
- 文件：
  - `server/migrations/*_time_entry_flowtime_type.up.sql`
  - `server/migrations/*_time_entry_flowtime_type.down.sql`
  - 如有 CHECK 约束，更新对应迁移
- 完成定义：
  - `time_entry.type` 可写入 `flowtime`。
  - 现有 `manual`、`pomodoro` 不受影响。
- 验证方式：
  - migration 可执行。
  - Go tests 可创建 flowtime entry。

### Task 2：实现 Flowtime complete 计算

- 目标：后端按实际 elapsed 创建 time entry。
- 文件：
  - `server/internal/handler/focus.go`
  - 必要时新增 `server/internal/service/focus.go`
  - `server/pkg/db/queries/focus.sql`
- 完成定义：
  - running session complete 会计算 `elapsed_focus_seconds + now - started_at`。
  - paused session complete 使用累计 `elapsed_focus_seconds`。
  - 创建 `time_entry(type='flowtime')`。
  - completion 和 session 更新在同一 transaction 中。
- 验证方式：
  - Go tests 覆盖 running complete、paused complete、失败回滚。

### Task 3：实现休息建议算法

- 目标：首版固定分段算法。
- 文件：
  - `server/internal/service/focus.go`
  - 或 `server/internal/handler/focus.go`
- 完成定义：
  - `<25m -> 5m`
  - `25m-50m -> 10m`
  - `>50m -> 15m`
  - 返回并写入 `suggested_break_seconds`。
- 验证方式：
  - 单元测试覆盖边界值：24:59、25:00、50:00、50:01。

### Task 4：前端 Flowtime UI

- 目标：在 `/focus` 中支持 Flowtime 正向计时。
- 文件：
  - `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`
  - `apps/workspace/src/features/time-tracking/components/FocusSessionPanel.tsx`
  - `apps/workspace/src/features/time-tracking/hooks/use-focus.ts`
- 完成定义：
  - Flowtime 显示正向 elapsed。
  - 支持 start/pause/resume/complete。
  - 完成后展示建议休息 CTA。
- 验证方式：
  - Vitest fake timers 覆盖 elapsed 展示和 complete。

### Task 5：历史展示

- 目标：My Time 和 Focus history 能识别 Flowtime。
- 文件：
  - `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
  - Focus history 相关组件，如执行阶段新增
  - `apps/workspace/src/shared/types/`
- 完成定义：
  - `type='flowtime'` 显示明确来源标记。
  - 不影响 Pomodoro 和 manual entry。
- 验证方式：
  - 组件测试覆盖 flowtime entry row。

## 目标文件

- `server/internal/handler/focus.go`
- `server/internal/service/focus.go`
- `server/pkg/db/queries/focus.sql`
- `server/migrations/`
- `apps/workspace/src/features/time-tracking/`
- `apps/workspace/src/shared/types/`

## 验证方式

- `make sqlc`
- `make test`
- `pnpm typecheck`
- `pnpm test`

## 回写要求

- 如果休息算法变化，更新 `design.md` 的算法表。
- 如果 `type='flowtime'` 改为 `type='focus'` + metadata，先更新 `design.md` 和相关 tasks。

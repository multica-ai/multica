# Personal Operating System Foundation Tasks

## 实现目标

建立知识管理、注意力管理、精力管理的最小执行闭环：

```text
Issue -> Focus -> TimeEntry -> DailyReview -> DailyPlan -> Knowledge -> Agent
```

第一批实现只覆盖 Phase 0 和 Phase 1。Phase 2/3 只作为后续方向，不在本 tasks 中展开实现。

## 前置依赖

- 已确认 `issue` 是核心工作对象。
- 已确认 `time_entry` 是 actual work 主线。
- 已确认 `Skill` / `workspace.context` 是 Phase 1 知识载体。
- 已确认不引入独立 Wiki、RAG、完整 timeboxing。

## 任务切片

### Task 1. Reverse sync Focus OpenSpec

目标：修正 Focus 文档状态漂移。

目标文件：

- `openspec/changes/focus-mode-flowtime/module-overview.md`
- `openspec/changes/focus-mode-flowtime/focus-mode-core/spec.md`
- `openspec/changes/focus-mode-flowtime/focus-mode-core/tasks.md`
- 如存在对应 Flowtime / Break / Anti-procrastination 文档，也同步更新状态。

完成定义：

- 已实现能力不再标为“未开始”。
- 明确剩余缺口：入口贯穿、quick start completed、focus signals to review。
- 文档引用当前代码证据。

验证方式：

- `rg -n "未开始|StartFocus|CompleteFocus|quick_start_completed" openspec/changes/focus-mode-flowtime`

### Task 2. Document time model boundary

目标：统一 `time_entry` / `worklog` 边界。

目标文件：

- `openspec/changes/personal-operating-system-foundation/module-overview.md`
- `openspec/changes/issue-worklog/design.md`
- `openspec/changes/planner-scheduler-timeboxing/module-overview.md`

完成定义：

- 新时间、Focus、Pomodoro、Daily Review 相关能力明确使用 `time_entry`。
- `worklog` 只作为 legacy issue-bound duration model。

验证方式：

- `rg -n "worklog|time_entry" openspec/changes/personal-operating-system-foundation openspec/changes/issue-worklog openspec/changes/planner-scheduler-timeboxing`

### Task 3. Inject workspace context into agent task context

目标：agent 执行任务时读取 workspace context。

当前状态：已完成。

目标文件或目录：

- `server/internal/service/task.go`
- `server/internal/daemon/execenv/context.go`
- `server/internal/daemon/prompt.go`
- `server/pkg/protocol/`
- 相关 sqlc query 或 response type。

完成定义：

- task claim / daemon context 链路包含 workspace context。
- 空 workspace context 不输出多余 prompt 段落。
- 已有 skill 注入行为保持不变。

验证方式：

- 增加或更新 Go 单元测试，覆盖 workspace context 有值和空值两种情况。
- 运行相关 Go 测试，例如 `cd server && go test ./internal/daemon/... ./internal/service/...`。

验证记录：

- `cd server && go test ./internal/daemon/execenv`

### Task 4. Add reusable Start Focus action

目标：从 issue/today/my-time 直接开始 Focus。

当前状态：已完成。

目标文件或目录：

- `apps/workspace/src/features/time-tracking/`
- `apps/workspace/src/features/issues/components/`
- `apps/workspace/src/features/issues/utils/workbench-view.ts`
- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`

完成定义：

- 新增可复用 Start Focus 组件或 dialog。
- Issue detail 可以用当前 issue 开始 Focus。
- Today/workbench 列表可以对 issue 开始 Focus。
- My Time 保持普通 timer 能力，同时能清楚进入 Focus。

验证方式：

- 增加或更新 Vitest/Testing Library 测试。
- 运行相关前端测试，例如 `pnpm --filter @multica/workspace exec vitest run <changed-test-files>`。

验证记录：

- `pnpm typecheck`
- `pnpm --filter @multica/workspace exec vitest run src/features/time-tracking/pages/FocusPage.test.tsx`
- Browser smoke：`http://localhost:3000/focus` 可见 Current focus、Flowtime、2 min start、Start、Context，控制台无 error/warning。

### Task 5. Complete quick start two-minute loop

目标：quick start 真正完成反拖延启动闭环。

当前状态：已完成。

目标文件或目录：

- `server/internal/handler/focus.go`
- `server/pkg/db/queries/focus.sql`
- `apps/workspace/src/features/time-tracking/pages/FocusPage.tsx`
- `apps/workspace/src/features/time-tracking/hooks/use-focus.ts`
- `apps/workspace/src/shared/types/focus.ts`

完成定义：

- quick start 模式显示 2 分钟倒计时。
- 倒计时完成后写入 `quick_start_completed` event。
- 完成后用户可以继续 Flowtime、完成或放弃。

验证方式：

- Go 测试覆盖 quick start completed event。
- 前端测试覆盖倒计时完成态或完成按钮行为。

验证记录：

- `cd server && go test ./internal/handler`
- `cd server && go test ./internal/handler -run 'TestCompleteQuickStartConvertsSessionToFlowtimeAndWritesEvent'`
- `pnpm --filter @multica/workspace exec vitest run src/features/time-tracking/pages/FocusPage.test.tsx`
- `pnpm typecheck`

### Task 6. Add energy check-in to Daily Review

目标：建立最小精力信号。

当前状态：已完成。

目标文件或目录：

- `server/migrations/`
- `server/pkg/db/queries/daily_review.sql`
- `server/internal/handler/daily_review.go`
- `server/internal/service/review.go`
- `apps/workspace/src/features/daily-review/`
- `apps/workspace/src/shared/types/daily.ts`

完成定义：

- Daily Review 支持可选 `energy_level`、`energy_note`、`recovery_need`。
- 不填写不阻塞 review 生成或确认。
- Review draft 可以使用这些字段或保留为空。

验证方式：

- 运行 sqlc 生成。
- Go 测试覆盖 response/request。
- 前端测试覆盖 UI 可选填写。

验证记录：

- `make sqlc`
- `cd server && go test ./internal/handler ./internal/service`
- `pnpm --filter @multica/workspace exec vitest run src/features/daily-review/components/DailyReviewPanel.test.tsx`
- `pnpm typecheck`

### Task 7. Feed focus and energy signals into Daily Plan

目标：Daily Plan 使用精力和注意力信号调整计划。

当前状态：已完成。

目标文件或目录：

- `server/internal/service/daily_plan.go`
- `server/internal/service/review.go`
- `server/pkg/db/queries/focus.sql`
- `apps/workspace/src/features/daily-plan/`

完成定义：

- DailyPlanService 能读取最近 confirmed review 和 focus signal summary。
- ReviewService 能读取当天 focus signal summary。
- plan draft 包含 high-energy work 和 low-energy fallback。
- 不新增结构化计划项。

验证方式：

- Go 测试覆盖 prompt 输入包含 energy/focus signal。
- 如果 LLM 不可用，模板 fallback 也要体现 energy-aware section。

验证记录：

- `make sqlc`
- `cd server && go test ./internal/service`
- `pnpm typecheck`

## 后续任务占位

以下任务不属于 Phase 1，不能在未更新 spec 前直接实现：

- Skill visibility。
- Skill search / global search integration。
- Skill import/export in manifest。

结构化计划项和计划块方向已暂停；不能作为本文档的后续任务实现。

## 回写要求

- 每完成一个任务，回写对应 spec 的当前状态和剩余缺口。
- 如果实现阶段调整任务顺序，先更新 `module-overview.md` 的优先级说明。
- 如果新增数据库字段或 API response，必须同步更新对应前后端类型和测试。

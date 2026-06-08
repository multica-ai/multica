# Planning Insights Tasks

## 实现目标

交付 estimate、project health、task flow stats 和只读 roadmap 的最小闭环。

## 前置依赖

- 本 design 包范围已确认。
- 明确 estimate 第一版使用 minutes。
- 确认 lifecycle history 的来源：复用现有 activity/event，或新增 status history。

## 任务切片

### Task 1：Issue estimate

- 目标：为 issue 增加估算字段。
- 目标文件：
  - `server/migrations/`
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
  - `apps/workspace/src/shared/types/issue.ts`
  - `apps/workspace/src/features/issues/components/`
- 完成定义：
  - issue create/update/detail/list 支持 `estimate_minutes`。
  - issue detail 可编辑 estimate。
  - estimate 校验为非负整数或 null。
- 验证方式：
  - Go handler tests。
  - workspace component tests。

### Task 2：Project health

- 目标：新增 project health 聚合接口和 UI。
- 目标文件：
  - `server/pkg/db/queries/project.sql`
  - `server/internal/handler/project.go`
  - `apps/workspace/src/features/projects/`
- 完成定义：
  - 接口返回完成率、blocked、overdue、estimate、logged time。
  - Project detail 展示 health summary。
  - archived issues 默认排除。
- 验证方式：
  - Go tests 覆盖聚合口径。
  - Vitest 覆盖 UI 空状态和正常状态。

### Task 3：Task flow stats

- 目标：新增服务端任务流统计。
- 目标文件：
  - `server/pkg/db/queries/analytics.sql`
  - `server/internal/handler/analytics.go`
  - `apps/workspace/src/features/analytics/`
- 完成定义：
  - throughput、lead time、cycle time 由服务端返回。
  - 历史不足时返回 insufficient data 标记。
  - 前端不基于分页 issue list 拼统计。
- 验证方式：
  - Go tests 覆盖指标计算。
  - Vitest 覆盖 insufficient data。

### Task 4：Read-only roadmap

- 目标：新增按 project/date window 展示 issue 的 roadmap 页面。
- 目标文件：
  - `server/internal/handler/roadmap.go`
  - `apps/workspace/src/features/roadmap/`
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
- 完成定义：
  - roadmap 可按时间窗口加载 issue。
  - 第一版只读，不支持拖拽改日期。
  - 使用 issue start/end/due dates。
- 验证方式：
  - E2E 覆盖 roadmap 入口和空状态。

### Task 5：回写与验证

- 目标：同步 OpenSpec 状态。
- 目标文件：
  - 本目录 `spec.md`
  - 本目录 `design.md`
  - 本目录 `tasks.md`
- 完成定义：
  - 已完成项回写当前状态。
  - 偏差先更新 design，再改代码。
- 验证方式：
  - `make check` 或相关子集后记录证据。

## 回写要求

如果执行阶段决定引入 cycle/iteration，必须先新开独立 OpenSpec change，不能把 cycle 偷塞进本能力。

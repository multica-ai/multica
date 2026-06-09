# Deadline Visibility Tasks

## 实现目标

实现最小 deadline banner 与 summary 派生能力，不新增数据库和后端接口。

## 前置依赖

- 当前 issue list query 能返回 due/start/end 字段。
- Today / My Time / Calendar 页面已有 issue 数据或可接入 issue query。

## 任务切片

### Slice 1：deadline summary 纯函数

- 目标：新增 `deriveDeadlineSummary`。
- 目标文件：
  - `apps/workspace/src/features/issues/utils/deadline-summary.ts`
  - `apps/workspace/src/features/issues/utils/deadline-summary.test.ts`
- 完成定义：
  - 能区分 overdue、today、upcoming。
  - 排除 `done` / `cancelled`。
  - upcoming 限制未来 7 天。
- 验证方式：
  - `pnpm --filter @multica/workspace exec vitest run src/features/issues/utils/deadline-summary.test.ts`

### Slice 2：DeadlineBanner 组件

- 目标：新增可复用横幅组件。
- 目标文件：
  - `apps/workspace/src/features/issues/components/deadline-banner.tsx`
  - `apps/workspace/src/features/issues/components/deadline-banner.test.tsx`
- 完成定义：
  - 展示 overdue/today/upcoming 数量。
  - 空 summary 时不渲染或渲染低噪声状态。
  - summary item 可点击跳转。
- 验证方式：
  - `pnpm --filter @multica/workspace exec vitest run src/features/issues/components/deadline-banner.test.tsx`

### Slice 3：接入 Today / My Time / Calendar

- 目标：在执行入口页面展示 deadline banner。
- 目标文件：
  - `apps/workspace/src/features/issues/components/workbench-issues-page.tsx`
  - `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
  - `apps/workspace/src/features/issues/components/IssueCalendarPage.tsx`
- 完成定义：
  - Today 页面展示当前列表对应 summary。
  - My Time 页面展示个人执行入口 summary。
  - Calendar 页面展示 calendar issue summary。
- 验证方式：
  - 运行相关组件测试。
  - 如果用户明确要求完整验证，再运行 `make check`。

## 目标文件

- `apps/workspace/src/features/issues/utils/deadline-summary.ts`
- `apps/workspace/src/features/issues/components/deadline-banner.tsx`
- `apps/workspace/src/features/issues/components/workbench-issues-page.tsx`
- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
- `apps/workspace/src/features/issues/components/IssueCalendarPage.tsx`

## 验证方式

- 单元测试覆盖日期边界。
- 组件测试覆盖空、有数据、点击跳转。
- 不要求后端测试。

## 回写要求

- 如果实现改动了 overdue 口径，更新 `research.md` 的边界条件。
- 如果新增后端 summary API，更新 `design.md` 的接口契约。

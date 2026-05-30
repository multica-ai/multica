# 单能力 Tasks

## 实现目标

交付独立任务统计页和配套的服务端聚合能力，而不是在现有 issue 列表里拼接临时统计。

## 前置依赖

- 先锁定完成率与逾期定义。
- 先确认任务统计页是否需要 assignee 维度筛选。
- 先确认入口放在 analytics 模块，而不是 workbench 页面内部。

## 任务切片

### Task 1

- 目标：新增任务统计前端入口与页面。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/issues/components/` 或新的 analytics 目录
- 改动：
  - 新增任务统计只读页和入口。
- 完成定义：
  - 页面展示 summary 与优先级分布。
- 验证方式：
  - 路由和页面渲染测试。

### Task 2

- 目标：补任务统计前端类型、query key、hook。
- 文件：
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/shared/types/issue.ts`
  - `apps/workspace/src/shared/query/keys.ts`
  - `apps/workspace/src/features/issues/` 下统计 hook
- 改动：
  - 新增 task analytics 响应模型与查询。
- 完成定义：
  - 前端可以按 preset 拉取统计结果。
- 验证方式：
  - hook 单测覆盖 preset 切换。

### Task 3

- 目标：补任务统计服务端聚合接口与 SQL。
- 文件：
  - `server/cmd/server/router.go`
  - `server/internal/handler/issue.go`
  - `server/pkg/db/queries/issue.sql`
  - `server/pkg/db/generated/`
- 改动：
  - 新增 issue analytics handler、SQL 和返回结构。
- 完成定义：
  - 能返回完成数、待完成、逾期、完成率、优先级分布。
- 验证方式：
  - handler / SQL 测试覆盖今日/本周/本月、空数据、非法参数。

### Task 4

- 目标：补 issue 变更后的统计刷新。
- 文件：
  - `apps/workspace/src/features/issues/mutations*`
  - `apps/workspace/src/features/issues/components/*.test.tsx`
- 改动：
  - issue 创建、更新、批量导入后失效统计 query。
- 完成定义：
  - 统计页不会长期显示旧值。
- 验证方式：
  - mutation + page 联动测试。

## 执行顺序说明

- 先做口径与服务端聚合，再接前端页面；否则前端只能基于错误来源做临时统计。

## 回写要求

- 回写 `spec.md` 的完成度与缺口。
- 回写 `04-analytics/overview.md` 的优先级与页面定位。
- 在 `research.md` 中补充实际落地的 handler / SQL 证据。

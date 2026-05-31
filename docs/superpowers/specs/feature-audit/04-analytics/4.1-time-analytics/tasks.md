# 单能力 Tasks

## 实现目标

交付一个与 `My Time` 操作页分离的只读时间统计页，并补齐对应的服务端聚合契约与验证。

## 前置依赖

- 先确认 `design.md` 的推荐方案：新增独立统计页，而不是继续膨胀 `TeamTimePage`。
- 先确认标签/任务/日分布使用统一 analytics 聚合接口。
- 若需要实时刷新，先确认新 query key 命名与 invalidation 位置。

## 任务切片

### Task 1

- 目标：新增时间统计前端入口并明确“操作页/统计页”边界。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/time-tracking/pages/`
- 改动：
  - 新增统计页路由与页面组件。
  - 保留 `My Time` 为操作页，不把统计卡片塞回 `MyTimePage`。
- 完成定义：
  - 用户可直接进入独立时间统计页。
  - `My Time` 与统计页职责在 UI 上清晰区分。
- 验证方式：
  - 路由/页面测试覆盖入口展示与默认状态。

### Task 2

- 目标：补统一时间统计查询契约与前端缓存键。
- 文件：
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/shared/query/keys.ts`
  - `apps/workspace/src/shared/types/time-entry.ts`
  - `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts`
- 改动：
  - 新增时间统计请求/响应类型。
  - 为统计页增加独立 query key 与 hook。
- 完成定义：
  - 统计页不再复用原始分页列表做全量聚合。
  - query key 能被单独 invalidation。
- 验证方式：
  - hook 单测覆盖 preset/custom 参数与 query key。

### Task 3

- 目标：补服务端时间统计聚合 API。
- 文件：
  - `server/cmd/server/router.go`
  - `server/internal/handler/time_entry.go`
  - `server/pkg/db/queries/time_entry.sql`
  - `server/pkg/db/generated/`
- 改动：
  - 新增 analytics handler 和 SQL。
  - 支持项目/标签/任务/日分布四类 group-by。
- 完成定义：
  - 返回 `summary`、`breakdowns`、`daily_buckets`。
  - 范围非法时返回明确错误。
- 验证方式：
  - handler / SQL 测试覆盖 today/week/month/custom 与空结果。

### Task 4

- 目标：补统计页刷新与回归验证。
- 文件：
  - `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts`
  - `apps/workspace/src/features/time-tracking/pages/*.test.tsx`
  - `server/internal/handler/*test.go`
- 改动：
  - 将时间统计 query 纳入 invalidation。
  - 补空状态、错误态、过滤切换的测试。
- 完成定义：
  - time entry 变更后，统计页重新读取聚合数据。
- 验证方式：
  - 前后端测试都能证明统计页不会长期展示陈旧值。

### Task 5

- 目标：补时间统计页面级验证，证明真实路由、范围切换和聚合结果一致。
- 文件：
  - `e2e/time-tracking.spec.ts`
  - 若实现拆出独立统计路由，则同步更新对应 page object / helper
- 改动：
  - 覆盖统计入口可达、默认本周 summary、成员/项目聚合、范围切换、空状态或错误态。
  - 覆盖 time entry 变更后统计页重新显示最新聚合结果。
- 完成定义：
  - Playwright 能在浏览器里证明统计页不是 `My Time` 操作页的变体。
  - 至少一条用例直接验证团队统计聚合，一条用例验证范围切换或刷新行为。
- 验证方式：
  - 运行 `pnpm exec playwright test e2e/time-tracking.spec.ts`

## 执行顺序说明

- 先做前端入口与契约定义，再做服务端聚合，随后补刷新与页面级验证；否则前端页面会被迫依赖临时 mock 或错误口径。

## 回写要求

- 实现后更新 `spec.md` 的当前状态与缺口关闭情况。
- 把新增路由或页面定位回写到 `04-analytics/overview.md`。
- 在 `research.md` 中补充最终落地的 SQL / handler / hook 证据，替换“待实现”描述。

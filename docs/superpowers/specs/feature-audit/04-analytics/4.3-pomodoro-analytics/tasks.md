# 单能力 Tasks

## 实现目标

交付与 `PomodoroPage` 操作页分离的番茄统计页，并补齐周/月统计、目标契约与每日分布图。

## 前置依赖

- 先确认完成率依赖正式 target 配置，而不是页面常量。
- 先确认统计页入口位置与操作页的关系。
- 先确认月统计和日分布的接口字段。

## 任务切片

### Task 1

- 目标：新增番茄统计前端入口与页面。
- 文件：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/time-tracking/pages/`
- 改动：
  - 新增只读番茄统计页，保留现有操作页。
- 完成定义：
  - 用户能进入独立统计页，且操作页不被打断。
- 验证方式：
  - 路由与页面测试。

### Task 2

- 目标：补番茄统计接口模型和前端 hook。
- 文件：
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-history.ts` 或新 hook
  - `apps/workspace/src/shared/types/`
- 改动：
  - 新增月统计、日分布、完成率、目标契约模型。
- 完成定义：
  - 前端能稳定读取周/月/完成率/分布数据。
- 验证方式：
  - hook 单测覆盖 week/month/target 缺失场景。

### Task 3

- 目标：补服务端番茄统计聚合与目标配置契约。
- 文件：
  - `server/internal/handler/pomodoro.go`
  - `server/pkg/db/queries/pomodoro.sql`
  - `server/pkg/db/generated/`
  - `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` 及其后续持久化位置
- 改动：
  - 新增 `month_count`、`daily_buckets`。
  - 新增 target 来源字段。
- 完成定义：
  - 完成率不再依赖前端常量。
- 验证方式：
  - handler / SQL / 设置读写测试。

### Task 4

- 目标：补统计页刷新与回归验证。
- 文件：
  - `apps/workspace/src/features/time-tracking/pages/*.test.tsx`
  - `server/internal/handler/*test.go`
- 改动：
  - pomodoro 完成后失效统计 query。
- 完成定义：
  - 新完成的番茄会体现在统计页。
- 验证方式：
  - 完成 pomodoro 后 summary 更新的联动测试。

### Task 5

- 目标：补番茄统计页面级验证，证明统计入口、summary 刷新和目标缺失提示成立。
- 文件：
  - `e2e/pomodoro.spec.ts`
  - 如统计入口独立，补对应 route helper
- 改动：
  - 覆盖统计入口可达、today/week/month summary、完成 pomodoro 后刷新、目标缺失提示。
  - 若保留 `/pomodoro` 操作页，明确区分操作页与统计页断言。
- 完成定义：
  - Playwright 能证明番茄统计读取真实历史/聚合，而不是只看本地组件状态。
  - 至少一条用例覆盖完成 pomodoro 后 summary 变化，至少一条用例覆盖目标缺失提示或周/月统计。
- 验证方式：
  - 运行 `pnpm exec playwright test e2e/pomodoro.spec.ts`

## 执行顺序说明

- 先补目标契约和统计接口，再做页面，最后补页面级验证；否则完成率口径无法稳定。

## 回写要求

- 回写 `spec.md` 的当前状态与“已完成/缺失”条目。
- 回写 `04-analytics/overview.md` 的“操作页/统计页”定位。
- 用最终落地的接口和字段替换 `research.md` 里的现状描述。

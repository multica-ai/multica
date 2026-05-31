# 单能力 Design

## 目标

- 为任务提供独立统计页，覆盖今日/本周/本月完成任务数、待完成任务数、逾期任务数、完成率与优先级分布。

## 非目标

- 不重写 issue 列表、看板和 workbench 页面。
- 不把 workbench 视图上的筛选结果误标成统计结果。
- 不引入新的任务存储模型。

## 当前架构基线

- 当前入口：  
  - `apps/workspace/src/router.tsx` `routeTree`：没有任务统计路由。  
  - `apps/workspace/src/features/layout/navigation.ts` `primaryNav`：没有任务统计导航项。
- 当前核心逻辑：  
  - `server/pkg/db/queries/issue.sql` `ListIssues`：只有列表筛选。  
  - `apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPageContent`：只有视图过滤与列表展示。
- 当前存储或状态：  
  - `apps/workspace/src/shared/types/issue.ts` `Issue`：已有 `status`、`priority`、`due_date`、`end_date` 等统计源字段。
- 当前 UI 或接口：  
  - `apps/workspace/src/features/issues/components/issues-header.tsx` `BulkImportButton`：当前任务区域偏输入和筛选操作，缺统计视图。

### 代码证据

- `server/pkg/db/queries/issue.sql` `ListIssues`：现有查询没有统计聚合。
- `apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPageContent`：只对 issue 列表做筛选。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：未来统计可复用现有字段。

## 缺口定义

- 产品面为空白：没有统计入口，也没有统计返回模型。
- 数据面为空白：没有聚合 API，导致完成率和分布都无处承接。
- 口径面空白：没有定义“完成”“逾期”“完成率”的统一计算规则。

## 方案与权衡

### 方案 A：前端基于 issue 列表直接聚合

- 做法：在前端读取 `useIssueStore` 或列表接口结果，自行求和并画图。
- 优点：前期看似开发快，不需要新增服务端接口。
- 风险：列表天然受分页、筛选、加载范围影响；完成率和逾期统计会失真，也不适合沉淀为审计能力。

### 方案 B：新增服务端任务统计聚合

- 做法：新增任务统计 handler / SQL，前端只消费只读 summary + distribution。
- 优点：口径单一、可测试、可扩展到周/月/优先级分布。
- 风险：需要补新接口与 SQL，首轮实现成本更高。

## 推荐方案

- 推荐方案 B。
- 完成率定义：`完成率 = 已完成任务数 / (已完成任务数 + 当前待完成任务数 + 当前逾期任务数)`。  
  - 证据：`Issue.status` 已明确 `done/cancelled` 等状态；结论：完成率应基于当前任务状态桶，而不是页面可见列表数量。  
- 逾期定义：`due_date < 当前统计时刻且 status 不在 done/cancelled`。  
  - 证据：`server/pkg/db/queries/issue.sql` `ListIssues` 已使用 `due_date` 和 `status NOT IN ('done', 'cancelled')` 过滤 today/upcoming；结论：现有仓库已有逾期相关原始字段与状态语义。  
- 推荐落地：新增独立任务统计页与 `GET /api/issues/analytics`（或同等聚合接口）。

## 数据模型或状态模型

- `TaskAnalyticsResponse`
  - `summary`: `today_completed` / `week_completed` / `month_completed` / `pending_count` / `overdue_count` / `completion_rate`
  - `priority_distribution[]`: `{ priority, count }`
  - `status_distribution[]`: `{ status, count }`
  - `computed_at`
- 状态变化
  - 进入统计页即读取 summary。
  - 筛选时间窗口后刷新相同 summary。
  - issue 更新/批量导入后失效任务统计 query。
- 关键约束
  - 统计页使用“全 workspace 事实源”，不受当前 issue 列表过滤器影响。

## 接口契约

### 输入

- `preset`: `today | week | month`
- 可选 `scope`: `workspace | assignee`
- 可选 `assignee_id`

### 输出

- `summary`
- `priority_distribution`
- `status_distribution`
- `computed_at`
- 错误场景
  - 未知预设值
  - assignee 不属于 workspace
  - workspace 权限失败

## UI 或交互流程

1. 用户从独立任务统计入口进入。
2. 页面先展示完成数、待完成、逾期、完成率四块摘要。
3. 用户切换今日/本周/本月后刷新 summary 与优先级分布。
4. 页面保持只读，不跳转为 issue 编辑操作。

### 页面交互流

```text
[导航 Task Analytics]
        |
        v
[默认本周 summary]
   |          |
   |          +--> [查看优先级分布]
   |
   +--------------> [切换 today/week/month]
                          |
                          v
                    [刷新 summary]
```

### 状态机

```text
[idle]
  |
  v
[loading] --> [error]
  |
  +--> [empty]
  |
  +--> [ready]
          |
          v
   [changing-preset]
          |
          v
       [loading]
```

### 数据变化流

```text
[issue 创建/更新/批量导入]
          |
          v
[issue analytics handler + SQL]
          |
          v
[taskAnalytics query]
          |
          v
[Task Analytics Page]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 与 issue 列表相同，限定在 workspace 成员范围内。
- 哪些输入非法  
  - 非法 preset、跨 workspace assignee、未知状态映射。
- 失败时如何处理  
  - 展示统计页错误态，不降级为 issue 列表。

## 实现约束

- 不能把统计口径写死在组件里；必须放在服务端聚合层。
- 不能直接复用当前列表页的客户端过滤结果做全局统计。
- 完成率、逾期定义必须按本设计执行，执行 Agent 不得自行重定义。

## 页面级验证策略

- 需要页面级验证。
- 触发原因：
  1. 任务统计是新入口和新只读页面，必须证明它不会与 workbench 工作视图混淆。
  2. summary、优先级分布、完成率依赖新的服务端聚合契约，单靠 SQL 或 hook 测试不能覆盖真实集成路径。
  3. issue 创建、更新、批量导入后统计是否刷新，是高回归风险点。
- 现有基线：
  - 当前仓库没有任务统计专属 Playwright 覆盖，`Today` / `Upcoming` / `Backlog` 的现有 E2E 不能替代 analytics 验证。
- 执行时必须覆盖的用户路径：
  1. 用户进入任务统计页并看到本周或默认预设的 summary 卡片。
  2. 用户切换 today/week/month 后，完成数、逾期数、完成率与优先级分布同步刷新。
  3. issue 状态或截止时间变化后，统计页在浏览器里反映新口径。
- 关键断言：
  - 统计页展示独立 heading 和 summary，不复用 issue 列表项作为主内容。
  - 完成率与逾期数不受当前 issue 列表筛选器影响。
  - 空状态、错误态和刷新态在真实页面里可见。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 前端偷用列表缓存做统计 | 数据失真 | 在设计与任务里明确必须新增聚合接口 |
| 逾期口径与工作台不一致 | 用户理解混乱 | 复用现有 `due_date + status not in done/cancelled` 语义并写测试 |
| 批量导入后统计不刷新 | 数据看起来错 | 将 issue 变更纳入任务统计 query invalidation |

## 验收检查

1. 用户能看到今日/本周/本月完成任务数、待完成、逾期、完成率。
2. 用户能看到优先级分布，且结果不依赖当前 issue 列表筛选器。
3. 统计口径在 handler / SQL / 前端页面测试中一致。

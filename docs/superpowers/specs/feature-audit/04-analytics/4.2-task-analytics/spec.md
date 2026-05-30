# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `4.2 任务统计`；结论：任务统计要覆盖完成数、待完成数、逾期数、完成率、优先级分布。
- 证据：`server/pkg/db/queries/issue.sql` `ListIssues`；结论：当前 issue 能力只提供列表与筛选，未形成统计产品面，因此需要完整设计包而不是直接实现。

## 范围

- 本次覆盖：任务统计页、服务端聚合契约、完成率/逾期/优先级分布口径。
- 本次不覆盖：issue 创建编辑、看板重构、workbench 视图重做。

## 当前状态

- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：没有任务统计入口。
- 证据：`apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPageContent`；结论：当前只有工作列表与筛选，没有统计摘要。
- 证据：代码搜索 `apps/workspace/src`、`server`，关键词 `task analytics|任务统计|completion rate|priority distribution|完成率统计|优先级分布`；结论：未找到匹配，说明 4.2 是空白能力。

## 证据

- `server/pkg/db/queries/issue.sql` `ListIssues`：支持状态、优先级、日期筛选，但没有聚合字段。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：任务原始字段已足够支撑统计。
- `apps/workspace/src/features/issues/components/workbench-issues-page.tsx` `WorkbenchIssuesPageContent`：现有视图只面向操作与筛选。
- `apps/workspace/src/features/layout/navigation.ts` `primaryNav`：导航没有任务统计入口。

## 缺口

1. 没有任务统计入口，用户无法看到任务完成概览。
2. 没有服务端聚合，客户端若直接用列表页统计会受分页/筛选条件影响而失真。
3. “完成率”“逾期数”没有统一口径，执行阶段不能自行脑补。

## 交接说明

- workbench 的 Today / Upcoming / Backlog 只能算工作视图，不能算任务统计实现。
- 进入实现前优先阅读 `design.md` 里对完成率与逾期口径的定义。
- 统计实现需要新增服务端聚合，不建议从 `useIssueStore` 的现有列表缓存直接求总数。

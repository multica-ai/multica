# 模块级设计总览

## 目标与范围

- 本轮补齐 `04-analytics/` 下 4.1、4.2、4.3 的设计包，并把“操作页”与“统计页”边界固定下来。
- 本轮包含时间统计、任务统计、番茄统计的 research/design/tasks，以及模块级推进顺序。
- 本轮不包含代码实现、不改 `dashboard.md`、不把 My Time / Pomodoro 的操作能力误记为完整统计能力。

## 能力列表

| 能力 | 当前状态 | 页面定位 | 优先级 | 备注 |
| --- | --- | --- | --- | --- |
| 4.1 时间统计 | 部分完成 | `MyTimePage` 是操作页，`TeamTimePage` 是轻量统计页 | P1 | 已有按成员/项目聚合，但没有独立统计入口 |
| 4.2 任务统计 | 缺失 | 仅有操作/筛选页，无统计页 | P2 | 需要新增服务端聚合契约 |
| 4.3 番茄统计 | 部分完成 | `PomodoroPage` 是操作页，只有轻量摘要 | P1 | 已有今日数与周统计字段，但未形成统计页 |

## 当前状态基线

### 操作页基线

- 证据：`apps/workspace/src/router.tsx` `myTimeRoute`、`pomodoroRoute`；结论：当前一级导航公开的是 `/my-time` 与 `/pomodoro` 两个以记录和专注操作为主的页面，而不是 `/analytics/*` 统计路由。
- 证据：`apps/workspace/src/features/layout/navigation.ts` `primaryNav`；结论：主导航把 `My Time`、`Team Time`、`Pomodoro` 与业务页并列，没有独立“Analytics”入口。
- 证据：`apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `StartTimerBar`、`MyTimePage`；结论：`My Time` 的主流程是开始/停止计时、编辑 time entry、查看按天分组列表，属于操作页。
- 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`；结论：`Pomodoro` 页面以内嵌 `PomodoroTimer` 的专注流程为核心，摘要只是附属信息，仍属于操作页。

### 统计页基线

- 证据：`apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`；结论：当前唯一接近统计页的是 `Team Time`，但它只支持按成员/按项目聚合，不覆盖日/月趋势、标签、任务维度。
- 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useTeamTimeStatsQuery`；结论：前端已有面向团队时间汇总的只读查询入口。
- 证据：`server/internal/handler/time_entry.go` `GetTeamTimeStats`；结论：后端目前只返回 `by_user` 与 `by_project` 两组聚合结果，没有任务统计或图表 bucket。
- 证据：`server/pkg/db/queries/pomodoro.sql` `GetPomodoroStats`；结论：番茄统计目前只有 `today_count`、`week_count`、`total_seconds`，没有月统计、完成率或分布图 bucket。

### 缺口基线

- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：仓库里没有独立 analytics 路由，导致操作页与统计页职责混杂。
- 证据：`server/pkg/db/queries/issue.sql` `ListIssues`；结论：任务数据当前仅支持筛选和列表返回，没有完成率、优先级分布、逾期计数等聚合查询。
- 证据：代码搜索 `apps/workspace/src`、`server`，关键词 `task analytics|任务统计|completion rate|priority distribution|完成率统计|优先级分布`；结论：未找到匹配，说明 4.2 仍是空白能力。

## 非目标

- 不在本轮把 `My Time`、`Pomodoro` 的操作页改写成“大而全”仪表盘。
- 不在本轮补 `dashboard.md` 或其他模块文档。
- 执行 Agent 不允许自行新增未在设计包中定义的统计口径。

## 优先级与推进顺序

1. 先推进 4.1 时间统计：证据：`useTeamTimeStatsQuery` 与 `GetTeamTimeStats` 已经存在；结论：它最接近独立统计页，复用成本最低。
2. 再推进 4.3 番茄统计：证据：`PomodoroPage` 与 `GetPomodoroStats` 已有今日/周数据；结论：可以在不碰计时主流程的前提下补齐统计页。
3. 最后推进 4.2 任务统计：证据：`ListIssues` 与 `WorkbenchIssuesPage` 只有筛选/列表逻辑；结论：该能力要先补服务端聚合契约，适合在时间/番茄统计模式稳定后再做。

## 共享约束

- 共享页面约束：操作页负责“开始/停止/编辑/导入输入”，统计页负责“只读汇总与筛选”，两者不能混成同一主页面。
- 共享数据约束：优先复用 `time_entry`、`issue`、`pomodoro` 现有实体，不新增平行统计存储。
- 共享查询约束：统计页必须以服务端聚合或明确的只读查询为准，不能依赖分页列表在客户端二次拼装全量统计。
- 共享交互约束：统计页至少要有范围切换、空状态、加载态、刷新态，并明确个人/团队视角。
- 共享技术约束：如果实现阶段发现统计页需要实时刷新，必须在现有 `queryKeys.timeTracking.*` 与 websocket invalidation 体系内补齐，不允许临时轮询分叉。
- 共享验证约束：4.1、4.2、4.3 都涉及跨路由真实用户路径和前后端聚合契约，执行阶段必须保留独立 Playwright 切片，不能只用 hook 单测或 handler 测试代替页面级验证。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 操作页与统计页继续混用 | 用户难以理解入口，范围审计会重复误判 | 在 4.1 与 4.3 设计中显式新增独立统计入口或统计子页 |
| 任务统计缺少服务端聚合 | 客户端基于分页列表统计会失真 | 4.2 推荐先补聚合 API，再接前端页面 |
| 番茄完成率缺少目标契约 | 无法定义统一口径 | 4.3 先把“目标值来源”作为前置决策，未落定前不默认计算 |
| 现有 websocket 不刷新统计聚合 | 新统计页可能展示陈旧数据 | 实现时要把统计 query key 纳入 invalidation 范围 |
| 页面级验证缺口持续存在 | 文档写了统计页，但交付 gate 无法证明真实路径成立 | 每个子能力都在 `design.md` 写明页面级验证策略，并在 `tasks.md` 拆独立 Playwright 切片 |

## 回写规则

- 4.1、4.2、4.3 实现后都要同步更新各自 `spec.md` 的“当前状态/缺口/交接说明”。
- 如果新增独立统计页或路由，要回写本 overview 的“页面定位”列，继续明确“操作页”与“统计页”。
- 如果统计口径调整，必须把证据补回 `research.md` 与 `design.md`，不能只在代码里变化。

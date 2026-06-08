# Planning Insights Design

## 目标

建立最小 planning insights 基线，让团队能从 issue/project/time 数据中看到估算、进度、任务流效率和轻量 roadmap，而不是只在操作页中查看记录。

## 非目标

- 不实现完整 cycle/iteration 系统。
- 不实现企业级 BI dashboard。
- 不新增独立 roadmap card 实体。
- 不把 My Time 或 Pomodoro 操作页改成统计大盘。

## 当前架构基线

- Issue 已有 project、dates、priority、archive 生命周期字段。
- Project 已有 CRUD、status、lead、project board 和 progress UI。
- Time entry 已有 team stats。
- Pomodoro 已有轻量 stats。
- 路由没有 analytics/roadmap 专属入口。

## 缺口定义

第一版需要补齐：

1. Estimate：issue 级估算字段。
2. Project health：服务端聚合项目完成率、逾期数、blocked 数、投入时间。
3. Task flow metrics：throughput、lead time、cycle time。
4. Roadmap/timeline：按 project 和 date window 展示 issue。

## 方案与权衡

### 方案 A：先做完整 cycle 和 roadmap

优点：规划能力完整。缺点：数据模型过大，不适合维护期后的轻装里程碑。

### 方案 B：先做估算 + 聚合 + 只读 roadmap

优点：复用现有 issue/project/time 数据，能快速形成可验证价值。缺点：不包含完整迭代节奏。

### 方案 C：只做前端 dashboard

优点：快。缺点：统计失真，不能支撑后续自动化。

## 推荐方案

采用方案 B。

## 数据模型或状态模型

建议新增：

- `issue.estimate_minutes` nullable integer，第一版只使用 minutes，避免 story points 与 minutes 双轨。
- `issue_status_history` 或等价 event source，用于计算 lead/cycle time。如果当前 activity/event 足够稳定，可先复用；否则新增专用表。

建议新增聚合响应：

- `ProjectHealth`
  - total_issues
  - completed_issues
  - blocked_issues
  - overdue_issues
  - total_estimate_minutes
  - total_logged_seconds
- `TaskFlowStats`
  - throughput_by_day
  - lead_time_p50/p90
  - cycle_time_p50/p90
- `RoadmapWindow`
  - projects
  - issues with start/end/due date

## 接口契约

建议接口：

- `PATCH /api/issues/{id}` 增加 `estimate_minutes`。
- `GET /api/projects/{id}/health`。
- `GET /api/analytics/task-flow?since=&until=&project_id=`.
- `GET /api/roadmap?since=&until=&project_id=`.

统计接口必须服务端聚合。

## UI 或交互流程

- Issue detail：在属性区展示和编辑 estimate。
- Projects page：项目详情展示 health summary。
- Analytics page：展示 task flow metrics。
- Roadmap page：按项目和时间窗口展示 issue timeline。

## 权限、边界条件、异常路径

- 只有 workspace member 可读统计。
- issue estimate 编辑权限沿用 issue update 权限。
- archived issue 默认不进入 active health，但 analytics 可通过参数包含。
- lead/cycle time 数据不足时显示 insufficient data，而不是计算 0。

## 实现约束

- 统计必须服务端聚合，不能用当前页面列表拼装。
- roadmap 第一版必须复用 issue/project，不新增独立 card。
- estimate 只使用一个字段，避免兼容 story points 和 minutes。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| 估算单位争议 | 第一版使用 minutes，后续需要 story points 再独立设计 |
| lifecycle 历史不足 | 先定义 insufficient data 状态，不伪造指标 |
| roadmap 过早复杂化 | 第一版只读 timeline，不做 drag/drop 调整 |
| 前端统计失真 | 聚合全部走后端查询 |

## 验收检查

- Issue 可保存和展示 estimate_minutes。
- Project health 接口返回完成率、逾期、blocked、投入时间。
- Task flow stats 能返回 throughput 和 lead/cycle time buckets。
- Roadmap page 能按时间窗口展示项目下 issue。
- 空数据和历史不足场景有明确 UI。

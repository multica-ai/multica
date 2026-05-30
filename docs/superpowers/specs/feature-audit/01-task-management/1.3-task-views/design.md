# 1.3 任务视图设计

## 1. 目标

1. 在现有列表 / 看板 / 日历之外补齐已完成、已归档、四象限、时间轴视图。
2. 保持每个视图都能回到同一 issue 详情与筛选体系。

## 2. 非目标

- 不实现 saved views。
- 不把四象限升级成独立优先级算法。
- 不为时间轴引入全新项目甘特系统。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/router.tsx` · 各 issue route | 现有视图入口已经路由化。 |
| `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent` | 列表型视图有成熟承载壳层。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` | 服务端视图参数需要扩展才能稳定支持 completed / archived。 |

## 4. 缺口定义

- 缺已完成、已归档视图入口与过滤契约。
- 缺四象限、时间轴的展示组件与 fallback 规则。

## 5. 方案与权衡

### 方案 A：把所有视图都塞进一个页面切 tab

优点：入口集中。  
缺点：不符合 `router.tsx` 的现有模式，也不利于分享 URL。

### 方案 B：继续走独立路由，列表型视图复用壳层，推荐

优点：贴合现有路由；完成/归档可直接复用列表页；四象限/时间轴可独立演化。  
缺点：需要补更多 route 与导航。

## 6. 推荐方案

采用方案 B，并分两阶段推进：

1. **阶段 1**：已完成视图、已归档视图。
2. **阶段 2**：四象限视图、时间轴视图。

原因：`server/pkg/db/queries/issue.sql:ListIssues` 当前已能按状态组合扩展完成视图，而已归档必须依赖 1.1；四象限和时间轴主要是展示组合，不应反向驱动底层 schema。

## 7. 数据模型或状态模型

- `completed`：`status in (done, cancelled)` 的命名视图。
- `archived`：依赖 `archived_at != null`。
- `quadrant`：基于 `priority` + `due_date` 组合分区。
- `timeline`：基于 `start_date` / `end_date` / `due_date` 的时间区间展示。

## 8. 接口契约

- `GET /api/issues?view=completed`
- `GET /api/issues?archived_only=true`
- 四象限和时间轴优先复用现有 issue 列表查询，不新增独立资源。

## 9. UI 或交互流程

- `completed` / `archived` 使用列表页壳层。
- `quadrant` 新增四宫格组件。
- `timeline` 新增时间轴条带组件。

### 页面交互流

```text
导航切换视图
  -> 进入 completed / archived / quadrant / timeline
  -> 载入对应 queryParams
  -> 点击某个任务
  -> 打开同一任务详情页
```

### 状态机

```text
默认列表
  -> completed
  -> archived
  -> quadrant
  -> timeline
任一视图
  -> issue detail
  -> 返回原视图
```

### 数据变化流

```text
Route
  -> IssueListPage / QuadrantView / TimelineView
  -> useIssuesListQuery
  -> issue handler + ListIssues
  -> 视图组件渲染
  -> IssueDetail
```

## 10. 权限、边界条件、异常路径

- 已归档视图依赖 1.1，1.1 未实现前只能保留占位路由。
- 时间轴优先使用 `start_date`/`end_date`，缺区间时回退到 `due_date` 单点。
- 四象限中无截止日期 issue 默认进入“重要不紧急/待安排”区，并在 UI 标注推导规则。

## 11. 实现约束

- 不得在每个视图各起一套 issue 查询契约。
- `completed` 与 `archived` 的计数、筛选、排序应继续复用 1.4 的定义。

## 12. 风险与对策

- 风险：完成视图与归档视图语义混淆。  
  对策：完成视图是工作状态，归档视图是生命周期，导航文案与筛选条件分开。
- 风险：四象限 / 时间轴过早要求新字段。  
  对策：本阶段只依赖现有时间字段，复杂调度留作低优先级。

## 13. 验收检查

1. 存在 completed 与 archived 视图入口。
2. completed 视图只显示已完成/已取消任务。
3. archived 视图只显示已归档任务。
4. 四象限视图可解释每个任务的落区规则。
5. 时间轴视图可从任务跳转到同一详情页。

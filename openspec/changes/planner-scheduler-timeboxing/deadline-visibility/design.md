# Deadline Visibility Design

## 目标

提供一个小改动、可闭环的 deadline visibility 层，让用户在进入 Today、My Time、Calendar 等执行页面时，立即看到已逾期、今日到期和未来 7 天即将到期的任务。

## 非目标

- 不实现桌面通知或提醒调度。
- 不实现 banner dismiss 持久化。
- 不新增 deadline 数据表。
- 不改变 issue due/start/end 的保存逻辑。
- 不实现完整 planner 页面。

## 当前架构基线

- Issue 已有 due/start/end 字段。
- Workbench 已有 Today / Upcoming 派生函数。
- 任务卡片和列表已有 schedule label。
- 路由已有 Today、Upcoming、Calendar、My Time 页面。

### ASCII 图

```text
Issue[]
  |
  | due_date / end_date / start_date
  v
deriveDeadlineSummary(issues, now)
  |
  +--> overdue items  -> count + top items
  +--> today items    -> count + top items
  +--> upcoming items -> count + top items
  |
  v
DeadlineBanner
  |
  +--> Today Page
  +--> My Time Page
  +--> Calendar Page
  |
  v
click item -> /issues/:id
```

Severity priority:

```text
overdue due/end
    >
today due/end
    >
active start/end window
    >
upcoming due/end within 7 days
```

## 缺口定义

当前缺口是“日期存在，但没有跨页面聚合提示”。用户必须进入具体列表或逐个任务扫描，才能知道今天哪些 deadline 需要处理。

## 方案与权衡

### 方案 A：后端新增 deadline summary API

优点：口径统一，可支持通知和后台提醒。缺点：第一版改动更大，需要新增 handler/query/tests。

### 方案 B：前端基于现有 issue list 派生 summary

优点：改动小，复用现有 query 和工具函数，适合作为第一步。缺点：依赖页面已加载 issue list，不适合后台提醒。

### 方案 C：只增强卡片 badge

优点：更小。缺点：不能形成 planner banner，和 Super Productivity deadline banner 差距仍明显。

## 推荐方案

采用方案 B，并为后续方案 A 留接口形状。

第一版新增前端 `DeadlineSummary` 工具与组件：

- `deriveDeadlineSummary(issues, now)`：纯函数。
- `DeadlineBanner`：页面级横幅，展示 overdue/today/upcoming。
- `DeadlineSummaryList`：可选的紧凑列表，展示 top 3 deadline items。

## 数据模型或状态模型

```ts
type DeadlineSeverity = "overdue" | "today" | "upcoming";

interface DeadlineSummaryItem {
  issueId: string;
  identifier: string;
  title: string;
  severity: DeadlineSeverity;
  date: string;
}

interface DeadlineSummary {
  overdueCount: number;
  todayCount: number;
  upcomingCount: number;
  items: DeadlineSummaryItem[];
}
```

排序规则：

1. overdue 最前，按日期升序。
2. today 次之，按日期升序。
3. upcoming 最后，按日期升序，限制未来 7 天。

## 接口契约

第一版不新增后端接口。

后续预留接口：

- `GET /api/issues/deadline-summary?since=&until=&assignee=me`
- 返回字段与前端 `DeadlineSummary` 同构。

## UI 或交互流程

- Today 页面顶部显示 `DeadlineBanner`。
- My Time 页面顶部显示 `DeadlineBanner`，因为该页是个人执行入口。
- Calendar 页面顶部显示 `DeadlineBanner`，并提供跳转到 issue detail 的链接。
- Banner 文案保持紧凑：
  - `3 overdue`
  - `2 due today`
  - `5 upcoming`
- 点击 summary item 跳转到 `/issues/$id`。

## 权限、边界条件、异常路径

- 只展示当前 workspace 可见 issue。
- 第一版不过滤 assignee，沿用当前页面 issue list 范围；如果页面是 My Time，后续可切换为 `assignee=me`。
- issue 日期解析失败时跳过该 issue。
- issue status 为 `done` 或 `cancelled` 时跳过。

## 实现约束

- `deriveDeadlineSummary` 必须是纯函数并有单元测试。
- 不在组件内重复实现日期判断。
- 不新增 API client 方法。
- 不改变 `deriveTodayIssues` / `deriveUpcomingIssues` 的现有行为；如需共享 helper，先保证测试覆盖。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| 前端派生 summary 与后端列表分页不完整 | 第一版在页面 query 使用足够 limit；后续切 API 聚合 |
| banner 过于打扰 | 第一版不做弹窗，只做静态紧凑横幅 |
| due/start/end 语义混乱 | banner 只把 due/end 作为 deadline，start/end active window 作为次级提示 |

## 验收检查

- 有逾期 issue 时，Today 页面顶部显示 overdue 数量。
- 有今日 due/end issue 时，显示 due today 数量。
- 有未来 7 天 due/end issue 时，显示 upcoming 数量。
- done/cancelled issue 不进入 summary。
- 点击 summary item 能进入 issue detail。

# 单能力 Design

## 目标

- 在不破坏 `My Time` 操作页的前提下，补一个独立时间统计面，覆盖今日/本周/本月/自定义、项目/标签/任务维度与每日分布图表。

## 非目标

- 不重写计时器创建、停止、编辑链路。
- 不把 `Team Time` 继续堆成“又能操作又能统计”的混合页。
- 不修改 `dashboard.md` 或其他模块统计定义。

## 当前架构基线

- 当前入口：  
  - `apps/workspace/src/router.tsx` `myTimeRoute` / `teamTimeRoute`：时间能力入口分散在两个路由。  
  - `apps/workspace/src/features/layout/navigation.ts` `primaryNav`：没有独立时间统计导航项。
- 当前核心逻辑：  
  - `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useTimeEntriesQuery`：负责原始记录列表。  
  - `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` `useTeamTimeStatsQuery`：负责团队聚合。
- 当前存储或状态：  
  - `apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`：原始事实已含 `issue_id`、`labels`。  
  - `apps/workspace/src/shared/query/keys.ts` `queryKeys.timeTracking.teamStats`：统计有独立缓存键，但刷新链路不完整。
- 当前 UI 或接口：  
  - `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `MyTimePage`：操作页。  
  - `apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`：轻量统计页。  
  - `server/internal/handler/time_entry.go` `GetTeamTimeStats`：只返回成员/项目聚合。

### 代码证据

- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `MyTimePage`：当前主要展示记录列表与计时操作。
- `apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx` `TeamTimePage`：当前只有成员/项目两张表。
- `server/pkg/db/queries/time_entry.sql` `ListTimeEntriesByUserRange`：原始范围查询已存在。
- `server/pkg/db/queries/time_entry.sql` `SumTimeEntriesByProjectInWorkspace`：项目聚合已有 SQL。
- `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`：实时刷新尚未覆盖统计聚合。

## 缺口定义

- 时间统计没有独立产品入口，用户必须在操作页中“顺带看统计”。
- 现有聚合只能回答“谁记录了多久、哪个项目花了多久”，无法回答“今天/本周/本月多少工时、标签/任务分布、每日走势”。
- 统计缓存没有纳入 websocket invalidation，新增统计页后会更明显暴露陈旧问题。

## 方案与权衡

### 方案 A：继续扩 `Team Time`

- 做法：把今日/本周/本月卡片、标签/任务图表都加到 `TeamTimePage`。
- 优点：复用现有页面和查询入口，前端改动表面最少。
- 风险：`TeamTimePage` 会同时承担团队统计、个人统计、图表筛选，页面职责继续膨胀，也无法把 `My Time` 操作页与统计页彻底分开。

### 方案 B：新增独立时间统计页

- 做法：保留 `My Time` 为操作页，新增只读时间统计页；`Team Time` 可复用为统计页内部的团队视角或被替换为 analytics 子视图。
- 优点：页面职责清晰，能自然承接 4.1 的全部统计维度，也符合模块 overview 要求的“操作页/统计页”区分。
- 风险：需要新增路由、导航与统一聚合契约，首轮工作量高于方案 A。

## 推荐方案

- 推荐方案 B。
- 证据：`MyTimePage` 已经承担大量创建/编辑操作，继续叠统计会让交互继续混杂；`TeamTimePage` 又只有轻量表格，不适合承接全部维度。  
- 推荐落地：  
  1. `My Time` 保持操作页；  
  2. 新增 `Time Analytics` 只读统计页；  
  3. 服务端统一新增时间统计聚合契约，支持 `preset/custom`、`scope(personal/team)`、`group_by(project|label|issue|day)`。

## 数据模型或状态模型

- 新增前端读取模型 `TimeAnalyticsResponse`
  - `summary`: `today_seconds` / `week_seconds` / `month_seconds`
  - `range`: `preset`、`since`、`until`
  - `breakdowns`: `projects[]`、`labels[]`、`issues[]`
  - `daily_buckets[]`: `{ date, total_seconds }`
- 状态变化
  - 进入统计页时按默认 `preset=week` 拉取一次。
  - 修改预设或自定义时间段时只刷新统计 query，不影响操作页 current timer。
  - time entry 变更后失效 `timeAnalytics` query。
- 关键约束
  - 统计页只读，不直接写 time entry。
  - 标签/任务统计必须来源于服务端聚合，不允许拿分页列表在客户端“猜总数”。

## 接口契约

### 输入

- `scope`: `personal | team`
- `preset`: `today | week | month | custom`
- `since` / `until`: 当 `preset=custom` 时必填
- `group_by`: `project | label | issue | day`

### 输出

- `summary`: 今日/本周/本月工时秒数
- `breakdowns`: 当前窗口内的项目/标签/任务聚合数组
- `daily_buckets`: 当前窗口每日工时 bucket
- 错误场景：
  - `since/until` 非法：返回参数错误
  - `custom` 范围为空：返回空数组和空状态
  - workspace 不存在：返回鉴权/成员错误

## UI 或交互流程

1. 用户从独立时间统计入口进入，而不是从 `My Time` 操作页内联触发。
2. 页面默认展示本周汇总卡片、项目/标签/任务 breakdown、每日分布图。
3. 用户可切换今日/本周/本月/自定义时间段，也可在个人/团队视角之间切换。
4. 当数据为空、范围非法或统计刷新失败时，页面返回明确反馈，不回退到操作页。

### 页面交互流

```text
[导航 Time Analytics]
        |
        v
[默认本周统计]
   |      |      |
   |      |      +--> [切换 group_by=项目/标签/任务]
   |      |
   |      +----------> [切换 personal/team]
   |
   +-----------------> [切换 today/week/month/custom]
                               |
                               v
                         [刷新统计结果]
```

### 状态机

```text
[idle]
  |
  v
[loading] ---> [error]
  |
  +--> [empty]
  |
  +--> [ready]
          |
          +--> [changing-filter]
                    |
                    v
                 [loading]
```

### 数据变化流

```text
[time_entry 变更]
      |
      v
[统计聚合 API]
      |
      v
[queryKeys.timeTracking.timeAnalytics]
      |
      v
[Time Analytics Page]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 证据：`server/internal/handler/time_entry.go` `GetTeamTimeStats`；结论：统计页沿用 workspace member 权限边界。
- 哪些输入非法  
  - `custom` 缺失 `since/until`、`until <= since`、未知 `group_by`。
- 失败时如何处理  
  - 请求失败保留筛选条件与最近一次成功结果的空态提示，不回退操作页。

## 实现约束

- 执行阶段不能自行把 `My Time` 入口删除或隐藏；操作页必须保留。
- 必须复用 `time_entry`、`issue`、`time_entry_labels` 现有实体，不允许新建平行“统计快照表”绕过源数据。
- 禁止用分页列表在浏览器端拼装全量统计。
- 如果补 websocket 刷新，必须复用现有 `queryKeys` 与 `useTimeTrackingSync` 体系。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 标签/任务聚合 SQL 需要 join 额外表 | 查询复杂度上升 | 把 `group_by` 收口到统一 handler，并为不同维度分别写 SQL |
| 时区影响今日/本周 bucket | 统计口径前后不一致 | 在接口契约中固定时区规则，并在测试覆盖边界时间 |
| 统计刷新不及时 | 新统计页展示陈旧数据 | 把新 query key 纳入 websocket invalidation 和 mutation success invalidation |

## 验收检查

1. 用户能从独立统计入口看到今日/本周/本月/自定义时间汇总，而不是只能在操作页里看列表。
2. 用户能切换项目/标签/任务 breakdown 和每日分布图，且数据都来自同一窗口。
3. 统计查询、空状态、错误态、缓存刷新都有自动化验证。

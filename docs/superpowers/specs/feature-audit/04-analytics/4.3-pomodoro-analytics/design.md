# 单能力 Design

## 目标

- 在保留 `PomodoroPage` 专注操作属性的前提下，补齐周/月统计、完成率与每日分布图，并形成独立番茄统计面。

## 非目标

- 不重写番茄计时器和设置页。
- 不把完整统计继续塞回操作页首屏。
- 不在没有目标契约前硬算完成率。

## 当前架构基线

- 当前入口：  
  - `apps/workspace/src/router.tsx` `pomodoroRoute`：当前只有一个番茄入口。  
  - `apps/workspace/src/features/layout/navigation.ts` `primaryNav`：没有番茄统计子入口。
- 当前核心逻辑：  
  - `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-history.ts` `usePomodoroHistoryQuery`：页面统一读取历史和轻量聚合。  
  - `apps/workspace/src/shared/api/client.ts` `getPomodoroHistory`：当前只有历史接口。
- 当前存储或状态：  
  - `apps/workspace/src/shared/types/time-entry.ts` `TimeEntry.type`：番茄完成记录写入 time entry。  
  - `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `PomodoroSettings`：没有目标值字段。
- 当前 UI 或接口：  
  - `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`：计时器是主角，摘要是附属。  
  - `server/pkg/db/queries/pomodoro.sql` `GetPomodoroStats`：接口只到 today/week。

### 代码证据

- `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`：当前是操作页。
- `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `TODAY_TARGET`：目标值写死为常量。
- `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `PomodoroSettings`：没有 target 字段。
- `server/pkg/db/queries/pomodoro.sql` `GetPomodoroStats`：缺月统计与分布。
- `server/internal/handler/pomodoro.go` `GetPomodoroHistory`：周统计已可返回。

## 缺口定义

- 周统计字段未接 UI，属于“后端强于前端”。
- 月统计与日分布需要新增聚合输出。
- 完成率依赖目标契约，而当前只有页面常量，没有可复用配置。

## 方案与权衡

### 方案 A：继续扩 `PomodoroPage`

- 做法：在 `PomodoroPage` 内增加周/月卡片、分布图与完成率。
- 优点：不新增路由，用户路径短。
- 风险：页面继续同时承担“开始专注”和“分析结果”，操作页会越来越重。

### 方案 B：新增独立番茄统计页

- 做法：保留 `PomodoroPage` 为操作页，新建只读统计页承接周/月/完成率/图表。
- 优点：与模块 overview 的“操作页/统计页”边界一致，也更利于以后扩周/月/趋势。
- 风险：需要新增入口和接口。

## 推荐方案

- 推荐方案 B。
- 完成率定义：先新增 `daily_target_pomodoros`（用户级设置），再以 `range_completed / range_target` 计算。  
  - 证据：`PomodoroSettings` 目前没有目标值字段；结论：若不先补配置契约，完成率会长期依赖 `TODAY_TARGET` 常量，不能扩展到周/月。  
- 当前阶段优先级：  
  1. 接通现有 `week_count`；  
  2. 补 `month_count` 与 `daily_buckets`；  
  3. 最后补基于目标契约的完成率。

## 数据模型或状态模型

- `PomodoroAnalyticsResponse`
  - `summary`: `today_count` / `week_count` / `month_count` / `completion_rate`
  - `daily_buckets[]`: `{ date, completed_count }`
  - `target`: `{ source, daily_target, range_target }`
- 状态变化
  - 进入统计页先请求默认 `preset=month`。
  - 修改范围后刷新 summary 和分布图。
  - 完成一个 pomodoro 后失效统计 query。
- 关键约束
  - 完成率必须依赖正式 target 契约，不能继续用页面常量。

## 接口契约

### 输入

- `preset`: `today | week | month`
- 可选 `timezone`

### 输出

- `summary`
- `daily_buckets`
- `target`
- 错误场景
  - 非法 preset
  - 缺失目标契约时，`completion_rate` 返回 `null` 并给出可解释状态

## UI 或交互流程

1. 用户在番茄操作页旁边进入统计页。
2. 统计页展示今日/本周/本月数量、完成率与每日分布图。
3. 用户切换周/月视角时刷新 summary 与图表。
4. 当目标契约未配置时，完成率卡片展示“待配置目标”而不是伪造百分比。

### 页面交互流

```text
[Pomodoro 操作页] ---> [Pomodoro Analytics]
                             |
                             +--> [看今日/周/月 summary]
                             |
                             +--> [看每日分布图]
                             |
                             +--> [配置目标后看完成率]
```

### 状态机

```text
[idle]
  |
  v
[loading] --> [error]
  |
  +--> [ready-no-target]
  |
  +--> [ready-with-target]
```

### 数据变化流

```text
[completePomodoro]
       |
       v
[pomodoro time_entry + stats SQL]
       |
       v
[pomodoroAnalytics query]
       |
       v
[Pomodoro Analytics Page]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 当前用户在当前 workspace 下查看自己的番茄统计。
- 哪些输入非法  
  - 非法 preset、无效 timezone。
- 失败时如何处理  
  - 历史失败与统计失败分开提示，不能把统计错误伪装成“无数据”。

## 实现约束

- 禁止继续依赖 `TODAY_TARGET` 常量表达长期完成率口径。
- 必须保留 `PomodoroPage` 的操作页角色，不把统计页并回主操作区域。
- 必须复用 `time_entry.type = "pomodoro"` 现有事实源。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 目标值来源不稳定 | 完成率失真 | 先补设置契约，再开放完成率 |
| 周统计已存在但 UI 未接 | 用户误判为后端不支持 | 首批实现先把 `week_count` 接通并验证 |
| 本地时区与服务端时区不一致 | 日分布图错桶 | 在接口参数中固定 timezone，测试跨日边界 |

## 验收检查

1. 用户可以在独立统计页看到今日/本周/本月番茄数。
2. 用户可以看到每日分布图，且数据来源清晰。
3. 若未配置目标，完成率卡片明确提示缺少目标；若已配置，则百分比计算一致。

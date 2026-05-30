# 单能力 Research

## 调研目标

- 确认仓库里是否已有统一数据导出能力。
- 确认当前可以复用哪些实体作为导出事实源。
- 确认导出入口为什么必须独立，而不能继续挂在单个功能页上。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`；结论：设置页没有导出入口。
   - 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：路由中没有数据导出页面。
2. 数据流  
   - 证据：`apps/workspace/src/shared/types/issue.ts` `Issue`；结论：issue 已有完整业务字段，可作为导出实体之一。
   - 证据：`apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`；结论：time entry 已有时间、标签、关联 issue 等字段，可作为导出实体之一。
   - 证据：`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `PomodoroSettings`；结论：番茄设置也是用户数据的一部分，需要纳入导出契约。
3. 输出结果  
   - 证据：代码搜索 `apps/`、`server/`，关键词 `export.*json|json.*export|导出.*JSON`；结论：未找到匹配，说明当前没有统一导出实现。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `SettingsPage` | 当前没有数据管理或导出设置入口。 |
| `apps/workspace/src/router.tsx` | `routeTree` | 当前没有导出专属路由。 |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | issue 数据可以直接进入导出快照。 |
| `apps/workspace/src/shared/types/time-entry.ts` | `TimeEntry` | 时间记录数据也适合作为导出快照的一部分。 |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` | `PomodoroSettings` | 番茄设置是需要导出的配置类数据。 |
| 代码搜索 `apps/`、`server/` | `rg(export.*json|json.*export|导出.*JSON)` | 未找到匹配，说明统一导出能力缺失。 |

## 数据模型或状态流

- 当前事实源  
  - 证据：`Issue`、`TimeEntry`、`PomodoroSettings`；结论：导出能力不需要新建实体，只需定义打包契约。
- 当前读取边界  
  - 证据：`apps/workspace/src/shared/api/client.ts` `authHeaders`；结论：所有数据都受 `X-Workspace-ID` 限制，导出也必须是 workspace 级。
- 当前状态  
  - 证据：`server/cmd/server/router.go` `RequireWorkspaceMember`；结论：导出应复用现有 workspace 成员权限。

## 边界条件

- 权限边界  
  - 导出必须只在当前 workspace 生效，不能跨 workspace 聚合。
- 空状态  
  - 空 workspace 也要能导出合法但为空的 manifest。
- 错误路径  
  - 若部分实体读取失败，导出必须明确失败，而不是静默导出半份数据。

## 未决问题

- CSV 是否作为主格式还是派生格式；该项在 `design.md` 中固定为“JSON 为主、CSV 为投影”。
- 导出入口放在 settings 还是独立数据管理页；该项在 `design.md` 中给出推荐方案。

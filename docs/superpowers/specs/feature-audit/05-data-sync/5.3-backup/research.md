# 单能力 Research

## 调研目标

- 确认当前仓库里是否已有备份或恢复能力。
- 确认为什么 5.3 需要依赖 5.1/5.2 的统一契约。
- 判断 5.3 在当前产品阶段中的优先级。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`；结论：当前没有备份/恢复入口。
   - 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：当前没有备份/恢复页面。
2. 数据流  
   - 证据：代码搜索 `apps/`、`server/`，关键词 `workspace backup|backup file|恢复数据|restore data|导入.*json|json backup`；结论：未找到匹配，说明当前没有备份或恢复实现。
   - 证据：`apps/workspace/src/shared/types/issue.ts` `Issue`、`apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`、`apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `PomodoroSettings`；结论：备份未来应直接复用这些现有实体，而不是另造专用存储。
3. 阶段判断  
   - 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal` 与 `server/internal/handler/issue.go` `BulkCreateIssues`；结论：当前连统一导入导出契约都未形成，备份恢复显然还缺公共基础。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `SettingsPage` | 没有备份/恢复入口。 |
| `apps/workspace/src/router.tsx` | `routeTree` | 没有备份/恢复页面。 |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | 备份应直接复用业务实体。 |
| `apps/workspace/src/shared/types/time-entry.ts` | `TimeEntry` | 备份应直接复用业务实体。 |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` | `PomodoroSettings` | 配置类数据也属于备份范围。 |
| 代码搜索 `apps/`、`server/` | `rg(workspace backup|backup file|恢复数据|restore data|导入.*json|json backup)` | 未找到匹配，说明备份/恢复链路缺失。 |

## 数据模型或状态流

- 当前事实源  
  - 证据：`Issue`、`TimeEntry`、`PomodoroSettings`；结论：备份没有额外事实源，应该直接包裹 canonical export manifest。
- 当前状态  
  - 证据：`SettingsPage`；结论：当前没有任何备份状态、最近备份记录、恢复记录 UI。

## 边界条件

- 权限边界  
  - 备份与恢复都必须受 workspace 成员权限控制。
- 空状态  
  - 空 workspace 也允许生成空备份包。
- 错误路径  
  - 恢复失败必须可追踪，不能像普通导入那样只给模糊 toast。

## 未决问题

- 当前阶段是否需要自动备份调度；该项在 `design.md` 中被明确降为低优先级。
- 备份是否包含附件/文件资产；该项在 `design.md` 中限定为“先不纳入当前阶段”。

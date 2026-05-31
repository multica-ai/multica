# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `5.3 数据备份`；结论：该能力包括手动本地备份、自动备份、备份文件管理和恢复。
- 证据：代码搜索 `apps/`、`server/`，关键词 `workspace backup|backup file|恢复数据|restore data|导入.*json|json backup`；结论：当前仓库没有备份恢复实现，因此必须先设计再实现。

## 范围

- 本次覆盖：备份文件契约、手动备份/恢复流程、与 5.1/5.2 的依赖关系、阶段判断。
- 本次不覆盖：当前阶段的自动调度、附件资产打包、外部存储连接器。

## 当前状态

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`；结论：没有备份/恢复入口。
- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：没有备份/恢复页面。
- 证据：`apps/workspace/src/features/issues/components/bulk-import-modal.tsx` `BulkImportModal` 与 `server/internal/handler/issue.go` `BulkCreateIssues`；结论：统一导入导出管线尚未形成，备份恢复缺公共基础。

## 证据

- `SettingsPage`：无数据管理标签。
- `routeTree`：无备份页面。
- `Issue` / `TimeEntry` / `PomodoroSettings`：备份未来应该复用这些实体。
- 代码搜索 `rg(workspace backup|backup file|恢复数据|restore data|导入.*json|json backup)`：未找到匹配。

## 缺口

1. 没有备份入口。
2. 没有备份格式。
3. 没有恢复流程。
4. 缺少统一导入导出契约，导致该能力不宜先行。

## 交接说明

- 5.3 被标记为低优先级，不建议在 5.1/5.2 之前实现。
- 备份应直接包裹 canonical manifest 与恢复元数据，不再自定义另一套格式。
- 自动备份与附件资产不属于当前阶段目标。

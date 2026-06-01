# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `5.1 数据导出`；结论：该能力要求 JSON 全量导出、时间段导出、工时 CSV、任务列表 CSV。
- 证据：代码搜索 `apps/`、`server/`，关键词 `export.*json|json.*export|导出.*JSON`；结论：当前仓库没有统一导出实现，因此必须先定义设计包而不是直接动手做格式输出。

## 范围

- 本次覆盖：统一导出契约、入口位置、多格式策略。
- 本次不覆盖：真正的云盘同步、备份调度、`dashboard.md` 报表导出。

## 当前状态

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`、`apps/workspace/src/features/settings/components/data-tab.tsx` `DataTab`；结论：Settings 已新增 Data 标签和 Export JSON 入口。
- 证据：`server/internal/handler/data_sync.go` `ExportWorkspaceData`、`server/cmd/server/router.go` `/api/data/export`；结论：后端导出接口已接入 workspace 路由。
- 证据：`server/internal/service/data_sync.go` `BuildExportManifest`；结论：导出契约已统一为 `schema_version=2026-05-31`，并支持分页聚合 issues。

## 证据

- `SettingsPage`：无数据管理标签。
- `routeTree`：无导出路由。
- `Issue` / `TimeEntry` / `PomodoroSettings`：导出事实源已存在。
- 代码搜索 `rg(export.*json|json.*export|导出.*JSON)`：未找到匹配。

## 缺口

1. 多格式派生（CSV/Excel/PDF）仍未实现，当前只交付 canonical JSON。
2. 导出范围暂只覆盖 issues，time entry / pomodoro 等实体仍待扩展。

## 验证记录

- 证据：`server/internal/service/data_sync_test.go` `TestBuildExportManifest_IncludesIssuesAndSchemaVersion`、`TestBuildExportManifest_PaginatesWithoutSilentTruncation`；结论：导出契约和分页行为已被单测覆盖。
- 证据：`server/internal/handler/handler_test.go` `TestExportWorkspaceData`；结论：导出接口返回 200 和 manifest。
- 证据：`e2e/data-sync.spec.ts` `can export workspace manifest from data tab`；结论：页面入口可触发后端导出并返回有效 manifest。

## 交接说明

- `design.md` 已固定“JSON 为主格式，CSV/Excel/PDF 为派生视图”的推荐方案。
- 后续执行时不要分别为 CSV/Excel/PDF 再造一份独立数据源。
- `5.1/5.2/5.3` 必须共享同一个 manifest 版本。

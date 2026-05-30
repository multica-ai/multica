# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `5.1 数据导出`；结论：该能力要求 JSON 全量导出、时间段导出、工时 CSV、任务列表 CSV。
- 证据：代码搜索 `apps/`、`server/`，关键词 `export.*json|json.*export|导出.*JSON`；结论：当前仓库没有统一导出实现，因此必须先定义设计包而不是直接动手做格式输出。

## 范围

- 本次覆盖：统一导出契约、入口位置、多格式策略。
- 本次不覆盖：真正的云盘同步、备份调度、`dashboard.md` 报表导出。

## 当前状态

- 证据：`apps/workspace/src/features/settings/components/settings-page.tsx` `SettingsPage`；结论：没有导出入口。
- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：没有导出页面。
- 证据：`apps/workspace/src/shared/types/issue.ts` `Issue`、`apps/workspace/src/shared/types/time-entry.ts` `TimeEntry`；结论：源数据已经存在，只缺导出契约和入口。

## 证据

- `SettingsPage`：无数据管理标签。
- `routeTree`：无导出路由。
- `Issue` / `TimeEntry` / `PomodoroSettings`：导出事实源已存在。
- 代码搜索 `rg(export.*json|json.*export|导出.*JSON)`：未找到匹配。

## 缺口

1. 没有统一导出入口。
2. 没有 canonical export 契约。
3. 没有多格式派生规则，执行阶段不能自行决定 JSON/CSV/PDF/Excel 各自用什么数据源。

## 交接说明

- `design.md` 已固定“JSON 为主格式，CSV/Excel/PDF 为派生视图”的推荐方案。
- 后续执行时不要分别为 CSV/Excel/PDF 再造一份独立数据源。
- `5.1/5.2/5.3` 必须共享同一个 manifest 版本。

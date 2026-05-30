# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `4.3 番茄统计`；结论：该能力不仅要有今日番茄数，还要有周/月数、完成率和每日分布图。
- 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`；结论：当前番茄能力主要服务专注操作，统计只做到轻量摘要，需要设计包把统计页与操作页拆开。

## 范围

- 本次覆盖：周/月统计、完成率定义、每日分布图、统计入口与现有 Pomodoro 操作页的边界。
- 本次不覆盖：番茄计时器交互重做、声音/时长设置改版。

## 当前状态

- 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`；结论：当前页面已经有今日摘要和近期历史，因此状态为部分完成。
- 证据：`apps/workspace/src/shared/api/client.ts` `PomodoroHistoryStats`；结论：客户端已有 `week_count` 字段类型，但 UI 还未消费。
- 证据：`server/pkg/db/queries/pomodoro.sql` `GetPomodoroStats`；结论：后端目前没有月统计和日分布 bucket。

## 证据

- `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `TODAY_TARGET`：今日目标是页面常量，不是正式配置契约。
- `apps/workspace/src/features/time-tracking/hooks/use-pomodoro-settings.ts` `PomodoroSettings`：设置结构没有 target 字段。
- `server/internal/handler/pomodoro.go` `GetPomodoroHistory`：服务端响应返回 `today_count` / `week_count` / `total_seconds`。
- `server/pkg/db/queries/pomodoro.sql` `GetPomodoroStats`：SQL 没有月统计和分布字段。

## 缺口

1. 周统计已有字段但没接 UI。
2. 月统计与日分布在接口层就不存在。
3. 完成率缺少目标值来源，执行阶段不能自行定义。

## 交接说明

- 后续优先看 `research.md` 中关于 `TODAY_TARGET` 常量和 `PomodoroSettings` 的证据。
- 进入实现前必须先锁定完成率目标来源。
- 当前“今日番茄数已完成”只代表轻量摘要完成，不代表番茄统计页完成。

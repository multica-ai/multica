# Daily Weekly Planner Spec

## 背景

Multica 已有 AI 明日计划能力，但目前计划内容主要是 Markdown 草稿和 top issue IDs。Super Productivity 的 planning workflow 更强调把今日/本周任务变成可调整的计划列表，并能和 schedule/timeboxing 结合。Daily / Weekly Planner 的目标是把现有 daily plan 从“生成文案”升级为“结构化个人计划”。

## 范围

本能力覆盖：

- daily planner：按日期展示计划项、top outcomes、未计划任务、已确认状态。
- weekly planner：按周展示每天的计划摘要和容量概览。
- plan items：结构化绑定 issue，可排序、可标记 planned/done/skipped。
- AI draft 兼容：保留 Markdown 作为生成摘要，不作为唯一数据源。

## 当前状态

- 后端已有 `daily_plan` 表。
- 服务端能生成明日计划和确认计划。
- 前端 My Time 页面已有 `DailyPlanPanel`。
- 类型 `DailyPlan` 只有 `draft_content` 和 `top_issue_ids`，没有结构化计划项。

## 证据

- `server/migrations/039_daily_plan.up.sql` `daily_plan`：已有 daily plan 表。
- `server/pkg/db/queries/daily_plan.sql` `UpsertDailyPlan` / `GetDailyPlanByDate` / `ListDailyPlans` / `ConfirmDailyPlan`：已有 daily plan CRUD-like 查询。
- `server/internal/service/daily_plan.go` `DailyPlanService.GeneratePlanDraft`：生成计划草稿。
- `server/internal/service/daily_plan.go` `DailyPlanService.ConfirmPlan`：确认计划。
- `apps/workspace/src/features/daily-plan/components/DailyPlanPanel.tsx` `DailyPlanPanel`：前端展示明日计划。
- `apps/workspace/src/shared/types/daily.ts` `DailyPlan`：前端类型只有 Markdown 和 top issue IDs。

## 缺口

- 没有当前日 planner 页面。
- 没有 weekly planner。
- daily plan 没有结构化 item。
- top issue IDs 不能表达排序、状态、估算、计划时间。
- plan 不能转成 timebox。

## 交接说明

执行 Agent 不能直接删除 `draft_content`。第一版必须兼容现有 daily plan 数据，并通过新增 plan item 模型逐步承接可操作计划。

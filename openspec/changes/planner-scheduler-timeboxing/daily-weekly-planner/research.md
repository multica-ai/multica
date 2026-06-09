# Daily Weekly Planner Research

## 调研目标

确认现有 daily plan 生成、存储、前端展示链路，并判断升级为结构化日/周计划所需的数据模型与接口。

## 现状链路

1. 用户在 My Time 页面点击生成计划。
2. 前端 `DailyPlanPanel` 调用 `useGeneratePlanMutation`。
3. API client 调用 `POST /api/daily-plans/generate`。
4. 后端 `DailyPlanService.GeneratePlanDraft` 查询当前用户 open issues 和昨日 review。
5. LLM 或 template 生成 Markdown。
6. `UpsertDailyPlan` 保存到 `daily_plan.draft_content`。
7. 用户可确认计划，`ConfirmDailyPlan` 设置 `status=confirmed`。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `server/migrations/039_daily_plan.up.sql` | `daily_plan` | 已有 daily plan 表，但没有 item 表 |
| `server/pkg/db/queries/daily_plan.sql` | `UpsertDailyPlan` | upsert 只保存 `draft_content` / `top_issue_ids` |
| `server/internal/service/daily_plan.go` | `GeneratePlanDraft` | 计划生成基于 open issues 和昨日 review |
| `server/internal/service/daily_plan.go` | `templatePlan` | fallback 生成 Markdown 文案 |
| `apps/workspace/src/features/daily-plan/hooks/use-daily-plan.ts` | `useTomorrowPlanQuery` / `useGeneratePlanMutation` | 前端只拉取明日计划 |
| `apps/workspace/src/features/daily-plan/components/DailyPlanPanel.tsx` | `DailyPlanPanel` | UI 是 panel，不是 planner 页面 |
| `apps/workspace/src/shared/types/daily.ts` | `DailyPlan` | 类型没有 plan items |
| `apps/workspace/src/router.tsx` | route tree | 没有 `/planner` 或 `/planner/week` 路由 |

## 数据模型或状态流

现有：

- `daily_plan`
  - `plan_date`
  - `draft_content`
  - `top_issue_ids`
  - `status`

建议新增：

- `daily_plan_item`
  - `id`
  - `workspace_id`
  - `daily_plan_id`
  - `issue_id`
  - `title_snapshot`
  - `position`
  - `planned_minutes`
  - `status`
  - `notes`

weekly planner 第一版不需要单独 `weekly_plan` 表，可通过日期范围查询 daily plans 和 open issues 聚合生成。

## 边界条件

- 已确认 plan 仍可编辑，但编辑后 status 是否回 draft 需明确。
- plan item 绑定的 issue 被删除或归档时，item 应保留 title snapshot。
- 一个 issue 可出现在多个日期的 plan 中，但同一天不应重复。
- AI 生成 Markdown 失败时，仍应能手动创建 plan item。

## 未决问题

- 确认后的计划是否允许修改？
- weekly planner 是否需要独立确认状态？
- plan item status 是否与 issue status 联动？
- plan item 是否可以不绑定 issue，作为 personal note/task？

# Issue Energy Loop 技术规格

## Context

本规格对应 [PRODUCT.md](./PRODUCT.md)。结构化 Plan 已从当前版本移除：不保留 `/plan` 页面、`/api/plans` facade、`plan_item` 表、计划项状态，也不让 Focus 或 Time Entry 关联计划项。

当前实现保留的闭环基础是：

- Issue 是核心工作对象，后续执行入口必须优先关联 issue。
- Issue type 是 workspace 级分类，用于表达工作形态和精力负载倾向。
- Focus session 记录用户当前执行状态、阻力原因、暂停/放弃/完成事件。
- Time entry 记录实际投入，仍只关联 issue 和 label。
- Daily review 记录精力状态、恢复需求和当天回顾。
- Daily plan 继续作为 Markdown 草稿能力存在，但不承载结构化计划项。

## Runtime Object Matrix

| Object | Runtime role | Can drive execution state? | Forbidden parallel semantics |
| --- | --- | --- | --- |
| `issue` | 唯一可执行工作对象，承载用户或 agent 要推进的工作。 | Yes. Issue 可以被 Focus、Time Entry、agent task 和 review 引用。 | 不允许再创建 `plan_item`、`todo_item` 等平行可执行工作项来承载同一类工作。 |
| `issue_type` | Issue 的工作形态和负载画像。 | No. 只能影响分类、展示、筛选和建议。 | 不允许用 `suggested_issue_type_id` 等字段在非 issue 对象上复制 issue type 语义。 |
| `focus_session` | 执行中的用户专注状态和事件容器。 | Partially. 只能表达当前专注生命周期。 | 不允许拥有计划项状态、排序或完成语义；只能关联 `issue_id`、description、commitment、label 和原因信号。 |
| `time_entry` | 实际投入记录。 | No. 只记录实际时间和关联上下文。 | 不允许关联 `plan_item_id` 或作为计划完成状态来源。 |
| `daily_review` | 当日执行和精力复盘。 | No. 只汇总和反馈信号。 | 不允许创建、排序或完成计划项。 |
| `daily_plan` | Markdown 草稿和 AI 生成文本。 | No. 只能作为轻量记录。 | 不允许承载结构化计划项、状态流转、执行入口或 Focus/Time Entry 外键。 |

任何新规格如果想新增可执行对象，必须先说明为什么 `issue` 无法承载该语义，并在产品评审前更新本矩阵。

## Removed Structured Plan Surface

本次删除以下结构化 Plan 运行时表面：

- `apps/workspace/src/features/plan/`
- 前端 `/plan` route 和主导航入口。
- `apps/workspace/src/shared/types/plan.ts`
- API client 中的 `getPlan`、`upsertPlan`、`listPlanCandidates`、`createPlanItem`、`updatePlanItem`、`deletePlanItem`、`startPlanItemFocus`。
- Query keys 中的 `plan.byDate` 和 `plan.candidates`。
- `server/internal/handler/plan.go`
- `server/pkg/db/queries/plan.sql`
- sqlc 生成的 `plan.sql.go`。
- `server/cmd/server/router.go` 中 `/api/plans` 和 `/api/plan-items` 路由。
- E2E `e2e/plan.spec.ts`。

## Data Model

### Migration Boundaries

稳定核心和实验表面必须拆成独立迁移、独立提交：

- 稳定核心：`issue_type`、`issue.issue_type_id` 这类已确认会长期保留的 issue 模型扩展。
- 实验表面：Plan 页面、`plan_item`、计划项状态、计划项排序、计划项执行入口、Focus/Time Entry 到计划项的外键。
- 不允许在同一个迁移里同时引入稳定核心和实验表面；否则实验表面回滚会污染核心模型历史。
- 如果实验表面被撤回，应新增专门的 remove migration，并避免继续扩大旧表面。

### Issue Type

保留 `issue_type` 表和 `issue.issue_type_id`：

- `issue_type.workspace_id` 隔离 workspace。
- `issue_type.key` 是稳定标识。
- `issue_type.name/color/icon/description/load_profile/position` 支持展示和排序。
- `load_profile` 支持 `deep_work`、`light_work`、`recovery`、`neutral`。
- 默认 seed task、feature、bug、chore、research、recovery。
- 已有 issue 回填到 task。

### Removed Plan Data

迁移 `048_remove_structured_plan` 删除：

- `plan_item` 表。
- `focus_sessions.plan_item_id`。
- `time_entry.plan_item_id`。
- 结构化 Plan 曾加在 `daily_plan` 上的 `energy_level`、`energy_note`、`recovery_need`、`capacity_minutes`、`capacity_note`。

注意：daily review 的精力字段继续保留，它们属于复盘能力，不属于结构化 Plan。

## API Contracts

### Kept

- `/api/issues` 和 issue detail/update/create 继续支持 `issue_type_id`。
- `/api/issue-types` 继续提供 workspace 级 issue type 管理。
- `/api/focus/*` 继续支持 issue 关联、description、commitment、label、阻力原因、暂停/放弃/完成原因。
- `/api/time-entries` 继续支持 issue 和 label。
- `/api/daily-plans` 继续保留 Markdown daily plan 草稿。

### Removed

- `GET /api/plans`
- `POST /api/plans`
- `GET /api/plans/candidates`
- `POST /api/plans/{id}/items`
- `PATCH /api/plan-items/{id}`
- `DELETE /api/plan-items/{id}`
- `POST /api/plan-items/{id}/start-focus`
- Focus request/response 中的 `plan_item_id`。
- Complete focus request 中的 `plan_item_status_after_complete`。
- Time entry response 中的 `plan_item_id`。

## Frontend

前端保留：

- Issue 创建和详情中的 issue type 字段。
- Focus 页面已有执行交互。
- My Time / Daily review / Daily plan Markdown 入口。

前端删除：

- Plan 页面。
- Plan 主导航项。
- Plan hooks、types、query keys 和 API client 方法。
- Plan E2E。

## Verification

必须验证：

- `pnpm typecheck`
- `cd server && go test ./...`
- 全局搜索运行时代码中不再存在 `PlanItem`、`plan_item_id`、`/api/plans`、`/api/plan-items`、`features/plan`。
- 如果执行完整检查，使用 `make check`。

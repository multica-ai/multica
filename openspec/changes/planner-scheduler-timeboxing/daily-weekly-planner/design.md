# Daily Weekly Planner Design

## 目标

把现有明日计划 Markdown 面板升级为结构化日/周计划器，让用户可以选择当天/本周要完成的 issue，排序、估算、确认，并为 timeboxing 提供 plan item 输入。

## 非目标

- 不实现完整 recurring planning。
- 不实现团队共享 sprint planner。
- 不实现外部 calendar import。
- 不直接实现 timebox 拖拽；该能力归入 `timeboxing-foundation`。
- 不移除现有 Markdown draft。

## 当前架构基线

- Daily plan 表和服务已经存在。
- My Time 页面已有 DailyPlanPanel。
- Issues 已有 priority、dates、project、assignee。
- Daily review 已存在，可作为计划生成上下文。

### ASCII 图

Current:

```text
open issues + previous review
          |
          v
DailyPlanService.GeneratePlanDraft
          |
          v
daily_plan.draft_content  -----> DailyPlanPanel
daily_plan.top_issue_ids         Markdown only
```

Target:

```text
open issues + previous review
          |
          v
DailyPlanService.GeneratePlanDraft
          |
          +--> daily_plan.draft_content  (summary / compatibility)
          |
          +--> daily_plan_item[]
                    |
                    +--> Daily Planner list
                    +--> Weekly Planner summary
                    +--> Schedule timebox
```

Planner navigation:

```text
/planner/week
   |
   +-- Mon summary -> /planner?date=YYYY-MM-DD
   +-- Tue summary -> /planner?date=YYYY-MM-DD
   +-- ...

/planner
   |
   +-- planned items
   +-- candidate issues
   +-- planned minutes
   +-- confirm plan
```

## 缺口定义

现有 daily plan 是“文本建议”，不是“可执行计划”。缺少：

1. 结构化 plan item。
2. 手动选择和排序。
3. 日计划页面。
4. 周计划概览。
5. 与 timebox 的连接点。

## 方案与权衡

### 方案 A：把 Markdown 解析成结构化 item

优点：改动小。缺点：解析不稳定，LLM 输出格式会成为数据契约。

### 方案 B：新增 `daily_plan_item` 表，Markdown 只作为 summary

优点：数据稳定，可编辑，可转 timebox。缺点：需要迁移和接口。

### 方案 C：只做前端本地 planner

优点：快。缺点：无法跨设备、无法与 AI/统计/timeboxing 结合。

## 推荐方案

采用方案 B。

新增 `daily_plan_item`，并让 `DailyPlanService.GeneratePlanDraft` 在生成 Markdown 的同时，从 top issues 生成 plan items。Markdown 保留为摘要，plan items 成为 planner UI 的主要数据。

## 数据模型或状态模型

```sql
CREATE TABLE daily_plan_item (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  daily_plan_id UUID NOT NULL REFERENCES daily_plan(id) ON DELETE CASCADE,
  issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
  title_snapshot TEXT NOT NULL,
  position INTEGER NOT NULL,
  planned_minutes INTEGER,
  status TEXT NOT NULL DEFAULT 'planned',
  notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (daily_plan_id, issue_id)
);
```

`status` allowed values:

- `planned`
- `done`
- `skipped`

前端类型：

```ts
interface DailyPlanItem {
  id: string;
  daily_plan_id: string;
  issue_id: string | null;
  title_snapshot: string;
  position: number;
  planned_minutes: number | null;
  status: "planned" | "done" | "skipped";
  notes: string;
}
```

## 接口契约

- `GET /api/daily-plans/today`
- `GET /api/daily-plans/by-date?date=YYYY-MM-DD`
- `GET /api/daily-plans/week?week_start=YYYY-MM-DD`
- `POST /api/daily-plans/{id}/items`
- `PATCH /api/daily-plan-items/{id}`
- `DELETE /api/daily-plan-items/{id}`
- `POST /api/daily-plans/{id}/items/reorder`

响应中的 `DailyPlan` 应包含 `items: DailyPlanItem[]`。

## UI 或交互流程

### Daily Planner

- 路由：`/planner`。
- 默认打开今天。
- 左侧：当天 plan items。
- 右侧：未计划候选 issue，按 priority / deadline 排序。
- 支持：
  - 添加 issue 到计划。
  - 拖拽排序。
  - 设置 planned minutes。
  - 标记 skipped。
  - 跳转 issue detail。
  - 生成/重新生成 AI summary。

### Weekly Planner

- 路由：`/planner/week`。
- 显示周一到周日列。
- 每列展示 daily plan items、planned total、deadline count。
- 第一版只支持查看和从周视图跳到某日，不做跨日拖拽。

## 权限、边界条件、异常路径

- plan 属于当前 user。
- workspace member 只能读写自己的 plan。
- issue 不存在时，保留 title snapshot。
- issue 已归档时，item 保留但显示 archived 标记。
- 删除 daily plan 时 cascade 删除 item。

## 实现约束

- 不从 Markdown 解析 item。
- 生成 AI 计划时，如 item 已被用户编辑，不能盲目覆盖；应提供 regenerate 行为，并明确会重置 draft 和 top generated items。
- Reorder 必须一次性提交 position 列表。
- Weekly planner 第一版通过 daily plans 聚合，不新增 weekly table。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| AI regenerate 覆盖用户计划 | UI 明确提示，并只在用户确认后覆盖 generated items |
| weekly planner 过早复杂化 | 第一版只做周概览和跳转，不做跨日拖拽 |
| plan item 与 issue status 双向联动混乱 | 第一版 item status 独立，issue done 可在 UI 提示 |
| nullable issue 造成 UI 空洞 | 使用 title snapshot 保底展示 |

## 验收检查

- 用户可以打开 `/planner` 查看今天计划。
- 用户可以添加 issue 到当天计划。
- 用户可以设置 planned minutes 并排序。
- 用户可以确认计划。
- `/planner/week` 能展示一周 daily plan 摘要。
- 旧 daily plan 的 Markdown 仍能展示。

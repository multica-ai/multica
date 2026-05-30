# 3.1 项目管理调研

## 1. 调研目标

- 盘点现有项目页、项目查询与项目模型。
- 明确颜色、已完成/隐藏视图与统计增强的缺口。

## 2. 现状链路

1. `apps/workspace/src/features/projects/components/projects-page.tsx` · `ProjectsPage` 已支持项目列表、创建、编辑、删除、状态筛选、时间统计卡片。
2. `apps/workspace/src/features/projects/queries.ts` · `useProjectsQuery` / `useProjectTimeStatsQuery` 已提供项目列表与时间统计读取。
3. `server/pkg/db/queries/project.sql` · `ListProjects` / `CreateProject` / `UpdateProject` / `DeleteProject` / `ProjectTimeStats` 提供项目后端主链路。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/project.ts` · `Project` | 项目类型只有 `id`、`name`、`description`、`status`、时间戳，没有颜色与隐藏字段。 |
| `apps/workspace/src/features/projects/components/projects-page.tsx` · `ProjectsPage` | UI 已有状态筛选与时间统计区，但没有颜色选择、隐藏切换、已完成/隐藏视图切换。 |
| `server/pkg/db/queries/project.sql` · `ListProjects` | 服务端当前只能按 `status` 过滤，没有隐藏维度。 |
| `server/pkg/db/queries/project.sql` · `ProjectTimeStats` | 时间统计已经存在，说明增强重点是口径和展示，而不是从零实现统计。 |

## 4. 数据模型或状态流

- 项目当前工作流是 create/update/delete/list/status-filter。
- 项目时间统计已是独立查询，不依附于项目列表响应。

## 5. 边界条件

- 删除项目会影响 issue 的 `project_id` 归属，因此“隐藏项目”不能复用删除。
- 项目颜色必须持久化，否则看板、列表和移动端无法保持一致。

## 6. 未决问题

1. 已完成项目是继续复用 `status=done`，还是另设 completed 视图谓词。
2. 隐藏项目是否对 issue 创建/编辑的项目选择器可见。

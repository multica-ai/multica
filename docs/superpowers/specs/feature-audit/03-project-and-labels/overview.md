# 项目与标签模块总览

## 1. 目标与范围

- 模块范围覆盖 `docs/superpowers/specs/feature-audit/03-project-and-labels/`。
- 本轮补齐 3.1 项目管理设计包，并完成 3.2 标签管理里的“按标签筛选任务”实现与回写。
- 3.2 已不再只是设计引用项，本总览需要同步记录其实现完成状态。

## 2. 能力列表

| 能力 | 当前状态 | 本轮动作 | 依赖 |
| --- | --- | --- | --- |
| 3.1 项目管理 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 1.x issue 查询与统计口径 |
| 3.2 标签管理 | 已完成 | 已实现标签筛选任务，并回写 spec/design/overview | 1.4、1.5 |

## 3. 当前状态证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/projects/components/projects-page.tsx` · `ProjectsPage` | 当前项目页已支持列表、创建、编辑、删除、状态筛选与时间统计展示。 |
| `apps/workspace/src/shared/types/project.ts` · `Project` | 项目模型已有 `status`，但没有 `color`、`hidden_at` 等字段。 |
| `server/pkg/db/queries/project.sql` · `ListProjects` / `ProjectTimeStats` | 后端已有项目列表与时间统计查询，但没有颜色、隐藏和已完成/隐藏视图维度。 |
| `apps/workspace/src/features/issues/components/issue-label-filter.tsx` · `IssueLabelFilter` | 主任务列表已新增标签筛选弹层，支持多标签选择、搜索和 `any/all` 模式切换。 |
| `server/internal/handler/issue.go` · `ListIssues` | `GET /api/issues` 已支持 `label_ids` 和 `label_match_mode`，并对非法参数返回 400。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` / `CountListedIssues` | issue 列表与 total 已共享同一套标签谓词，支持 `any` / `all`。 |

## 4. 非目标

- 本轮不重写 3.2 的研究与任务拆分。
- 本轮不扩到 portfolio 报表、项目权限或跨工作区模板。

## 5. 优先级与推进顺序

1. **先做 3.1 的项目字段补齐**：颜色和隐藏能力都要求先有 schema 字段。
2. **再做 3.1 的视图与统计**：已完成 / 隐藏项目视图依赖新的项目状态过滤。
3. **最后联动 3.2**：项目页中的标签联动仍直接引用 3.2，不反向改写其设计。

## 6. 共享约束

- `Project` 仍是 issue 的上层容器，不能和 1.x 的任务视图重复建模。
- 项目统计默认只统计未归档 issue；原因见 `01-task-management/1.1-basic-operations/design.md` 的归档生命周期约束。
- 3.2 的标签筛选已落地，3.1 若后续需要项目侧标签联动，应直接复用这条服务端过滤路径。

## 7. 风险与依赖

| 主题 | 风险 | 依赖 / 对策 |
| --- | --- | --- |
| 颜色 | 若仅前端存颜色，会导致跨端不一致。 | 在 project schema 中落字段。 |
| 隐藏项目 | 隐藏若与删除混淆，会破坏 issue 归属。 | 独立 `hidden_at` 语义，不复用删除。 |
| 统计 | 项目统计若不遵循 issue 生命周期，会把已归档任务算进去。 | 复用 1.1 的默认过滤口径。 |

## 8. 回写规则

1. 3.1 若修改项目统计口径，必须同时回写本 `overview.md`。
2. 3.2 后续若扩展到其他 issue 视图并调整标签筛选契约，需要同步更新本总览中的依赖说明。

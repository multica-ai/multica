# 3.2 标签管理设计包

## 目的

这个目录在原有审计 `spec.md` 之外，补一套可直接落地的设计文档，专门覆盖当前缺口“按标签筛选任务”。

当前结论已经明确：

- 标签 CRUD 已完成，来源：`apps/workspace/src/features/settings/components/labels-tab.tsx:LabelsTab`
- 任务多标签绑定已完成，来源：`apps/workspace/src/features/issues/components/pickers/label-picker.tsx:LabelPicker`
- 主任务列表还没有标签筛选状态、后端查询参数和筛选入口，来源：`apps/workspace/src/features/issues/stores/view-store.ts:viewStoreSlice`、`server/internal/handler/issue.go:ListIssues`、`server/pkg/db/queries/issue.sql:ListIssues`、`apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListFiltersRow`

## 文档索引

| 文档 | 类型 | 作用 |
| --- | --- | --- |
| `spec.md` | 审计台账 | 维护当前完成度、证据、缺口和交接信息 |
| `research.md` | 现状调研 | 固化代码证据、边界条件和已确认问题 |
| `design.md` | 方案设计 | 说明目标、候选方案、推荐方案、状态模型和 UI 方案 |
| `tasks.md` | 执行切片 | 将设计拆成可实施的文件级任务与验收检查 |

## 设计冻结结论

1. 后端要把标签筛选做成 `GET /api/issues` 的一等过滤能力，而不是只在前端做本地过滤。
2. 在现有 issue view-store 上新增 `labelFilters` 与 `labelFilterMode`，由前端状态驱动服务端查询参数。
3. 标签筛选默认采用 `any` 语义，并允许切换到 `all`。
4. 标签筛选入口放在 `IssueListFiltersRow` 的搜索与日期筛选同一行，活跃筛选通过 chip 展示并可单独移除。
5. 前端不再对标签重复执行第二遍本地过滤，避免和服务端语义漂移。

## 交接说明

- 如果后续进入实现阶段，应以 `design.md` 和 `tasks.md` 为准，而不是重新从 plan 反推。
- 如果后续发现任务管理模块 `1.4 任务筛选与排序` 的审计结论需要和这里对齐，应单独做一次审计修正，不要在实现阶段顺手改动统计口径。

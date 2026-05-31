# 3.2 标签管理缺口调研

## 调研目标

确认“按标签筛选任务”当前缺在哪里，缺口属于状态层、后端查询层、UI 层，还是契约层。

## 当前已完成能力

### 1. 标签 CRUD 已具备

- `apps/workspace/src/features/settings/components/labels-tab.tsx:LabelsTab` 已提供标签创建、编辑、删除和颜色选择。这里说明标签实体与基础管理界面已经完整存在。

### 2. 任务多标签绑定已具备

- `apps/workspace/src/features/issues/components/pickers/label-picker.tsx:LabelPicker` 已通过 `useWorkspaceLabelsQuery()` 读取工作区标签，并支持给 issue 添加、移除、创建和编辑标签。
- `apps/workspace/src/shared/types/issue.ts:Issue` 明确包含 `labels?: IssueLabel[]` 字段，说明 issue 列表数据本身已经携带标签信息。

### 3. 工作区标签数据源已具备

- `apps/workspace/src/features/issues/queries.ts:useWorkspaceLabelsQuery` 已封装标签查询缓存，适合作为筛选器选项来源。

## 当前缺口证据

### 1. view-store 没有标签筛选状态

- `apps/workspace/src/features/issues/stores/view-store.ts:IssueViewState` 当前只包含 `statusFilters`、`priorityFilters`、`assigneeFilters`、`includeNoAssignee`、`creatorFilters`，没有 `labelFilters` 或 `labelFilterMode`。
- `apps/workspace/src/features/issues/stores/view-store.ts:viewStoreSlice` 也没有对应的 toggle、clear、persist 字段。这意味着标签筛选既无法驱动 UI，也无法参与工作区切换后的状态管理。

### 2. 后端 issue 列表查询没有标签过滤能力

- `server/internal/handler/issue.go:ListIssues` 当前只解析 `status`、`priority`、`assignee_id`、`creator_id`、`project_id`、`search`、日期范围和 `view`，没有标签参数解析。
- `server/pkg/db/queries/issue.sql:ListIssues` 与 `server/pkg/db/queries/issue.sql:CountListedIssues` 当前都只对 `issue` 主表字段做过滤，没有关联 `issue_to_label`。
- `apps/workspace/src/shared/types/api.ts:ListIssuesParams` 没有 `label_ids` 或 `label_match_mode` 字段。
- `apps/workspace/src/shared/api/client.ts:ApiClient.listIssues` 也没有序列化任何标签筛选参数。

### 3. issue 标签关系已经存在，后端过滤有可依托的数据模型

- `server/pkg/db/queries/label.sql:AddIssueLabel` 和 `server/pkg/db/queries/label.sql:RemoveIssueLabel` 说明 issue 与标签关系已经落在 `issue_to_label`。
- `server/pkg/db/queries/label.sql:ListIssueLabels` 说明后端已经通过 `issue_to_label + issue_label` 读取 issue 的标签详情。
- `server/migrations/001_init.up.sql` 已建 `issue_label` 与 `issue_to_label` 表，因此这不是“先补 schema，再谈查询”的问题。

### 4. 主任务列表没有标签筛选入口

- `apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListFiltersRow` 当前渲染搜索、项目筛选和日期筛选，没有标签筛选控件。
- `apps/workspace/src/features/issues/components/issue-list-page.tsx:FilterChip` 当前只渲染状态、优先级、负责人、创建者和日期 chip，没有标签 chip。
- `apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListPageContent` 当前构造的 `queryParams` 不包含标签字段。

## 约束

1. 复用现有 `useWorkspaceLabelsQuery()`，不要重复发明标签选项来源。
2. 复用现有 `view-store` 持久化机制，避免把标签筛选塞进组件局部状态。
3. 标签筛选要走 `ListIssues` 查询参数主路径，不再追加一层独立前端过滤。
4. 保持与现有筛选体验一致，筛选结果必须通过活跃 chip 可见、可移除、可清空。

## 已确认设计输入

### 1. 标签筛选必须同时覆盖状态层和后端查询层

- 现有 issue 列表已经把搜索、项目和日期筛选发送到后端，来源：`apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListPageContent` 与 `apps/workspace/src/shared/api/client.ts:ApiClient.listIssues`。标签筛选如果停留在前端，会让 issue list 形成一条新的例外路径。

### 2. 需要支持多标签语义

- 既有计划 `docs/superpowers/plans/2026-05-24-project-and-label-gaps.md` 为标签筛选预留了 `labelFilterMode: "any" | "all"`。这比单纯的“多选即 OR”更完整，且与多标签绑定场景更匹配。

## 调研结论

“按标签筛选任务”当前缺的是一整条前后端联动链路：

1. 缺少可持久化的标签筛选状态。
2. 缺少 `ListIssues` / `CountListedIssues` 的标签过滤契约与 SQL 实现。
3. 缺少主任务列表上的筛选入口和活跃反馈。

因此应补的是一套前后端一体的设计包，而不是只补某个按钮或只补一段前端过滤函数。

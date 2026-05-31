# 任务管理模块总览

## 1. 目标与范围

- 模块范围覆盖 `docs/superpowers/specs/feature-audit/01-task-management/` 下的 1.1~1.7。
- 本轮目标是为所有未完成二级能力补齐 `research.md`、`design.md`、`tasks.md`，并把它们组织成可直接交给执行 Agent 的实现合同。
- 本轮不改代码、不重写既有 `spec.md`，只在总览中补共享约束、优先级、风险、依赖和回写要求。

## 2. 能力列表

| 能力 | 当前状态 | 本轮文档动作 | 依赖 |
| --- | --- | --- | --- |
| 1.1 基础任务操作 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 无 |
| 1.2 任务属性配置 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 1.7 的重复规则模型 |
| 1.3 任务视图 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 1.1 的归档生命周期 |
| 1.4 筛选与排序 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 3.2 标签筛选设计包 |
| 1.5 批量操作 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 1.1、3.2 |
| 1.6 子任务与层级 | 部分完成 | 新增 `research.md` / `design.md` / `tasks.md` | 无 |
| 1.7 重复任务 | 缺失 | 新增 `research.md` / `design.md` / `tasks.md` | 1.1、1.2 |

## 3. 当前状态证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | 当前 issue 模型已有 `status`、`priority`、`project_id`、`parent_issue_id`、`child_issues`、`attachments`，但没有 `archived_at`、`estimated_minutes`、`recurrence_*` 等字段。 |
| `apps/workspace/src/features/issues/components/issue-detail.tsx` · `IssueDetail` | 详情页已经覆盖标题、描述、优先级、项目、标签、截止日期、父任务和附件区，是 1.1、1.2、1.6 扩展的主入口。 |
| `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent` | 主任务列表已经把搜索、项目、日期过滤放在服务端查询参数里，并在前端补充视图状态，是 1.3、1.4 的现有主链路。 |
| `apps/workspace/src/features/issues/stores/view-store.ts` · `IssueViewState` | 本地筛选/排序状态当前只覆盖 `status`、`priority`、`assignee`、`creator` 和 `sortBy`，没有层级、归档、重复规则上下文。 |
| `apps/workspace/src/features/issues/components/batch-action-toolbar.tsx` · `BatchActionToolbar` | 批量工具栏已经支持状态、优先级、负责人和删除，但没有项目迁移、标签增删、归档。 |
| `server/pkg/db/queries/issue.sql` · `ListIssues` / `CountListedIssues` / `DeleteIssue` | 服务端 issue 主查询没有归档谓词，删除仍是物理删除，因此 1.1、1.3、1.5 的“已归档”能力都缺后端基线。 |

## 4. 非目标

- 本模块本轮不扩展到 dashboard、saved views、跨工作区协作或通知系统。
- 本模块本轮不把全部任务筛选一次性重构为“全后端过滤”；1.4 只定义当前阶段的筛选归属和剩余缺口。
- 本模块本轮不重新定义标签设计包；标签筛选与标签批量操作直接引用 `../03-project-and-labels/3.2-label-management/`。

## 5. 优先级与推进顺序

1. **先做 1.1 归档生命周期**：`server/pkg/db/queries/issue.sql:DeleteIssue` 仍是物理删除，未先补归档就无法稳定落 1.3 已归档视图和 1.5 批量归档。
2. **再做 1.4 归属收敛**：`apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListPageContent` 与 `apps/workspace/src/features/issues/stores/view-store.ts:IssueViewState` 现在已经分担筛选责任，必须先锁定“谁负责哪类过滤”，避免后续文档互相打架。
3. **并行准备 1.2 / 1.5 / 1.6**：这三项都建立在现有 issue 模型之上，可共用同一批 schema 与 UI 入口约束。
4. **最后落 1.7**：重复任务必须建立在“issue 仍是执行实例、归档不是完成状态”的前提上，否则会污染现有状态机。
5. **1.3 视图分两阶段**：先补已完成/已归档视图，再补四象限/时间轴；原因是前者依赖真实生命周期，后者依赖展示组合，不应反向驱动底层模型。

## 6. 共享约束

### 6.1 issue 仍是任务主模型

- `apps/workspace/src/shared/types/issue.ts` · `Issue` 说明任务、子任务、标签、项目、附件都已围绕 issue 组织；1.1~1.7 的设计都必须在 issue 模型上渐进扩展，不能再起第二套任务实体。

### 6.2 1.4 筛选归属必须明确

- `apps/workspace/src/features/issues/components/issue-list-page.tsx` · `IssueListPageContent` 当前把搜索、项目、日期发往服务端。
- `apps/workspace/src/features/issues/stores/view-store.ts` · `IssueViewState` 当前把状态、优先级、负责人、创建者和排序保存在本地。
- `docs/superpowers/specs/feature-audit/03-project-and-labels/3.2-label-management/design.md` · `推荐方案` 已经把标签筛选定义为服务端过滤。
- 结论：1.4 本轮必须把“搜索/项目/日期/标签走服务端；状态/优先级/负责人/创建者/排序走前端”的边界写死。

### 6.3 1.6 层级只允许树，不允许 DAG

- `apps/workspace/src/shared/types/issue.ts` · `Issue.parent_issue_id` 与 `Issue.child_issues` 说明当前模型天然是单父子树。
- `apps/workspace/src/features/issues/components/issue-detail.tsx` · `ParentIssuePicker` 也只支持选择一个父任务。
- 结论：1.6 只能继续增强树形层级，不应扩成多父依赖图；跨任务依赖属于未来单独能力。

### 6.4 1.7 重复任务不能重载 issue.status

- `apps/workspace/src/shared/types/issue.ts` · `IssueStatus` 当前已经承担 `todo` / `in_progress` / `done` / `cancelled` / `backlog`。
- `server/pkg/db/queries/issue.sql` · `ListIssues` 直接按 `status` 做列表过滤。
- 结论：重复规则必须是独立于 `status` 的系列模型，不能把“重复中”“待生成”等语义塞进现有状态枚举。

## 7. 风险与依赖

| 主题 | 风险 | 依赖 / 对策 |
| --- | --- | --- |
| 归档 | 归档若直接复用删除，会导致已归档视图和恢复能力失真。 | 先实现 1.1 的独立归档字段与 API，再接 1.3、1.5。 |
| 标签 | 1.4 和 1.5 如果各自再定义标签语义，会与 3.2 冲突。 | 统一引用 `3.2-label-management/design.md` 的服务端标签过滤与批量标签操作语义。 |
| 层级 | 1.6 若直接在 UI 做树形假象，但查询仍是平铺列表，会出现排序和统计漂移。 | 先补服务端递归读模型，再补交互。 |
| 重复任务 | 1.7 若直接复制 issue 而没有系列边界，后续“改单次/改全部”无法成立。 | 先定义系列/实例契约，再设计生成时机。 |

## 8. 回写规则

1. 任一能力进入实现前，执行 Agent 必须同时读取对应目录下的 `design.md` 与 `tasks.md`。
2. 任一能力实现完成后，先回写该能力 `spec.md` 的完成状态，再回写本 `overview.md` 的状态、优先级和依赖说明。
3. 如果 1.1、1.4、1.7 的实现改变了共享约束，必须先更新本 `overview.md`，再继续后续能力实现。

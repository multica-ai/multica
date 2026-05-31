# 3.2 标签管理

## 范围

- 一级模块：项目与标签管理
- 二级能力：3.2 标签管理
- 清单来源：`docs/功能列表清单.md:114-120`

## 对照清单

- 已完成：新建标签
- 已完成：编辑标签
- 已完成：删除标签
- 已完成：标签颜色自定义
- 已完成：任务多标签绑定
- 已完成：按标签筛选任务

## 当前状态

- 状态：已完成
- 完成度：6 / 6

## 证据

- `apps/workspace/src/features/settings/components/labels-tab.tsx:LabelsTab`：标签 CRUD 和颜色自定义已经在 settings 页落地。
- `apps/workspace/src/features/issues/components/pickers/label-picker.tsx:LabelPicker`：issue 详情已支持多标签绑定、复用标签和新建标签。
- `apps/workspace/src/features/issues/queries.ts:useWorkspaceLabelsQuery`：工作区级标签列表已封装成可复用查询。
- `apps/workspace/src/features/issues/stores/view-store.ts:viewStoreSlice`：issue 视图状态已新增 `labelFilters`、`labelFilterMode` 和清空逻辑，并纳入持久化。
- `apps/workspace/src/features/issues/components/issue-label-filter.tsx:IssueLabelFilter`：主任务列表已新增标签筛选弹层，支持标签搜索、多选和 `any/all` 模式切换。
- `apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListFiltersRow`：主任务列表已接入标签筛选入口、活跃标签 chips 和模式 chip。
- `apps/workspace/src/shared/api/client.ts:ApiClient.listIssues`：客户端已把 `label_ids` 和 `label_match_mode` 发送到 `/api/issues`。
- `server/internal/handler/issue.go:ListIssues`：issue 列表 handler 已解析、校验并去重标签筛选参数。
- `server/pkg/db/queries/issue.sql:ListIssues`：issue 列表 SQL 已通过 `issue_to_label` 支持 `any/all` 标签过滤，`CountListedIssues` 复用了相同谓词。
- `server/internal/handler/handler_test.go:TestListIssuesLabelFilters`：后端已覆盖 `any`、`all` 和非法参数场景。

## 缺口

- 无阻断缺口。
- 后续若要把标签筛选扩到 board、today、upcoming 等其他视图，应另开能力设计，不在本能力内追加兼容层。

## 推荐实现切片

- 已完成：采用“view-store 驱动服务端查询”的模型，没有引入前后端双过滤。
- 已完成：`ListIssuesParams`、`ApiClient.listIssues`、`Handler.ListIssues`、`issue.sql` 已补齐 `label_ids + label_match_mode(any|all)`。
- 已完成：设计文档、实现代码和 handler 测试已对齐。

## 交接说明

- 当前能力已可进入后续审计或扩展阶段，无需再补本轮实现。
- 如果后续新增跨视图标签筛选，请沿用本设计中的服务端单一路径约束。

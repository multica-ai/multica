# 3.2 标签管理执行切片

## 目标

把“按标签筛选任务”拆成可直接实施的前后端切片，保证每一步都对应明确文件和验收点。

## 切片 1：扩展后端查询契约

### 涉及文件

- `server/pkg/db/queries/issue.sql`
- `server/pkg/db/generated/issue.sql.go`
- `server/pkg/db/generated/models.go`
- `server/internal/handler/issue.go`
- `server/internal/handler/handler_test.go` 或专门的 issue handler 测试

### 动作

1. 先补失败测试，覆盖单标签、`any`、`all`、空标签 issue、不同行为空间标签五类场景。
2. 在 `ListIssues` / `CountListedIssues` 中增加标签过滤谓词。
3. 在 `Handler.ListIssues` 中解析 `label_ids` 与 `label_match_mode`。
4. 生成 sqlc 代码并让 handler 把参数传透。

### 完成标准

- 后端单独调用 `GET /api/issues` 就能返回标签过滤后的结果和正确的 `total`。

## 切片 2：扩展前端 API 类型与 client

### 涉及文件

- `apps/workspace/src/shared/types/api.ts`
- `apps/workspace/src/shared/api/client.ts`

### 动作

1. 在 `ListIssuesParams` 中新增 `label_ids` 与 `label_match_mode`。
2. 在 `ApiClient.listIssues()` 中序列化重复 query key `label_ids`。
3. 保证没有选中标签时不发送空数组参数。

### 完成标准

- 前端能稳定把标签筛选状态映射成 HTTP 查询参数。

## 切片 3：扩展筛选状态模型

### 涉及文件

- `apps/workspace/src/features/issues/stores/view-store.ts`

### 动作

1. 在 `IssueViewState` 中新增 `labelFilters`、`labelFilterMode`、`toggleLabelFilter`、`setLabelFilterMode`、`clearLabelFilters`。
2. 在 `viewStoreSlice` 中补默认值和对应 action。
3. 在 `clearFilters()` 中纳入标签字段。
4. 在 `viewStorePersistOptions().partialize` 中纳入标签字段。

### 完成标准

- 标签筛选状态和现有状态、优先级筛选一样受持久化管理。

## 切片 4：新增标签筛选组件

### 涉及文件

- `apps/workspace/src/features/issues/components/issue-label-filter.tsx`
- `apps/workspace/src/features/issues/components/pickers/label-picker.tsx`

### 动作

1. 新建受控组件 `IssueLabelFilter`。
2. 复用 `useWorkspaceLabelsQuery()` 获取标签选项。
3. 复用 `LabelPicker` 里的“已选优先 + 名称排序”处理方式，避免不同标签组件出现两套排序规则。
4. 在组件内部提供搜索、多选、模式切换和局部清空动作。

### 完成标准

- 组件本身不持有业务筛选状态，只消费外部传入的 `selectedIds` 和 `mode`。

## 切片 5：接入主任务列表页面

### 涉及文件

- `apps/workspace/src/features/issues/components/issue-list-page.tsx`

### 动作

1. 在 `IssueListFiltersRow` 中接入 `IssueLabelFilter`。
2. 在 `IssueListPageContent` 中读取 `labelFilters` 和 `labelFilterMode` 并传给 `queryParams`。
3. 在活跃 chip 区域增加标签 chip 和模式 chip。
4. 让 `Clear all` 覆盖标签筛选。

### 完成标准

- 标签筛选与现有搜索、项目、日期筛选一起工作，不新增第二套“局部过滤结果”状态。

## 切片 6：回写审计台账

### 涉及文件

- `docs/superpowers/specs/feature-audit/03-project-and-labels/3.2-label-management/spec.md`
- `docs/superpowers/specs/feature-audit/03-project-and-labels/overview.md`

### 动作

1. 实现完成后回写证据路径和函数名。
2. 将 `按标签筛选任务` 从“缺失”更新为“已完成”。
3. 同步模块完成度与 handoff。

### 完成标准

- 审计台账和真实代码状态一致，不再停留在“有设计、无回写”的中间状态。

## 建议验证顺序

1. `cd server && go test ./internal/handler -run TestIssue`
2. `make sqlc`
3. `pnpm typecheck`
4. 如补了页面测试，再运行对应测试文件

## 不要做的事

1. 不要只做前端本地标签过滤然后把后端保持空白。
2. 不要在 `issue-list-page.tsx` 内联写一套标签过滤逻辑。
3. 不要让 SQL 和前端各自维护一套互不校验的 `any` / `all` 语义。

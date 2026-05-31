# 3.2 标签管理设计

## 状态

已实现。本文档记录“按标签筛选任务”的最终设计，范围覆盖工作区主任务列表、`GET /api/issues` 过滤契约，以及对应 SQL 语义。

## 目标

1. 在 `/issues` 主任务列表上提供标签筛选入口。
2. 支持选择多个标签，并在 `any` / `all` 两种匹配语义间切换。
3. 让标签筛选纳入现有 `view-store` 和 `ListIssues` 查询主路径。
4. 让活跃标签筛选像现有其他筛选一样可见、可移除、可清空。

## 非目标

1. 不把全部筛选体系一次性重构成“全后端过滤”。
2. 不在 board、backlog、today、upcoming 页面同步补齐标签筛选。
3. 不引入保存视图或跨设备同步语义，这属于更上层的 saved views 设计。

## 当前架构基线

- `apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListPageContent` 负责组合服务端查询参数与客户端过滤。
- `apps/workspace/src/features/issues/stores/view-store.ts:viewStoreSlice` 是 issue 视图筛选状态的唯一来源。
- `server/internal/handler/issue.go:ListIssues` 是 issue 列表过滤的服务端入口。
- `server/pkg/db/queries/issue.sql:ListIssues` 与 `CountListedIssues` 是 issue 列表过滤的 SQL 入口。
- `apps/workspace/src/features/issues/queries.ts:useWorkspaceLabelsQuery` 是工作区标签选项缓存。

当前设计必须贴着这条主路径扩展，不能新起第二套状态和第二套标签匹配语义。

## 候选方案

### 方案 A：服务端标签过滤优先，推荐

在 `ListIssuesParams`、API client、后端 handler 和 SQL 中新增标签过滤参数，前端把标签选择转成接口查询；标签筛选不再进入 `filterIssues()`。

优点：

- 数据量更大时更可扩展。
- 过滤结果由服务端统一计算。

缺点：

- 需要同时改 shared types、client、handler、SQL 和测试。
- 会出现“标签走后端，其他维度仍有前端过滤”的阶段性混合模型。

### 方案 B：前端本地过滤扩展

在现有 `IssueViewState` 中增加标签筛选状态，在 `filterIssues()` 中基于 `issue.labels` 执行匹配，再在 `IssueListFiltersRow` 增加筛选 UI。

优点：

- 改动集中在前端。
- 可以直接复用 `useWorkspaceLabelsQuery()` 和 `Issue.labels`。

缺点：

- 和搜索、项目、日期这几类服务端过滤形成分裂语义。
- issue 总量升高后，标签筛选会继续受客户端载入规模影响。
- 无法满足当前“后端也要支持过滤”的设计要求。

### 方案 C：前后端双过滤兜底

前端既把标签状态发给后端，也在 `filterIssues()` 再做一次同语义过滤。

优点：

- 理论上能容忍个别后端漏判。

缺点：

- 服务端和前端需要长期维护两套相同语义。
- 一旦 `any` / `all` 或空标签边界条件不一致，就会出现难排查的结果漂移。
- 纯属维护成本，不是合理默认路径。

## 推荐方案

选择方案 A。

这项缺口本质上已经不是“前端少一个筛选按钮”，而是 issue list 的服务端过滤契约缺了一维。搜索、项目和日期都已经是服务端过滤，标签继续留在本地只会让同一页面存在两种完全不同的筛选归属。更稳的方案是让后端成为标签匹配的唯一真相源，前端只负责状态、请求和反馈。

## 状态模型

建议在 `apps/workspace/src/features/issues/stores/view-store.ts:IssueViewState` 中新增：

```ts
labelFilters: string[];
labelFilterMode: "any" | "all";
toggleLabelFilter: (labelId: string) => void;
setLabelFilterMode: (mode: "any" | "all") => void;
clearLabelFilters: () => void;
```

### 语义

1. `labelFilters` 为空，表示未启用标签筛选。
2. `labelFilterMode = "any"` 时，issue 只要命中任一已选标签即可显示。
3. `labelFilterMode = "all"` 时，issue 必须同时拥有全部已选标签才显示。
4. `clearFilters()` 必须同时清空 `labelFilters` 并恢复 `labelFilterMode = "any"`，避免旧模式残留到下一次查询。

## API 契约

建议在 `apps/workspace/src/shared/types/api.ts:ListIssuesParams` 中新增：

```ts
label_ids?: string[];
label_match_mode?: "any" | "all";
```

建议 HTTP 查询形态为：

```text
GET /api/issues?label_ids=<uuid1>&label_ids=<uuid2>&label_match_mode=all
```

原因：

1. `label_ids` 天然是多值字段，重复 query key 比手写逗号分隔更稳定。
2. `URLSearchParams.append()` 可以直接生成这一形态。
3. `r.URL.Query()["label_ids"]` 能直接拿到数组，不需要额外字符串切分。

### 请求校验规则

1. `label_ids` 未传、为空，或只包含空字符串时，不启用标签过滤，同时忽略 `label_match_mode`。
2. 传入 `label_ids` 但未传 `label_match_mode` 时，服务端默认按 `any` 处理。
3. `label_match_mode` 只接受 `any` 和 `all`，其他值返回 `400 Bad Request`。
4. `label_ids` 中任一值不是合法 UUID 时，返回 `400 Bad Request`。
5. 服务端在执行 `all` 匹配前先对 `label_ids` 去重，避免重复参数改变语义。

## 后端过滤规则

建议在 `server/pkg/db/queries/issue.sql:ListIssues` 和 `CountListedIssues` 中新增统一标签谓词。

### 匹配语义

1. 当 `label_ids` 为空时，不参与过滤。
2. `any` 模式下，只要 issue 在 `issue_to_label` 中命中任一所选标签即可返回。
3. `all` 模式下，issue 必须命中全部所选标签才返回。
4. 如果 issue 没有标签且当前有标签筛选，则不会返回。
5. `CountListedIssues` 必须复用完全相同的标签谓词，避免总数和列表结果不一致。

### SQL 建议

1. `any` 模式使用 `EXISTS (...)`。
2. `all` 模式使用 `COUNT(DISTINCT issue_to_label.label_id) = selected_label_count`。
3. 标签过滤必须限定在当前 issue 的 `issue_id` 上，不能只按 workspace 标签命中。

## 实现结果

1. `apps/workspace/src/features/issues/stores/view-store.ts:viewStoreSlice` 已新增 `labelFilters`、`labelFilterMode` 和对应清空/切换 action，并纳入持久化。
2. `apps/workspace/src/features/issues/components/issue-label-filter.tsx:IssueLabelFilter` 已落地标签筛选入口、标签搜索、`any/all` 模式切换和清空动作。
3. `apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListFiltersRow` 已接入标签筛选入口与活跃 chips，`IssueListPageContent` 已把 `label_ids` 和 `label_match_mode` 送入 `useIssuesListQuery`。
4. `apps/workspace/src/shared/api/client.ts:ApiClient.listIssues` 已按重复 query key 序列化 `label_ids`。
5. `server/internal/handler/issue.go:ListIssues` 已完成 `label_ids` / `label_match_mode` 解析、校验和去重。
6. `server/pkg/db/queries/issue.sql:ListIssues` 与 `CountListedIssues` 已复用同一套标签匹配谓词，支持 `any` / `all`。
7. `server/internal/handler/handler_test.go:TestListIssuesLabelFilters` 已覆盖 `any`、`all`、非法 `label_match_mode` 和非法 `label_ids`。

## 前端查询接线

1. `IssueListPageContent` 从 `useViewStore()` 读取 `labelFilters` 和 `labelFilterMode`。
2. `queryParams` 在有标签筛选时带上 `label_ids` 和 `label_match_mode`。
3. `useIssuesListQuery()` 基于新 query key 自动重取列表。
4. `filterIssues()` 不再新增标签条件，避免双重过滤。

## UI 设计

## 入口位置

把标签筛选入口放在 `apps/workspace/src/features/issues/components/issue-list-page.tsx:IssueListFiltersRow` 的搜索、项目、日期同一行。原因有两个：

1. 这里已经承载了 issue list 专属筛选入口。
2. 标签筛选依赖 `useWorkspaceLabelsQuery()`，并且要直接影响 `IssueListPageContent` 的 `queryParams`。

## 交互结构

建议新增 `apps/workspace/src/features/issues/components/issue-label-filter.tsx`，职责只做三件事：

1. 渲染标签多选入口。
2. 渲染 `any` / `all` 模式切换。
3. 暴露 `selectedIds`、`mode`、`onToggle`、`onModeChange`、`onClear` 这些受控接口。

### 交互细节

1. 触发按钮文案使用 `Labels`，选中后显示计数，如 `Labels (2)`。
2. 下拉面板顶部保留搜索框，便于在大量标签里查找。
3. 已选标签置顶，其余标签按名称排序，和 `LabelPicker` 的处理保持一致。
4. 面板顶部展示模式切换控件，默认 `Match any`，可切到 `Match all`。
5. 每个标签项展示颜色、名称和勾选状态，保持与现有标签视觉一致。
6. 面板底部提供 `Clear labels` 动作，只清空标签筛选，不影响其他筛选。

## 活跃反馈

`apps/workspace/src/features/issues/components/issue-list-page.tsx:FilterChip` 需要增加标签筛选 chip：

1. 每个已选标签渲染一个独立 chip，支持单独移除。
2. 当 `labelFilterMode === "all"` 且已选标签数大于 1 时，额外渲染一个模式 chip，例如 `Match: all`，点击后恢复到 `any` 或直接清空模式。
3. `Clear all` 必须一并清空标签筛选。

这样用户能看见筛选是否来自标签，而不是只看到一次重新请求后的结果变化。

## 数据流

```text
IssueLabelFilter
    |
    v
view-store(labelFilters, labelFilterMode)
    |
    v
IssueListPageContent 构造 queryParams
    |
    v
ApiClient.listIssues(label_ids, label_match_mode)
    |
    v
Handler.ListIssues + SQL label predicate
    |
    v
返回过滤后的 issue 列表 + 活跃 chips
```

## ASCII 图补齐

### 页面交互流

```text
任务列表页
  -> 打开标签筛选面板
  -> 选择一个或多个标签
  -> 选择 any / all 模式
  -> 触发服务端重查
  -> 列表与筛选 chips 同步更新
```

### 状态机

```text
未筛选
  -> 已选择标签(any)
已选择标签(any)
  -> 已选择标签(all)
已选择标签(any/all)
  -> 清空筛选
```

### 数据变化流

```text
IssueLabelFilter
  -> view-store(labelFilters, labelFilterMode)
  -> listIssues(label_ids, label_match_mode)
  -> Handler.ListIssues + SQL predicate
  -> issue list / chips 回显
```

## 风险与对策

### 风险 1：ListIssues 和 CountListedIssues 谓词不一致

对策：两条 SQL 一起改，并在 handler / query 测试里同时覆盖列表和 total。

### 风险 2：前端状态和后端查询参数命名不一致

对策：统一使用 `labelFilters -> label_ids`、`labelFilterMode -> label_match_mode` 的一一映射，不再在中间层做第三套命名。

### 风险 3：前端又把标签塞回 `filterIssues()`，造成双重过滤

对策：把“标签过滤唯一由后端执行”写进实现约束和测试目标，避免维护两套语义。

## 验收检查

1. 选中一个标签后，只显示拥有该标签的 issue。
2. 选中多个标签且模式为 `any` 时，任一命中即可显示。
3. 选中多个标签且模式为 `all` 时，必须同时命中全部标签。
4. 列表返回条数与 `total` 在标签筛选下保持一致。
5. 点击 `Clear all` 后，标签筛选与其他筛选一起清空。
6. 刷新页面后，标签筛选状态仍能从持久化中恢复。

---
title: Issue Table Server Query and Grouping - Plan
type: feat
date: 2026-07-17
topic: issue-table-server-query
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: product-and-code-audit
execution: code
---

# Issue Table Server Query and Grouping - Plan

> 本文定义 Table View 的目标产品语义、前后端边界和分阶段实施方案。核心目标不是提高当前的 `1000` 上限，而是移除“前端必须拥有完整结果集才能 Group/Hierarchy”的架构前提。

> **Implementation status (2026-07-22):** 新 Table 路径的 canonical compiler、U2–U3、U5–U7，以及 U8 的 exact facets / cache invalidation 已落地；标准字段和默认 position keyset 顺序所需 concurrent indexes 已加入。Rows 先裁 page，再只为当前页解析 hierarchy；默认 position 续页额外提供可下推的 cursor range bound，避免深页从 workspace 索引起点线性过滤。Assignee 显示名在 actor 聚合后解析；无自身 active filter 的标准 facets 在批量请求中用 `GROUPING SETS` 共享一次 base scan。Legacy GET handlers 尚未迁入同一 compiler。Table 采用 hard cutover，新旧客户端仍可分别使用 additive table endpoints 与 legacy endpoints，不维护两套前端 Table truth。当前显式导出由浏览器遍历同一 rows Query Spec，并在 schema fallback、fingerprint/cursor 漂移、重复行或最终数量不等于首屏 total 时 fail closed；非默认/自定义属性排序仍可能扫描并排序完整 membership，由 query timeout 限损；server-stream / async export job、observability、100k/1m staging SLO 与 browser network smoke 属于后续收口项。

## Goal Capsule

- **Objective:** 让 Table 的 Filter、Search、Sort、Group、Hierarchy、Count 和 Export 对任意规模的结果集都保持正确，不再因结果数量改变产品行为。
- **Product authority:** 本文的 Product Contract。实现不得以“已加载窗口”“浏览器内存上限”重新定义 Group、Hierarchy 或 Count 的语义。
- **Primary user outcome:** 用户选择 Group 后，无论查询命中 10、1,001 还是 100,000 个 issue，都能立即看到准确的 Group Header 和 Count，并按需加载组内行。
- **Execution profile:** Go backend + `packages/core` 查询层 + `packages/views` Table 渲染层的跨层改造；保留现有 flat/list/board API 作为兼容路径。
- **Rollout:** 当前 Web/Desktop Table hard cutover 到 additive server Table endpoints；API 错误不得静默 fallback 到前端分组。旧客户端继续使用未删除的 legacy endpoints。
- **Stop conditions:** U1–U8 完成；后端、core、views 测试通过；1001+ issue 场景浏览器验证通过；无自动全量物化；性能与一致性验收项达标。
- **Open blockers:** 无产品决策 blocker。自定义属性 Group 的 100k 数据性能必须在 rollout 前用 `EXPLAIN (ANALYZE, BUFFERS)` 验证，但不阻塞标准字段实现。

---

## 1. Executive Decision

采用一套后端权威的 `IssueTableQuerySpec`，所有需要对完整结果集求真的能力都从该 Query Spec 派生：

- 后端负责 membership、search、sort、group membership、group count、hierarchy membership、facet count 和 aggregate；当前导出由前端遍历同一 server-authoritative rows query，后续可迁到 server-stream/job。
- 前端负责 view mode、columns、column width、collapsed groups、collapsed parents、selection、density 和虚拟滚动。
- Group Header 与 Rows 分开查询：Header 返回准确 Count；Rows 按展开且进入视口的 group/parent branch 使用 cursor 加载。
- Group membership 默认取 issue 自己的字段，不再根据“是否完整加载”改用根父项字段。
- Hierarchy 只嵌套“父子都命中当前 Query Spec 且 group key 相同”的关系；跨组或父项未命中的子项作为该组根行显示。
- 第一版不支持 `multi_select` Group；仍支持它作为 Filter 和 Column。
- 现有 `TABLE_STRUCTURE_MAX_WINDOW = 1000` 及其结构暂停逻辑在新路径稳定后删除，不改成更大的数字。

目标数据流：

```text
View state
  ├─ Query state: scope/filter/search/sort/group/hierarchy
  │    └─ POST /api/issues/table/groups   → exact group headers/counts
  │    └─ POST /api/issues/table/rows     → cursor-paged root/child rows
  │    └─ POST /api/issues/table/facets   → exact filter facets
  │    └─ export replays table/rows pages → same server membership, fail closed
  └─ Presentation state: columns/collapse/selection/density
       └─ frontend render + virtualization
```

目标职责矩阵：

| Capability | Target owner | Reason |
|---|---|---|
| Scope / Filter / Search | Backend | 决定完整 membership、total 和 export |
| Sort / stable pagination | Backend | 必须跨 pages 保持全局顺序 |
| Group membership / order / count | Backend | 必须对完整结果集求真 |
| Hierarchy membership / child count | Backend | 父项可能不在当前客户端 window |
| Facet count / aggregate | Backend | loaded rows 不能代表全集 |
| Export | Backend membership + frontend orchestration（当前） | 显式导出逐页读取同一 rows query；fingerprint/cursor/schema 漂移或最终数量不等于首屏 total 时拒绝生成部分 CSV。server stream/job 为后续优化 |
| View mode / columns / width / density | Frontend | 只改变呈现，不改变 membership |
| Group / parent collapse | Frontend | 个人交互状态，决定哪些 branch 需要请求 |
| Selection | Frontend | 当前交互 session 状态；membership 变化时清理 |
| Saved View persistence | Backend（follow-up） | 团队共享、权限、跨设备和 URL identity |

---

## 2. Product Contract

### 2.1 Problem Frame

当前 Table 从 `GET /api/issues` 获取每页 100 条平铺数据。`tableGrouping` 和 `tableHierarchy` 不进入后端请求，而是在 `TableView` 中对已加载数组执行 `buildIssueTableRows`。

为了让分组和父子树“看起来完整”，Table 在结果不超过 1000 时自动拉完所有 offset pages；超过 1000 时又把 Group 和 Hierarchy 强制改成 `none/false`。因此同一个用户动作的产品语义会随结果数量变化：

- 999 条：Group 生效，但浏览器自动下载全部数据。
- 1001 条：相同 Group 选择不生效，只显示一条弱提示。
- Filter 缩窄到 1000 以下：Group 又自动恢复。
- Hierarchy 开启时，子 issue 在窗口未完成前按自己字段归组，完成后改按根父项字段归组，行会跳组。

这不是性能优化，而是查询责任放错层级后的行为不一致。

### 2.2 Non-negotiable Invariants

- **R1 — Result-size independence.** Group/Hierarchy 是否可用不得由结果数量决定。
- **R2 — One query, one membership.** Table rows、group counts、facet counts、selection membership、export 必须使用同一个 Query Spec。
- **R3 — Server-authoritative grouping.** Group membership 和 Count 必须由后端基于完整结果集计算；前端不得用 loaded rows 推断完整 Group。
- **R4 — Bounded client window.** 首屏和滚动只加载视口需要的 pages/branches；不得因 Group、Hierarchy、Count 或 working chip 自动下载完整结果集。
- **R5 — Stable semantics.** 同一 issue 的 group key 不得因分页加载进度变化。
- **R6 — Explicit unsupported state.** 不支持的字段必须从 UI capability 中移除，或返回明确错误；不得允许用户选中后静默 no-op。
- **R7 — Exact counts.** Group Header、Filter facet 和 Aggregate 标注为精确值时，必须来自后端完整查询集。
- **R8 — No silent fallback.** 新路径请求失败时显示错误与 Retry，不得回退到 loaded-row grouping。
- **R9 — Realtime correctness.** 会改变 membership、sort、group 或 hierarchy 的写入必须使相关查询失效并重新求真。
- **R10 — Shareable query shape.** Query Spec 必须可序列化，以便后续 Saved View、URL 和 Export 复用；不得绑定 React component state。

### 2.3 Query Semantics

#### Filter

- 空数组表示该维度不过滤。
- 同一维度内 OR，不同维度间 AND。
- `include_no_assignee` / `include_no_project` 与选中值做 OR。
- Custom property filter 维持现有语义：同定义内 OR，定义间 AND。
- `working_only` 由后端通过运行中 task membership 求值；新 Query Spec 不再要求浏览器发送全部 running issue IDs。
- `include_sub_issues=false` 是 Query membership，后端使用 `parent_issue_id IS NULL`。
- Search、Filter、Scope 共同决定唯一 membership 集合。

#### Sort

- V1 UI 保持单字段 Sort；API 使用单个 `{field, direction}` 对象。
- `position` 表示 manual order，canonicalization 固定为 ascending；客户端传入的 descending 不改变语义。
- 后端自动追加稳定 tie-break：`created_at DESC, id DESC`。
- Hierarchy 开启时，Sort 应用于每一层的 siblings，而不是把所有 descendants 做一次全局排序。
- Sparse date/property 值固定 `NULLS LAST`，升降序均一致。

#### Group

V1 支持：

- `none`
- `status`
- `assignee`
- `property:<id>`，仅 active `select` / `checkbox`

V1 不支持：

- `multi_select`：一个 issue 可能属于多个值，涉及重复行、unique total、batch selection 和 export 语义。
- `text` / `url` / `number` / `date`：高基数 group 暂不作为首发能力。
- archived/unknown property：返回 `unsupported_group_field`，前端清理 stale persisted value 并提示用户。

Group membership 使用 issue 自己的值：

- Status：`issue.status`。
- Assignee：`assignee_type + assignee_id`；NULL 进入 `unassigned`。
- Select：active option ID 正常成组；缺失值进入 `no-value`；引用已删除 option 的 stale value 进入单独的 `unavailable-value` group，不能和真正未填写合并。
- Checkbox：`false`、`true`、`no-value` 是三个不同 group。

只返回非空 group。Group order：

- Status：使用 `ALL_STATUSES` 的 canonical order。
- Assignee：按 display name case-insensitive 排序，再以 actor type/id tie-break；unassigned 最后。
- Select：按 definition config option order；`unavailable-value`、`no-value` 依次排在最后。
- Checkbox：false、true、no-value。

#### Hierarchy

Hierarchy 与 Group 同时存在时使用以下稳定规则：

1. 父 issue 和子 issue 都必须命中当前 Query Spec。
2. 两者 group key 必须相同。
3. 同时满足时，子 issue 可嵌套在父 issue 下。
4. 父项未命中、父项属于另一 group、或父项不存在时，子 issue 在自己的 group 中作为 root row 渲染。
5. 不自动注入未命中的 ancestor context row；这样 total、selection 和 export 都只包含真正命中的 issue。
6. 每个 row 返回当前查询下的 `direct_child_count`；子分支在展开/进入视口时按需加载。

该规则避免“Todo 子项因为父项是 Backlog 而出现在 Backlog group”这种 group label 与 row value 冲突，也避免当前实现按加载完整度改变规则。

#### Collapse and Selection

- `collapsedGroups`、`collapsedParents` 保留为前端/个人状态。
- 折叠 group 不请求 group rows。
- 折叠 parent 不请求 child rows。
- Selection 只包含真实 issue IDs，不包含 group headers 或 loading placeholders。
- Query membership 改变时清理 selection；仅 Sort 改变时保留 selection，延续现有规则。

### 2.4 Facet Count Semantics

Filter 菜单中的数量采用 disjunctive faceting：

- 计算某个维度的候选值时，保留 scope、search 和其他 filter dimensions。
- 暂时排除当前 dimension 自身，再分别计算各 candidate count。
- 已选值即使 count=0 仍返回，以便用户看到并移除。
- Response 中的 counts 按 contract 即为精确值，不另设 `exact` 标记；超时或不可用时整个维度显示 loading/unknown，不显示 loaded-row 近似值。

### 2.5 Toolbar Information Architecture

Desktop Table header 调整为第一层能力：

```text
Filter · Sort: Title ↑ · Group: Status · Columns · Table
```

- `Sort` trigger 只承载排序，不再用 “Title” 入口承载 Group/Hierarchy/Columns。
- `Group` trigger 始终展示当前字段；`None` 时可只显示 “Group”。
- `Columns` 和 `Hierarchy` 放入 Table Display 菜单。
- Mobile 可保留合并 Display popover，但 active Group 必须在 trigger/badge 中可见。
- 新请求加载时保留上一组 headers 作为 placeholder，并显示 refreshing 状态；不闪回 ungrouped table。

### 2.6 Acceptance Examples

- **AE1 — 1001+ issues.** Given 查询命中 1,001 个 issue，When Group=Status，Then 所有非空 status headers 与准确 counts 出现，网络不会自动请求 11 个 flat pages。
- **AE2 — Large assignee cardinality.** Given 10,000 个 issue、2,000 个 assignee groups，When 打开 Table，Then group headers 自身 cursor 分页；只为展开且进入视口的 groups 获取 rows。
- **AE3 — Group count parity.** Given Filter=Priority High，When Group=Assignee，Then group counts 之和等于 Query total，且 Export 使用相同 membership。
- **AE4 — Cross-group child.** Given parent.status=Todo、child.status=Done，且两者都命中，When Group=Status + Hierarchy on，Then child 作为 Done group 的 root row，不嵌套在 Todo parent 下。
- **AE5 — Missing parent.** Given child 命中而 parent 不命中，Then child 作为自己的 group root；total 不加入 parent。
- **AE6 — Same-group hierarchy.** Given parent/child 都是 Todo 且都命中，When 展开 parent，Then child 在 parent 下出现，child page 可继续 cursor 加载。
- **AE7 — Multi-select property.** Given property type is multi_select，Then Group menu 不显示它；Filter 和 Column 仍可用。
- **AE8 — Realtime regroup.** Given issue 从 Todo 改到 Done，Then loaded row 可立即 optimistic patch，随后 Todo/Done headers、counts 和 branches 失效并重新求真。
- **AE9 — Error behavior.** Given group rows 请求失败，Then 对应 branch 显示 Retry；Table 不显示 loaded-row derived group。
- **AE10 — Stale property.** Given persisted group references archived property，Then API 返回 structured unsupported error，前端切回 Group=None 并给出一次性说明。

### 2.7 Scope Boundaries

**本计划交付：**

- Canonical Table Query Spec。
- Server-backed group headers/counts。
- Cursor-paged root/child rows。
- Status、Assignee、Select、Checkbox Group。
- Hierarchy lazy branches。
- Exact filter facets。
- Query-consistent export（当前复用 rows cursor；server stream/job 后续实现）。
- Toolbar 信息架构调整。
- Realtime cache correctness。
- Hard-cutover compatibility、observability 和 rollout 验证。

**后续能力，接口预留但不在首发：**

- Multi-select Group。
- 多字段 Group / nested Group。
- 多字段 Sort。
- text/number/date 高基数 Group。
- Group aggregate footer 的 Sum/Average/Min/Max；V1 只完成 count 和现有 calculation state 清理。
- Saved View 服务端实体、权限和团队共享。
- Web URL 编码的临时 view overrides。
- Board/List/Swimlane 全部切到同一 Query API；本计划先保证 Table。

---

## 3. Current-State Code Map

### 3.1 Frontend State and Controls

- `packages/core/issues/stores/view-store.ts`
  - `tableGrouping`、`tableHierarchy`、collapsed groups/parents、columns 都在 Zustand。
  - `viewStorePersistOptions` 把这些状态存入 workspace-aware local storage。
  - `tableHierarchy` 默认 `true`，因此当前 Table 即使不 Group 也可能触发全量自动加载。
- `packages/views/issues/components/issues-header.tsx`
  - Display trigger 显示当前 Sort label，却同时容纳 Group、Hierarchy、Sort、Sub-issues 和 Columns。
  - `tableGroupableProperties` 当前包含 `select`、`multi_select`、`checkbox`。
  - Filter option counts 来自 `scopedIssues`；Table 未完成分页时 `facetCountsExact=false`，因此隐藏 counts。
- `packages/views/issues/surface/use-issue-surface-controller.ts`
  - 将 Filter/Search/Sort 编译成 `IssueFlatFilter + IssueSortParam`。
  - 不读取 `tableGrouping` 作为查询参数，因此 Group 不进入 query key/API。
  - Table Search 是本地 state + 250ms debounce。
- `packages/core/issues/stores/surface-view-store.ts`
  - View 状态按 surface key 存在本地 registry，不可分享。

### 3.2 Table Data and Structure

- `packages/core/issues/queries.ts`
  - `ISSUE_FLAT_PAGE_SIZE = 100`。
  - `issueFlatListOptions` 使用 offset infinite query。
  - `fetchAllFlatPages` 用于 Export，在浏览器循环到 total。
  - `scope === "all"` 时前端分别拉 assigned/created/involved，再合并去重。
- `packages/views/issues/surface/use-issue-surface-data.ts`
  - Table 使用 `useInfiniteQuery(issueSurfaceFlatOptions)`，再 flatten pages。
  - 后端已经执行的 Filter 又在 loaded rows 上执行一次 `applyIssueFilters`。
  - working chip 为了精确 scope 又维护第二个最多 1000 的 materialized window。
- `packages/views/issues/components/table-view-model.ts`
  - `TABLE_STRUCTURE_MAX_WINDOW = 1000`。
  - `buildIssueTableRows` 在浏览器执行 Group/Hierarchy。
  - complete hierarchy 下，group key 来自根 ancestor；incomplete window 下来自 issue 自己。
- `packages/views/issues/components/table-view.tsx`
  - Group/Hierarchy wanted 时连续 `fetchNextPage()`。
  - total>1000 时把有效 Group/Hierarchy 强制关闭。
  - Group count 使用 loaded `group.issues.length`。

### 3.3 Backend

- `server/internal/handler/issue.go::ListIssues`
  - 支持 Table 的 status/priority/actor/project/label/property/date/search/top-level/ids filters。
  - 执行 server sort、`LIMIT/OFFSET` 和独立 `COUNT(*)`。
  - 每页 limit clamp 到 100。
- `server/internal/handler/issue.go::QueryIssues`
  - 只是 `GET /api/issues` 的 POST twin，接收 string map，解决大量 IDs 的 request-line 限制。
- `server/internal/handler/issue.go::ListGroupedIssues`
  - 后端分组雏形，只接受 `group_by=assignee`。
  - 使用 `COUNT(*) OVER (PARTITION BY...)` 和 `ROW_NUMBER()`，offset/limit 对每组生效。
  - 只服务 Board assignee grouping，Table 不调用。
- `server/internal/handler/property.go`
  - 已有 property filter compiler 和 typed property sort expression。
- Existing indexes
  - Workspace、status、assignee、parent、project、title trigram、properties GIN 已存在，但 assignee/parent 缺少 workspace-leading composite index。

### 3.4 Root Cause

`tableGrouping` 和 `tableHierarchy` 不属于服务器 query contract，导致：

1. 后端只能返回平铺 pages。
2. 前端要准确 Group/Tree 就必须拥有完整 membership。
3. 完整 membership 与无限数据规模冲突。
4. 1000 上限成为防止 unbounded download 的防护栏。
5. 防护栏又被呈现成产品限制。

因此修复点是 query contract，不是 Table render function，也不是把上限改大。

---

## 4. Target Query Contract

### 4.1 Canonical Type

TypeScript 形态；Go 使用等价 tagged structs：

```ts
type IssueTableScope =
  | { kind: "workspace"; assignee_types?: IssueAssigneeType[] }
  | { kind: "project"; project_id: string }
  | { kind: "assignee"; actor: IssueActorRef }
  | { kind: "creator"; actor: IssueActorRef }
  | {
      kind: "my";
      relation: "assigned" | "created" | "involved" | "any";
    };

interface IssueTableFilters {
  statuses?: IssueStatus[];
  priorities?: IssuePriority[];
  assignees?: IssueActorRef[];
  include_no_assignee?: boolean;
  creators?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  properties?: Record<string, string[]>;
  date?: {
    field: "created_at" | "updated_at";
    start: string;
    end: string;
  };
  working_only?: boolean;
  include_sub_issues?: boolean;
}

interface IssueTableQuerySpec {
  scope: IssueTableScope;
  filters: IssueTableFilters;
  search?: string;
  sort: {
    field: SortField;
    direction: "asc" | "desc";
  };
}

type IssueTableGroupSpec =
  | { kind: "none" }
  | { kind: "status" }
  | { kind: "assignee" }
  | { kind: "property"; property_id: string };
```

设计约束：

- `workspace_id` 继续由现有 workspace resolution/middleware 决定，不信任 body 中的任意 workspace ID。
- `scope.kind="my"` 使用当前 authenticated user；body 不接受 user ID，防止查询其他用户的 personal relation scope。
- Query Spec canonicalize 后生成 SHA-256 fingerprint；cursor 携带 version + fingerprint。
- 所有 arrays 在 fingerprint 前排序/去重；语义顺序字段（如 status order）不直接使用客户端顺序。
- 空值规范化为字段缺失，避免等价 query 产生不同 cache key/fingerprint。

### 4.2 Endpoint: Group Headers

`POST /api/issues/table/groups`

Request：

```json
{
  "query": {
    "scope": { "kind": "workspace" },
    "filters": { "statuses": ["todo", "in_progress"] },
    "sort": { "field": "title", "direction": "asc" }
  },
  "group": { "kind": "status" },
  "page": { "limit": 100, "cursor": null }
}
```

Response：

```json
{
  "query_fingerprint": "sha256:...",
  "total": 18342,
  "groups": [
    {
      "key": "status:todo",
      "value": { "kind": "status", "status": "todo" },
      "count": 5210
    }
  ],
  "next_cursor": null
}
```

Group value 是 tagged union：

```ts
type IssueTableGroupValue =
  | { kind: "status"; status: IssueStatus }
  | { kind: "assignee"; actor: IssueActorRef | null }
  | {
      kind: "property";
      property_id: string;
      value: string | boolean | null;
      value_state: "value" | "unavailable" | "unset";
    };
```

`value` 仅在 `value_state="value"` 时承载 active option/checkbox value；`unset` 与 `unavailable` 使用不同 stable group key，即使 response 中 value 为 null 也不会合并。

Notes：

- `total` 是 Query membership 的 unique issue 数量。
- Group counts 之和必须等于 `total`，因为 V1 每个 issue 只属于一个 group。
- Group headers 本身 cursor 分页，limit default/max 为 100。
- Group label 由前端根据 locale/catalog 渲染；后端可在 assignee group SQL 中按 display name 排序，但不把 localized label 当 identity。

### 4.3 Endpoint: Rows and Child Branches

`POST /api/issues/table/rows`

Request：

```json
{
  "query": { "...": "same IssueTableQuerySpec" },
  "group": { "kind": "status" },
  "group_key": "status:todo",
  "hierarchy": { "enabled": true },
  "parent_id": null,
  "page": { "limit": 50, "cursor": null }
}
```

- `parent_id=null + hierarchy=true`：返回该 group 的 root rows。
- `parent_id=<id> + hierarchy=true`：返回该 parent 的 direct matching/same-group children。
- `hierarchy=false`：`parent_id` 必须为空，返回 group 中的平铺 rows。
- `group.kind=none`：`group_key` 必须为空，同一 endpoint 支持 ungrouped Table。

Response：

```json
{
  "query_fingerprint": "sha256:...",
  "group_key": "status:todo",
  "parent_id": null,
  "total": 0,
  "rows": [
    {
      "issue": { "id": "...", "title": "..." },
      "direct_child_count": 3
    }
  ],
  "branch_total": 1,
  "next_cursor": "opaque-cursor"
}
```

`total` 只在 `group.kind=none`、`parent_id=null` 的首页返回整个 Query membership；续页、grouped branch 和 child branch 返回 `0`，grouped header 使用 groups response 的 `total`/`count`。`branch_total` 为兼容响应结构保留，值等于当前页 `rows.length`，不是权威 branch count；这两个约束避免 rows endpoint 为每个 branch/page 额外执行全量 COUNT。

### 4.4 Cursor Design

不继续扩展 offset pagination。Cursor 使用 base64url JSON envelope：

```json
{
  "v": 1,
  "query": "sha256:...",
  "group_key": "status:todo",
  "parent_id": null,
  "sort_is_null": false,
  "sort_value": "Example title",
  "created_at": "2026-07-17T10:00:00Z",
  "id": "uuid"
}
```

- Cursor fields 全部经过类型验证并只作为 bind args，绝不拼接用户值到 SQL。
- Cursor fingerprint 与当前 canonical Query Spec 不一致时返回 `409 cursor_query_mismatch`。
- 默认 `position ASC, created_at DESC, id DESC` 保留精确的混合方向谓词，同时增加语义冗余的 `position >= cursor.position` index bound，使 PostgreSQL 可直接 seek 到深页位置；不能改成普通 row-value 比较，因为 position 不保证唯一且 tie-break 方向相反。
- Cursor 不代表跨请求数据库 snapshot。每个响应是当次 transaction/read snapshot；实时写入通过 WS invalidation 触发 refetch。
- Keyset 可避免普通 insert 下 offset shift 的重复/遗漏；如果某行的 sort/group 值在翻页期间变化，它可能跨过 cursor，WS invalidation 是最终一致性的修复机制。
- Group header cursor 使用 group order rank/display sort key/stable group key，而不是 row sort cursor。

### 4.5 Endpoint: Facets

`POST /api/issues/table/facets`

Request 复用 Query Spec，并声明需要的 facets：

```json
{
  "query": { "...": "IssueTableQuerySpec" },
  "facets": [{ "kind": "status" }],
  "include_total": false
}
```

Response 返回 query fingerprint、query total，以及每个 facet 的 kind、可选 property ID 和 `{key,count}` values。`include_total` 省略时默认为 `true`，保留原 endpoint contract；筛选选项不消费 query total，因此前端传 `false`，跳过额外 COUNT。

Table 初始渲染不请求 facets。用户打开某个标准字段或 custom property 的筛选子菜单时，只请求该维度；关闭筛选菜单后停用查询。这样一次正常交互最多执行当前维度的聚合，不会在每次 Table mount / realtime invalidation 时依次扫描全部标准字段和工作区 properties。

API 仍支持批量 `facets[]`。对于没有自身 active filter、因而与 base query 共享同一 membership 的 status / priority / assignee / creator / project 维度，server 使用一条 `GROUP BY GROUPING SETS` 查询，并可把 base total 作为空 grouping set 同批返回。存在自身 active filter 的维度必须使用“排除自身 filter”的独立 universe；label join 与 custom-property 展开也继续独立查询，避免改变 exact disjunctive facet 语义。

前端不再用 `scopedIssues` 计算 Table facet counts；Board/List 在统一 Query API 前可保留现有行为。

### 4.6 Endpoint: Export

V1 不新增独立 export endpoint。显式导出在浏览器中使用与 Table 相同的 Query Spec，逐页调用 `POST /api/issues/table/rows`：

- 导出固定使用 `group=none`、`hierarchy=false`，因此只输出 query membership 中的真实 issue 行，不受 viewport、collapsed state 或 loaded branches 影响。
- 每一页必须保持相同的非空 `query_fingerprint`，cursor 必须前进，issue ID 不得重复；结束时唯一 issue 数必须等于首屏 `total`（续页 `total=0`）。
- Response schema fallback、fingerprint 漂移、cursor loop、重复 issue 或最终数量不一致时 fail closed，不生成可能截断或混合 snapshot 的 CSV。
- CSV formula injection protection沿用 `escapeCsvCell`。
- Server-stream / async export job 是大规模导出的后续优化；迁移时仍须复用同一 Query Spec 和上述一致性语义。

### 4.7 Error Contract

```json
{
  "error": "unsupported_group",
  "code": "property_type_unsupported",
  "message": "This property type cannot be used for grouping."
}
```

- `400`：shape、UUID、limit、date range、cursor envelope 等非法；body 使用现有 `{ "error": "..." }` contract。
- `409`：cursor 与 query/group/parent 不匹配。
- `422 unsupported_group`：unknown/archived/unsupported property 或 group kind；机器可读原因在顶层 `code`。
- `500/504`：受控 query failure/timeout，body 沿用 handler 的顶层 error contract。

前端对 branch 错误局部展示 Retry；group header 冷启动错误展示 Table-level Retry。

---

## 5. Backend Design

### 5.1 Shared Filter Compiler

新建 `server/internal/handler/issue_query_filter.go`，抽出 canonical filter representation 和 SQL builder：

```go
type issueQueryFilter struct {
    Scope            issueTableScope
    Statuses         []string
    Priorities       []string
    Assignees        []issueActorFilter
    IncludeNoAssignee bool
    Creators         []issueActorFilter
    ProjectIDs       []pgtype.UUID
    IncludeNoProject bool
    LabelIDs         []pgtype.UUID
    Properties       [][]json.RawMessage
    Date             *issueDateFilter
    Search           string
    WorkingOnly      bool
    IncludeSubIssues bool
}
```

Builder 返回：

```go
type issueQuerySQL struct {
    Where string
    Args  []any
}
```

要求：

- 新 JSON decoder、legacy GET query decoder、现有 POST twin 都编译到同一 canonical struct。
- `ListIssues` 的 ordinary paged path 和 `ListGroupedIssues` 逐步改用同一 builder，避免新旧 endpoint filter drift。
- `open_only` 和 `scheduled` 的特殊路径不强行塞进 Table contract；保留现有分支。
- `my relation=any` 在 SQL 中做 assigned OR created OR involved，不再前端三次全量请求合并。
- `working_only` 使用 `EXISTS` 查询 running tasks；不得把 WS snapshot IDs 作为 canonical membership。
- 所有动态 column/expression 只能来自枚举或后端解析过的 active property definition。

### 5.2 Group Capability Resolver

新建 `server/internal/handler/issue_table_group.go`：

```go
type resolvedIssueGroup struct {
    Kind            string
    PropertyID      *pgtype.UUID
    PropertyType    string
    GroupExpr       string
    GroupPredicate  func(value issueGroupValue, addArg func(any) string) string
    OrderExpr       string
}
```

- Static kind 使用固定 SQL expression。
- Property kind 先通过 workspace-scoped catalog 查询确认 active/type。
- UUID 重新序列化后才允许嵌入 JSONB extraction expression；group value 始终 bind。
- Group key 是后端编码的稳定 identity，不直接当 SQL fragment。
- Stale select option 单独进入 stable stale/no-value bucket；不要丢行。

### 5.3 Group Header Query

逻辑 SQL：

```sql
WITH filtered AS (
    SELECT i.*, <group_expr> AS group_value
    FROM issue i
    WHERE <canonical_where>
), grouped AS (
    SELECT group_value, COUNT(*) AS issue_count
    FROM filtered
    GROUP BY group_value
)
SELECT group_value, issue_count
FROM grouped
WHERE <group_cursor_predicate>
ORDER BY <semantic_group_order>, <stable_group_key>
LIMIT $limit_plus_one;
```

`total` 可由 `SUM(issue_count) OVER ()` 返回，避免第二次 count。若 group cursor 后的 page 会使 window sum 只覆盖剩余行，则在 cursor 前的 grouped CTE 上计算 total，再外层分页。

Assignee display order 需要 workspace-scoped actor name projection。优先复用现有 member/agent/squad lookup；unassigned 明确排最后。

### 5.4 Row/Hierarchy Query

逻辑分两步：

1. `filtered_group`：完整 Query membership + requested group predicate。
2. `branch`：根据 hierarchy/parent 选择 roots 或 direct children。

Root 判定：

```sql
NOT EXISTS (
  SELECT 1
  FROM filtered_group parent
  WHERE parent.id = child.parent_issue_id
)
```

因为 `filtered_group` 已经限定同 group，父项未命中或不同 group 时 child 自动成为 root。

Direct child branch：

```sql
child.parent_issue_id = $parent_id
AND EXISTS (
  SELECT 1 FROM filtered_group parent
  WHERE parent.id = $parent_id
)
```

`direct_child_count` 在当前 Query/group 下批量计算，不能 N+1：

```sql
LEFT JOIN (
  SELECT parent_issue_id, COUNT(*) AS child_count
  FROM filtered_group
  GROUP BY parent_issue_id
) cc ON cc.parent_issue_id = row.id
```

Sort/keyset 在 branch 层执行。Hierarchy off 时直接对 `filtered_group` 做 keyset pagination。

### 5.5 Index Plan

先用 production-like 数据执行 EXPLAIN，再创建必要索引。预期至少需要：

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_assignee
    ON issue (workspace_id, assignee_type, assignee_id);
```

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_parent
    ON issue (workspace_id, parent_issue_id);
```

如果 `working_only EXISTS` 计划不能稳定使用现有 issue-id index，再增加：

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_running_issue
    ON agent_task_queue (issue_id)
    WHERE status = 'running' AND issue_id IS NOT NULL;
```

仓库规则：每个 concurrent index 单独一个、只含单 statement 的 migration；不添加 FK/cascade。

Custom property Group 使用 `i.properties -> '<definition-id>'`。现有 `jsonb_path_ops` GIN 适合 containment Filter，但不能直接加速任意 key 的 GROUP BY。Rollout gate：

- 先验证 100k issue / 20 active properties 上 select/checkbox group p95。
- 若不能达到 SLO，不通过提高客户端上限解决；后续引入无 FK 的 normalized `issue_property_value` projection，并在 property mutation transaction 中双写。
- normalized projection 是性能 contingency，不作为首发前置，除非 EXPLAIN 明确证明必须。

### 5.6 Query Limits and Safety

- Group header limit default/max 100。
- Row/child limit default 50、max 100。
- Search 长度、property filter definitions/values 复用现有 bounds。
- Query context deadline 初始 5s；超时返回 structured `query_timeout`。
- Cursor decode 后限制 envelope 大小和 field lengths。
- 所有 scope 继续经过 `RequireWorkspaceMember`，并复用 `resolveWorkspaceID`。
- Query logs 记录 kind、group kind、hierarchy、limit、duration、returned count、timeout；不记录 search text、issue title 或 raw property values。

---

## 6. Frontend Design

### 6.1 Core Types and API

修改：

- `packages/core/types/api.ts`
  - `IssueTableQuerySpec`
  - groups/rows/facets request/response tagged unions
- `packages/core/api/schemas.ts`
  - 对所有新 response 使用 Zod schema；不要 raw cast。
- `packages/core/api/client.ts`
  - `listIssueTableGroups`
  - `listIssueTableRows`
  - `getIssueTableFacets`
  - export 复用 `listIssueTableRows`
- `packages/core/issues/queries.ts`
  - 新 query key prefix：`issueKeys.tableQueryAll(wsId)`。
  - group headers key 包含 canonical query + group。
  - row branch key 包含 query + group + groupKey + hierarchy + parentId。

Query key 中不得放 cursor；React Query infinite page param 单独管理 cursor。

### 6.2 Surface Query Compilation

在 `packages/core/issues/surface/` 新建 pure compiler：

```ts
buildIssueTableQuerySpec(
  scope: IssueSurfaceScope,
  viewState: IssueViewState,
  search: string,
): IssueTableQuerySpec
```

职责：

- 将 `IssueSurfaceQueryPlan` 转为 typed scope，而不是散落到 flat/grouped params。
- 将 date/property filters、showSubIssues、workingOnly 编译到同一 spec。
- `my/all` 编译为 `{kind:"my", relation:"any"}`，不再调用 `fetchAllMyFlatIssues`。
- Stale property filters 在 catalog settled 后移除，延续现有 cold-catalog guard。
- Canonicalize arrays，以稳定 React Query key 和后端 fingerprint。

### 6.3 Table Query Hooks

建议新建 `packages/views/issues/surface/use-issue-table-data.ts`，避免继续扩大 `use-issue-surface-data.ts`。

提供：

- `useIssueTableGroups(spec, group)`：group headers infinite query。
- `useIssueTableBranch(spec, group, groupKey, hierarchy, parentId)`：单 branch infinite query。
- `useIssueTableFacets(spec)`：按打开的 filter submenu 懒加载。
- export controller：使用 `group=none` / `hierarchy=false` 遍历 rows cursor，并执行完整性校验。

加载规则：

- Group=None：只加载 root/flat top branch。
- Group!=None：先加载 group headers。
- Collapsed group：不 mount branch query。
- Expanded group：仅当 header/loading placeholder 接近 viewport 时 enable root branch。
- Expanded parent：仅当 parent 可见且未 collapsed 时 enable child branch。
- 不存在任何“为了 count/structure 自动 while(hasNextPage) fetch”的 effect。

### 6.4 Render Model

替换当前 `buildIssueTableRows(issues)` 的全数组模型，改为由 server branches 组装 display rows：

```ts
type IssueTableDisplayRow =
  | { kind: "group"; descriptor: IssueTableGroupDescriptor }
  | { kind: "issue"; issue: Issue; depth: number; childCount: number }
  | { kind: "branch-loading"; key: string; depth: number }
  | { kind: "branch-error"; key: string; depth: number };
```

- Frontend只做 flatten 已加载 branch tree，不重新计算 group key/membership/count。
- Group count 始终取 descriptor.count。
- `depth` 由 branch parent 链计算，不依赖后端返回全树。
- DataTable 继续 virtualization；动态 branch 插入后由 virtualizer remeasure。
- Shift range selection 只覆盖当前可见且已加载 issue rows，行为在 UI copy/测试中明确。
- “Loaded X / Total” 改为 “Visible X · Total Y”；X 是当前已加载真实 issue 数，不暗示完整加载。

### 6.5 View State

保留前端：

- `viewMode`
- `tableColumns` / width / order
- `tableCollapsedGroups`
- `tableCollapsedParents`
- selection
- density（未来）

进入 Query Spec：

- filters
- search
- sort
- `tableGrouping`
- `tableHierarchy`
- showSubIssues

`tableCalculation` 当前没有生产消费者：本计划中从 persist state 和 dead helper 清理，或在 Aggregate 单独立项前保持 hidden 但不得展示不准确结果。

Web 后续通过 app-level NavigationAdapter 把 query state 编码到 URL；`packages/views` 不引入 `next/*`。

### 6.6 Cache and Realtime

新 query prefix：

```ts
issueKeys.tableQueryAll(wsId)
```

第一版采用 correctness-first invalidation：

- Issue create/delete：invalidate workspace table query prefix。
- Issue update：先 patch 所有 loaded row branches 中的 issue snapshot。
- 如果 changed dimensions 影响任何 query 的 filter/sort/group/hierarchy，invalidate 对应 table query keys。
- Assignee/status/property/parent/project/label/title/date 改动均可能改变至少一个 Table query；利用 query key 中的 spec 做 predicate，无法证明安全时失效。
- Group header counts 永远通过 refetch 修正，不在客户端猜 source/destination count delta。
- Mutation optimistic update 失败时 rollback row snapshots；现有 `cache-coordinator` 模式继续复用。

后续可优化为只 invalid source/destination group，但首发不以复杂 delta logic 换取少一次 refetch。

### 6.7 Deployment Strategy

本次不引入 frontend-public release flag，也不在同一客户端保留双 Table 数据路径：

- 新 Table endpoints 是 additive API；老 Web/Desktop 客户端继续使用 legacy endpoints。
- 新客户端的 Table membership/structure 始终使用新 API；API error 显示 Retry，不 fallback 到 loaded-row Group。
- 回滚以回滚 Web/Desktop build 为单位，后端 additive endpoints 和 indexes 可保留，不涉及数据回迁。
- Board/List 仍使用原有 API；它们不在本次 hard cutover 范围内。

---

## 7. Implementation Units

### U1. Canonical Query Spec and Shared Filter Compiler

- **Goal:** 所有 Table endpoint 和 legacy list path 可复用同一 membership compiler。
- **Requirements:** R2、R5、R10。
- **Dependencies:** none。
- **Files:**
  - `server/internal/handler/issue_query_filter.go`（new）
  - `server/internal/handler/issue.go`
  - `server/internal/handler/issue_table_filters_test.go`
  - `server/internal/handler/issue_involves_test.go`
- **Approach:** 定义 Go Query Spec、canonicalization、scope/filter validation、where/args builder；把 ordinary `ListIssues` 和 `ListGroupedIssues` 的重复 filter fragments 接到 builder。保留 `open_only/scheduled` 特殊路径。
- **Critical test scenarios:** legacy GET 与新 canonical spec 在所有现有 Table filters 上返回相同 membership；my relation any 去重；workspace isolation；empty/no-value semantics；property/date/search parity。
- **Verification:** 现有 handler issue tests 全绿；新增 compiler table tests 覆盖每个 dimension。

### U2. Group Resolver and Group Headers API

- **Goal:** 后端返回准确、可分页的 group headers/counts。
- **Requirements:** R1、R3、R6、R7。
- **Dependencies:** U1。
- **Files:**
  - `server/internal/handler/issue_table_group.go`（new）
  - `server/internal/handler/issue_table_query.go`（new）
  - `server/internal/handler/issue_table_query_test.go`（new）
  - `server/cmd/server/router.go`
  - `server/internal/featureflags/keys.go`
- **Approach:** 实现 status/assignee/select/checkbox resolver；group semantic order；group header keyset cursor；structured errors；flag gate。
- **Critical test scenarios:** 1,001 seeded issues；count sum=total；unassigned/no-value；select option order；checkbox false/true/null；archived/unknown/multi-select rejection；2-page group cursor 无重无漏。
- **Verification:** Go integration tests；EXPLAIN standard groups on 100k fixture。

### U3. Cursor-paged Rows and Lazy Hierarchy API

- **Goal:** 支持 ungrouped/grouped flat rows、root rows 和 direct child branches。
- **Requirements:** R1、R4、R5。
- **Dependencies:** U1、U2。
- **Files:**
  - `server/internal/handler/issue_table_query.go`
  - `server/internal/handler/issue_table_cursor.go`（new）
  - `server/internal/handler/issue_table_query_test.go`
- **Approach:** 实现 typed cursor、query fingerprint、stable keyset、same-query same-group hierarchy、page row counts/direct child counts。
- **Critical test scenarios:** title/date/property/position asc+desc；NULLS LAST；created_at ties；cursor mismatch；same-group child；cross-group child；filtered-out parent；deep tree branch loading；parent moved between groups。
- **Verification:** 反复分页结果与一次性 SQL baseline ID set 相同；race/update scenario 通过 invalidation contract 测试。

### U4. Indexes and Performance Gate

- **Goal:** 标准字段 group/rows/hierarchy 达到 rollout SLO。
- **Requirements:** R4。
- **Dependencies:** U2、U3 query shape settled。
- **Files:**
  - next available single-statement migrations for workspace-assignee/workspace-parent
  - optional running-task partial index migration
  - EXPLAIN notes appended to this plan or linked benchmark artifact
- **Approach:** production-like 100k/1m fixtures EXPLAIN；只为证明确有收益的 indexes 建 migration；每个 index 单文件 `CREATE INDEX CONCURRENTLY`。
- **Verification:** 无 sequential global scan；workspace predicate 生效；group header p95<500ms、rows p95<300ms 的 staging 指标目标。

### U5. Core API, Schemas, Query Keys and Surface Compiler

- **Goal:** 提供类型安全、可缓存的新 Table repository。
- **Requirements:** R2、R10。
- **Dependencies:** U2、U3 contract stable。
- **Files:**
  - `packages/core/types/api.ts`
  - `packages/core/api/schemas.ts`
  - `packages/core/api/client.ts`
  - `packages/core/issues/queries.ts`
  - `packages/core/issues/surface/query-plan.ts`
  - `packages/core/issues/surface/repository.ts`
  - new compiler/tests under `packages/core/issues/surface/`
- **Approach:** typed union + Zod parse；canonical key builder；group/branch infinite query options；my any scope 不再走三次 full fetch。
- **Critical test scenarios:** schema defaults不得把 malformed group value 伪装成 empty success；query keys 隔离 group/parent/hierarchy；canonical array order；POST body exactness；response enum 向前兼容。
- **Verification:** core unit tests、API schema tests、typecheck。

### U6. Table Data Hook and Server-backed Renderer

- **Goal:** Table 不再物化完整 window，也不再前端求 Group/Hierarchy truth。
- **Requirements:** R1–R5、R8。
- **Dependencies:** U3、U5。
- **Files:**
  - `packages/views/issues/surface/use-issue-table-data.ts`（new）
  - `packages/views/issues/surface/use-issue-surface-controller.ts`
  - `packages/views/issues/surface/use-issue-surface-data.ts`
  - `packages/views/issues/surface/issue-surface.tsx`
  - `packages/views/issues/components/table-view.tsx`
  - `packages/views/issues/components/table-view-model.ts`
  - related tests
- **Approach:** group headers + viewport-enabled branch hooks；display row flatten；per-branch error/loading；visible/total copy；保留 TanStack virtualization。
- **Removal:** 新路径删除 `TABLE_STRUCTURE_MAX_WINDOW`、`shouldAutoLoadNextWindowPage`、structure suspension copy/effects；不再对 Group/Hierarchy 调 `fetchNextFlatPage`。
- **Critical test scenarios:** total=1001 group 仍生效；collapsed group zero row requests；expand visible parent只发对应 branch；branch retry；selection忽略 placeholders；switch group保留 placeholder但不显示旧 group 为新 group。
- **Verification:** views tests、typecheck、browser network trace 无 auto-materialization。

### U7. Header Controls and Capability UX

- **Goal:** Group、Sort、Columns 的产品入口与实际能力一致。
- **Requirements:** R6 及 Toolbar contract。
- **Dependencies:** U5、U6。
- **Files:**
  - `packages/views/issues/components/issues-header.tsx`
  - `packages/core/issues/stores/view-store.ts`
  - `packages/views/locales/{en,zh-Hans,ja,ko}/issues.json`
  - header/surface tests
- **Approach:** desktop 拆分 Sort/Group/Columns；移除 multi-select group option；active group持续可见；stale property清理提示；修复 Filter trigger 内嵌不可键盘操作的 clear control。
- **Critical test scenarios:** group trigger label；keyboard navigation；mobile merged display；archived property fallback；all four locales；sort icon accessible label。
- **Verification:** component tests + browser keyboard/accessibility smoke。

### U8. Facets, Export, Realtime and Compatibility

- **Goal:** 所有 Table 衍生能力复用相同 Query Spec，并安全发布。
- **Requirements:** R2、R7–R10。
- **Dependencies:** U5、U6。
- **Files:**
  - backend facets handler and tests
  - `packages/core/issues/cache-coordinator.ts`
  - `packages/core/issues/ws-updaters.ts`
  - `packages/views/issues/surface/use-issue-table-data.ts`
  - analytics/observability touchpoints
- **Approach:** exact disjunctive facets；rows-based fail-closed export；table query prefix invalidation；row optimistic patch；additive endpoint compatibility。
- **Critical test scenarios:** group/facet/export membership parity；export cursor/fingerprint/schema drift 与 final-total mismatch；create/delete/update invalidation；property edit regroup；offline/5xx retry；old desktop client继续使用旧 endpoints。
- **Verification:** core WS/cache tests、integration tests、staging rollout dashboards。

### Sequencing

```text
U1
 └─ U2
     ├─ U3
     │   └─ U4
     └─ U5
         └─ U6
             ├─ U7
             └─ U8
```

U2/U3/U4 可由 backend owner 连续完成；U5 可在 API schema freeze 后与 U3 并行；U6 只有在 hierarchy contract 和 cursor tests 稳定后切换。

---

## 8. Rollout and Compatibility

### 8.1 Compatibility

- 保留 `GET /api/issues`、`POST /api/issues/query`、`GET /api/issues/grouped`。
- 老 Web/Desktop 客户端继续使用旧 API，不受新 response 影响。
- 新前端的 Table 直接使用新 endpoints；Board/List 以及老客户端保持原有 API。
- 新 endpoint 不改变数据库 schema，除性能 indexes 外无数据 migration。

### 8.2 Validation Cohorts

1. Local/dev：功能与测试完成。
2. Internal workspace：收集 group/rows query duration、error、timeout、branch request fan-out。
3. 10% cloud workspaces：重点观察大 workspace、自定义 property group。
4. 100% cloud：发布新 Web/Desktop build。
5. 至少一个 desktop release 周期后，再评估 legacy endpoint 和旧 client grouping 的退休窗口。

### 8.3 Rollback

- 回滚 Web/Desktop build 即回到旧 Table 数据路径；旧 endpoints 全程保留。
- Rollback 不删除 index；concurrent indexes 可在后续独立 migration 决定是否移除。
- 新 Table API 的单次请求失败只显示 Retry，不按请求级别自动 fallback，避免同一 session 同时存在两套 Group truth。

### 8.4 Observability

Backend structured fields：

- endpoint: groups/rows/facets
- group_kind
- hierarchy
- scope_kind
- duration_ms
- db_rows_returned
- group_count/branch_count
- timeout/error_code
- cursor_present

Frontend events：

- table_group_changed
- table_group_headers_loaded/failed
- table_branch_loaded/failed
- table_export_started/completed/failed

不得记录 search text、issue title、raw property values、assignee display name。

---

## 9. Test and Verification Matrix

### Backend Integration

- 每种 filter dimension 与 group 组合。
- `my relation=any` 的 assigned/created/involved OR + dedupe。
- status/assignee/select/checkbox group identity/order/count。
- 1001、10k、100k fixture 的 counts 和 pages。
- group header cursor、row cursor、child cursor。
- asc/desc/null/tie cases。
- hierarchy same-group/cross-group/missing-parent/deep tree。
- workspace isolation 与 actor/property stale input。
- malformed cursor、query mismatch、limit clamp、timeout。
- running_only membership。

### Core Unit

- Query Spec canonicalization。
- Scope compiler。
- Query key separation。
- Zod parse/fallback failure behavior。
- API request serialization。
- Cache invalidation predicate。
- WS create/update/delete handling。

### Views Unit

- Group header exact count display。
- No auto-fetch of all groups/pages。
- Collapse/expand query enablement。
- Branch flatten depth。
- Loading/error placeholders。
- Selection range over visible issue rows only。
- Group/Sort/Columns controls and keyboard semantics。
- Stale property fallback。
- Four locales。

### Browser Smoke

1. Seed/prepare a workspace with >1000 issues。
2. 打开 Network panel，进入 Table。
3. 切换 Group=Status、Assignee、Select、Checkbox。
4. 验证首屏只请求 headers 与可见 branches，不出现连续 offset page loop。
5. 折叠/展开 group 与 parent，验证 branch requests。
6. 修改 status/assignee/property，验证 row optimistic update 后 counts/refetch 正确。
7. Filter/Search/Sort 后验证 group totals 与 Export parity。
8. Keyboard 操作 Filter/Sort/Group/Columns/Table controls。

### Performance SLO

在 staging production-like Postgres 数据上：

- Standard group headers p95 < 500ms，p99 < 1s。
- Row/child page p95 < 300ms，p99 < 800ms。
- Table first interactive 不随 total 线性增长。
- 浏览器自动加载 issue rows 上限为当前可见 branch pages，不超过 2 pages/branch prefetch。
- Group header request memory 不随 issue count在应用层线性增长；聚合发生在数据库。
- Custom property group 若不达标，flag cohort 不扩大，触发 normalized projection contingency。

不要在 CI 以 wall-clock assertion 执行 SLO；CI 验证逻辑正确性，SLO 由可重复 benchmark/staging telemetry 验证。

---

## 10. Risks and Mitigations

### Risk A — Shared filter compiler refactor causes legacy drift

- **Mitigation:** 先建立 legacy GET 与 canonical spec 的 golden parity tests，再逐段替换；保留特殊 open/scheduled 分支。

### Risk B — Dynamic property group scans large JSONB windows

- **Mitigation:** flag cohort、query deadline、EXPLAIN gate；必要时 normalized property-value projection，不回退客户端 materialization。

### Risk C — Hierarchy branch queries重复评估完整 filtered set

- **Mitigation:** workspace-leading parent index；用 CTE/derived query 让 planner复用；对可见 parent 才请求；观察 branch fan-out；必要时批量 children endpoint。

### Risk D — Realtime updates cause count flicker或跨 cursor遗漏

- **Mitigation:** optimistic patch只改善单元格即时性；任何 shape-changing field 都 invalidate server queries；不猜 count delta。

### Risk E — 高基数 Assignee groups 产生过多 HTTP requests

- **Mitigation:** headers cursor；collapsed/viewport enablement；首发监测 fan-out。若 p95 session fan-out过高，增加 batched branch request，而不改变 Query Spec。

### Risk F — Old persisted multi-select grouping

- **Mitigation:** catalog settled 后 capability validation；一次性降级为 none + toast；不要把 multi-select组合值继续发送到 API。

### Risk G — Feature flag造成双路径维护时间过长

- **Mitigation:** 在 rollout 中写明 retirement milestone；100% 后一个 desktop release 周期内删除旧 client structure path。

---

## 11. Definition of Done

- [x] Result >1000 时 Group/Hierarchy 不暂停、不消失。
- [x] Group counts 来自后端完整 Query membership。
- [x] Group/row/child headers 和 rows 均使用 cursor pagination。
- [x] Table 不存在自动 materialize entire window 的 effect。
- [x] Multi-select property 不在 Group capability 中出现。
- [x] Filter、Search、Sort、Group、Hierarchy、Facet、Export 使用同一 Query Spec。
- [x] `my/all` 不再前端三次全量获取合并。
- [x] Working-only 不再通过浏览器发送全部 running IDs。
- [x] Cross-group hierarchy semantics 与本文一致并有 integration tests。
- [x] Realtime shape changes invalidate server group truth。
- [x] Toolbar 独立显示 Sort/Group/Columns/View，active Group 可见。
- [x] 新 API 有 structured errors、query deadlines 和 safe cursors。
- [x] 必要 indexes 以单 statement concurrent migrations 落地。
- [ ] Go、core、views tests 与 typecheck 通过。
- [ ] 1001+ issue browser smoke 和 network trace 通过。
- [ ] Staging SLO 和 custom-property EXPLAIN gate 通过。
- [ ] Rollout/rollback/flag retirement owner 明确。

---

## 12. Primary Source Map

- Frontend view state: `packages/core/issues/stores/view-store.ts`
- Per-surface persistence: `packages/core/issues/stores/surface-view-store.ts`
- Query keys and flat pagination: `packages/core/issues/queries.ts`
- Surface repository: `packages/core/issues/surface/repository.ts`
- Surface query compilation: `packages/views/issues/surface/use-issue-surface-controller.ts`
- Surface data selection: `packages/views/issues/surface/use-issue-surface-data.ts`
- Table structure/render: `packages/views/issues/components/table-view-model.ts`, `table-view.tsx`
- Header controls/facet counts: `packages/views/issues/components/issues-header.tsx`
- API types/schemas/client: `packages/core/types/api.ts`, `packages/core/api/schemas.ts`, `packages/core/api/client.ts`
- Backend flat/grouped handlers: `server/internal/handler/issue.go`
- Property filter/sort compiler: `server/internal/handler/property.go`
- Routes: `server/cmd/server/router.go`
- Cache/realtime: `packages/core/issues/cache-coordinator.ts`, `packages/core/issues/ws-updaters.ts`
- Backend tests: `server/internal/handler/issue_table_filters_test.go`, `issue_grouped_test.go`, `issue_sort_test.go`, `issue_involves_test.go`
- Frontend tests: `packages/views/issues/components/table-view-model.test.ts`, `table-group-row.test.tsx`, `packages/views/issues/surface/use-issue-surface-controller.test.tsx`, `issue-surface.test.tsx`

# Workflow 阶段可视化（Stage Overview）

**Status:** Approved for implementation
**Author:** Claude Code
**Date:** 2026-06-19
**Related context:** `ui/Workflow需求文档.md`, `server/internal/handler/workflow.go`, `server/migrations/108_workflow.up.sql`, `packages/views/workflows/components/dag-canvas.tsx`, `packages/core/types/workflow.ts`

## 1. Background

当前 Multica 的 Workflow 系统具备底层编排能力：节点（`workflow_node`）、边（`workflow_edge`）、执行（`workflow_run`）、节点运行（`workflow_node_run`），以及完整的 agent/worker/critic 配置。前端通过 `WorkflowDetailPage` 提供了 ReactFlow 可视化编辑器。

然而 Workflow 存在两个信息呈现问题：
- **缺少"阶段"这一业务层面的组织概念**。当前所有节点归属在同一个扁平的 DAG 画布上，用户无法看到流程的阶段划分。
- **能力信息未被充分暴露**。plugin、agent、skill、format_schema、critic 等关键配置散落在节点编辑面板中，没有集中查看的入口。

本设计引入 **Stage（阶段）** 作为 Workflow 的一等概念，新建一个独立的概览页，以"阶段画布 → 节点 DAG → 节点配置详情"三层逐层下钻的方式展示 Workflow 的完整结构。

## 2. Goal

为 Workflow 新增一个 `/workflows/[id]/overview` 概览页，实现：

- **阶段画布**：阶段卡片横向排列，展示全局阶段划分和每个阶段的节点数
- **阶段内节点 DAG**：点击阶段后展开该阶段内的只读 ReactFlow 画布，展示节点和边
- **节点配置详情下钻**：点击节点后滑出抽屉面板，展示 worker、critic、format_schema、关联 plugin/skill、上下游关系
- **轻量阶段编辑**：添加/删除/排序阶段，拖拽节点到不同阶段
- **数据库层面新增 `workflow_stage` 表**，`workflow_node` 新增 `stage_id` 外键列

## 3. Non-goals

- 不替换或重构现有 `WorkflowDetailPage` 编辑器
- 不在概览页中编辑节点的 worker/critic/format_schema 配置（这些操作保留在原编辑器）
- 不改变现有的 Workflow Run 执行模型
- 不引入节点内部的复合嵌套（保持阶段→节点两层结构）
- 不实现跨阶段的节点连线（边只在阶段内部）
- 不在首屏一次性展示所有细节

## 4. Architecture Decisions

### 4.1 页面关系：新增而非替换

保留 `WorkflowDetailPage`（编辑器），新建 `WorkflowOverviewPage`（查看/轻量编辑）。路由设计：

- `/workflows/[id]` — 现有编辑器（不变）
- `/workflows/[id]/overview` — 新增概览页
- `/workflows/[id]/runs` — 运行历史（不变）
- `/workflows/[id]/runs/[runId]` — 运行详情（不变）

| Aspect | Overview (new) | Detail (existing) |
|--------|---------------|------------------|
| 主视图 | 阶段卡片条 + 阶段内节点 DAG | 全局节点 DAG 编辑器 |
| 节点 DAG | 只读，阶段内 | 可编辑，全局 |
| 阶段管理 | 添加/删除/排序/分配节点 | 不在编辑器中管理阶段 |
| 节点配置 | 只读查看 | 编辑 worker/critic/schema |
| 定位 | 理解流程结构 | 编辑流程配置 |

### 4.2 阶段归属：所有 Workflow

阶段是 Workflow 的固有属性，不限于模板（`is_template`）。模板和实例都保留阶段结构。

### 4.3 边的作用域：阶段内部

边只在阶段内部连接节点。阶段之间的数据流是隐式的：前一阶段所有终点节点的输出自动成为下一阶段所有起点节点的输入。`stage.sort_order` 决定宏观执行顺序。

### 4.4 交互模式：以查看为主的混合模式

页面以查看/浏览为主（阶段画布、节点 DAG 只读），阶段层面支持轻量编辑（添加/删除/排序/节点分配）。节点内部配置编辑保留在原编辑器。

### 4.5 组件架构：双层画布

上层为 HTML/CSS 实现的横向滚动阶段卡片条，下层为 ReactFlow 只读画布渲染当前选中阶段的节点 DAG，侧边抽屉面板展示节点详情。

选择 HTML 卡片条而非 ReactFlow 统一画布的原因：实现简单、每层职责清晰、ReactFlow 只需处理阶段内单层 DAG、阶段卡片的可访问性和响应式更优。

## 5. Data Model

### 5.1 New table: `multica_workflow_stage`

```sql
CREATE TABLE multica_workflow_stage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES multica_workflow(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_stage_workflow_id ON multica_workflow_stage(workflow_id);
```

### 5.2 Altered table: `multica_workflow_node`

```sql
ALTER TABLE multica_workflow_node
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_workflow_node_stage_id ON multica_workflow_node(stage_id);
```

`stage_id` is nullable to support existing nodes after migration (old nodes get `stage_id = NULL`, rendered as "未分组" on the frontend). When a stage is deleted, its nodes revert to NULL (not cascade-deleted).

### 5.3 Data constraints

- An edge can only connect two nodes that share the same `stage_id` (intra-stage DAG)
- `stage.sort_order` determines the macro execution order between stages
- One stage contains one or more nodes; a node belongs to exactly zero or one stage

### 5.4 Migration

New migration file `server/migrations/XXX_workflow_stage.up.sql`:
1. Create `multica_workflow_stage` table
2. Add `stage_id` column to `multica_workflow_node`
3. Add indices

### 5.5 TypeScript types

```typescript
// packages/core/types/workflow.ts — additions

export interface WorkflowStage {
  id: string;
  workflowId: string;
  name: string;
  description: string;
  sortOrder: number;
  nodeCount: number;        // computed by API
  createdAt: string;
  updatedAt: string;
}

// WorkflowNode gets a new optional field:
// stageId?: string | null;
```

## 6. API Design

### 6.1 Modified endpoint

**`GET /api/workflows/{id}`** — response now includes `stages` array:

```json
{
  "id": "...",
  "title": "...",
  "stages": [
    {
      "id": "...",
      "workflow_id": "...",
      "name": "需求",
      "description": "需求收集与分析阶段",
      "sort_order": 0,
      "node_count": 3,
      "created_at": "...",
      "updated_at": "..."
    }
  ],
  "nodes": [{ "...", "stage_id": "..." }],
  "edges": [...]
}
```

### 6.2 New endpoints

| Method | Path | Description | Handler |
|--------|------|-------------|---------|
| `POST` | `/api/workflows/{id}/stages` | Create a stage | `CreateWorkflowStage` |
| `PUT` | `/api/workflows/{id}/stages/{stageId}` | Update stage (name, description) | `UpdateWorkflowStage` |
| `DELETE` | `/api/workflows/{id}/stages/{stageId}` | Delete stage (nodes → NULL) | `DeleteWorkflowStage` |
| `PUT` | `/api/workflows/{id}/stages/reorder` | Batch update `sort_order` | `ReorderWorkflowStages` |
| `PUT` | `/api/workflows/{id}/nodes/{nodeId}/stage` | Assign node to stage (or remove) | `AssignNodeToStage` |

### 6.3 Edge validation

`CreateWorkflowEdge` and `UpdateWorkflowEdge` must validate that `source_node_id` and `target_node_id` share the same `stage_id`. Cross-stage edges return 400.

## 7. Frontend Component Architecture

### 7.1 File structure

```
packages/core/types/workflow.ts               ← +WorkflowStage interface
packages/core/workflows/queries.ts             ← +workflowOverviewOptions, stage mutations
packages/core/api/client.ts                    ← +stage API methods

packages/views/workflows/components/
├── overview/
│   ├── index.ts                               ← barrel export
│   ├── workflow-overview-page.tsx              ← top-level page component
│   ├── stage-canvas.tsx                       ← horizontal scrollable stage card strip
│   ├── stage-card.tsx                         ← single stage card (name, count, selection)
│   ├── stage-node-dag.tsx                     ← read-only ReactFlow DAG for one stage
│   ├── node-detail-panel.tsx                  ← slide-out node detail drawer
│   ├── node-detail-worker.tsx                 ← worker config display section
│   ├── node-detail-critic.tsx                 ← critic config display section
│   ├── node-detail-schema.tsx                 ← format_schema formatted display
│   ├── node-detail-relations.tsx              ← upstream/downstream node relations
│   ├── stage-create-dialog.tsx                ← create/edit stage dialog
│   └── overview-page.test.tsx                 ← page-level tests

apps/web/app/[workspaceSlug]/(dashboard)/workflows/[id]/overview/
└── page.tsx                                   ← Next.js route wrapper
```

### 7.2 Component tree & responsibilities

```
WorkflowOverviewPage                            ← entry point, fetches workflow + stages
├── StageCanvas                                 ← horizontal scrollable card strip
│   ├── StageCard[]                             ← per-stage card: name, N/M, node count, selected state
│   │   └── click → setSelectedStageId
│   │   └── ┇ menu → edit name / delete stage
│   └── "+" AddStageButton                      ← opens StageCreateDialog
│       └── StageCreateDialog                   ← name, description fields
├── StageNodeDag                                ← shown when stage selected, read-only ReactFlow
│   ├── WorkflowNode[]                          ← reuses reactflow-nodes.tsx (read-only)
│   ├── WorkflowEdge[]                          ← reuses reactflow-nodes.tsx
│   └── EmptyNodesPlaceholder                   ← "此阶段暂无节点"
└── NodeDetailPanel                             ← shown when node selected, slide-out drawer
    ├── BasicInfo (name, description, index)
    ├── NodeDetailWorker                        ← worker type + assignee name
    ├── NodeDetailCritic                        ← critic type + reviewer name
    ├── NodeDetailSchema                        ← formatted format_schema display
    ├── NodeDetailRelations                     ← upstream/downstream + agent/plugin/skill links
    └── "在编辑器中打开" → WorkflowDetailPage
```

### 7.3 Reuse from existing code

- **Node/edge renderers**: reuse `WorkflowNode` and `WorkflowEdge` from `reactflow-nodes.tsx` in read-only mode
- **Node-to-flow mapping**: extract shared logic from `dag-canvas.tsx` into a `useReadonlyFlowData(stageNodes, stageEdges)` hook
- **AssigneePicker data**: reuse agent/member resolution from existing queries
- **API client**: extend `ApiClient` class with stage-related methods

## 8. Data Flow & State Management

### 8.1 Query layer (`packages/core/workflows/queries.ts`)

```typescript
// Query key — overview reuses workflowDetailOptions (same cache key)
// so edits in WorkflowDetailPage auto-invalidate the overview page

// New mutations:
useCreateStage()
useUpdateStage()
useDeleteStage()
useReorderStages()
useAssignNodeToStage()
```

### 8.2 Page-level state (useState — no global store)

| State | Type | Reason |
|-------|------|--------|
| `selectedStageId` | `string \| null` | Pure browse state, not persisted across pages |
| `selectedNodeId` | `string \| null` | Pure browse state |
| `editingStage` | `Stage \| null` | Dialog transient state |

### 8.3 Derived data (pure functions, not state)

- `nodesByStage(stageId)` — filter nodes by stage_id
- `edgesByStage(stageId)` — filter edges where both nodes share stage_id
- `nodeIndexInStage(nodeId, stageNodes)` — compute "节点 2/4" label
- `stageNodeCount` — prefer API's `node_count` field, fallback to local filter

### 8.4 Why not Zustand

Selection state on this page is transient browse UI — not needed by other components, not persisted across navigation. `useState` is sufficient. If future requirements demand "remember last expanded stage", a lightweight persistence field can be added to `workflows/store.ts`.

## 9. UI/UX Behavior

### 9.1 Stage card strip

```
┌──────────────────────────────────────────────────────────┐
│  ← scroll left             Stage Canvas          scroll right →  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ Stage 1/5│  │ Stage 2/5│  │ Stage 3/5│  │ Stage 4/5│ ...      │
│  │  需求    │  │  设计  ✓ │  │  编码    │  │  测试    │          │
│  │ 3 nodes  │  │ 2 nodes  │  │ 4 nodes  │  │ 2 nodes  │          │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘          │
│                    ▔▔▔▔▔▔▔▔▔▔                                     │
│                    selected state                                   │
└──────────────────────────────────────────────────────────┘
```

- Horizontal scroll; gradient fade masks at edges when content overflows
- Selected card gets border + background accent
- `+` button at end of strip for creating stages
- Each card has a ┇ menu: edit name, delete stage
- Drag-and-drop cards for reordering (P1 — initial release uses dialog-based reorder)

### 9.2 Node DAG expansion

- Click stage card → `selectedStageId` changes → ReactFlow renders that stage's nodes/edges
- Transition: `max-height: 0 → 500px` with 300ms ease-out
- Read-only mode: nodes selectable (opens detail panel), but not draggable, no editing, no keyboard shortcuts for delete/undo
- `fitView` on stage change to auto-zoom the DAG
- Empty stage: "此阶段暂无节点，可在编辑器中添加"

### 9.3 Node detail panel

- **Trigger**: click a node in the ReactFlow DAG
- **Position**: slide-out drawer on the right (380px desktop, full-screen mobile)
- **Close**: click outside, × button, or select another node
- **Sections**: worker config → critic config → format_schema → relations → agent/plugin/skill links
- **Footer**: "在编辑器中打开" button → navigate to `/workflows/[id]`

### 9.4 Keyboard interaction

| Key | Action |
|-----|--------|
| `←` / `→` | Select previous/next stage card |
| `Esc` | Close node detail panel / deselect stage |
| `Tab` | Move focus between stage cards |

### 9.5 Responsive

- **≥1024px**: horizontal stage strip, DAG below, detail panel right drawer
- **<1024px**: vertical stage list (accordion), detail panel bottom drawer

## 10. Error Handling & Edge Cases

| Scenario | Handling |
|----------|----------|
| Workflow has no stages | Empty state: "尚未定义阶段" with "创建第一个阶段" CTA button |
| Stage has no nodes | Empty DAG: "此阶段暂无节点，可在编辑器中添加" |
| Node has no worker/critic configured | Detail panel shows "未配置" in muted text |
| Node format_schema is empty | Schema section collapsed or shows "无格式约束" |
| Legacy nodes (stage_id = NULL) | Auto-generated "未分组" virtual card in the strip, collecting all unassigned nodes |
| Workflow not found | Standard 404 page |
| No workspace access | Standard NoAccessPage |
| Many stages (>10) | Horizontal scroll + fade masks, cards keep min-width |
| Many nodes in a stage (>20) | ReactFlow zoom/pan + fitView on stage change |
| Concurrent edit conflict | Last-write-wins for stage names (matches existing editor convention); TanStack Query auto-syncs across tabs |
| Delete stage with nodes | Confirm dialog: "此阶段包含 X 个节点，删除阶段后节点将移至'未分组'" |
| Delete last stage | Allowed; workflow returns to "no stages" state |

### Loading & error states

```
StageCanvas
├── loading:   <StageCanvasSkeleton />  ← 5 gray pulse placeholder cards
├── error:     <Alert variant="destructive"> + retry button
└── empty:     <EmptyStageState />

StageNodeDag
├── loading:   Centered spinner in canvas area
├── error:     Centered error message + retry
└── empty:     <EmptyNodesState />

NodeDetailPanel
├── loading:   Skeleton inside panel
└── error:     Compact error message inside panel
```

## 11. i18n

New namespace entries under `workflows`:

```json
// en/workflows.json additions
{
  "overview": {
    "title": "Workflow Overview",
    "stage_canvas": {
      "empty_title": "No stages defined yet",
      "empty_description": "Stages help you organize workflow nodes into logical phases. Create your first stage to get started.",
      "create_first": "Create first stage",
      "add_stage": "Add stage",
      "unassigned": "Unassigned",
      "stage_n_of_m": "Stage {{n}}/{{m}}",
      "nodes_count": "{{count}} node",
      "nodes_count_plural": "{{count}} nodes"
    },
    "node_dag": {
      "empty_title": "No nodes in this stage",
      "empty_description": "Add nodes to this stage in the workflow editor.",
      "open_editor": "Open in editor",
      "node_n_of_m": "Node {{n}}/{{m}}"
    },
    "detail_panel": {
      "title": "Node Details",
      "worker": "Worker",
      "critic": "Critic",
      "format_schema": "Format Schema",
      "relations": "Relations",
      "upstream": "Upstream",
      "downstream": "Downstream",
      "plugins": "Plugins",
      "skills": "Skills",
      "not_configured": "Not configured",
      "no_schema": "No format constraints",
      "open_in_editor": "Open in editor"
    },
    "stage_dialog": {
      "create_title": "Create Stage",
      "edit_title": "Edit Stage",
      "name_label": "Stage name",
      "name_placeholder": "e.g. Requirements, Design, Build",
      "description_label": "Description (optional)",
      "description_placeholder": "What happens in this stage?",
      "delete_confirm_title": "Delete stage?",
      "delete_confirm_description": "This stage contains {{count}} node(s). Deleting it will move them to \"Unassigned\"."
    }
  }
}
```

## 12. Testing Plan

### 12.1 Go backend tests

| Test | Location | What it verifies |
|------|----------|-----------------|
| Stage CRUD | `server/internal/handler/workflow_stage_test.go` | Create, update, delete, reorder stages |
| Assign node to stage | `server/internal/handler/workflow_stage_test.go` | PUT node stage assignment, including NULL unassignment |
| Cross-stage edge rejected | `server/internal/handler/workflow_edge_test.go` | Creating edge between nodes in different stages returns 400 |
| GET workflow includes stages | `server/internal/handler/workflow_test.go` | Response includes `stages` array with `node_count` |
| ON DELETE SET NULL | `server/internal/handler/workflow_stage_test.go` | Deleting a stage sets its nodes' stage_id to NULL |
| Permission checks | `server/internal/handler/workflow_stage_test.go` | Non-members cannot modify stages |

### 12.2 Frontend component tests (`packages/views/workflows/components/overview/overview-page.test.tsx`)

| Test | What it verifies |
|------|-----------------|
| Renders stage card strip | Stage cards displayed with name, count, order |
| Loading skeleton | Skeleton cards shown while query is loading |
| Empty state | CTA shown when workflow has no stages |
| Stage selection | Clicking a card shows the node DAG for that stage |
| Node selection | Clicking a node opens the detail panel |
| Detail panel content | Worker, critic, schema, relations sections render |
| Unassigned nodes | Nodes with null stage_id appear under "未分组" |
| Empty stage DAG | Placeholder shown when stage has no nodes |
| Stage creation | Dialog opens, submits, new card appears (optimistic) |
| Stage deletion | Confirm dialog → stage removed, nodes go to unassigned |
| "Open in editor" link | Navigates to correct workflow editor URL |
| Error state | Retry button shown on fetch failure |
| Responsive layout | Vertical layout below breakpoint |

### 12.3 E2E tests

| Test | What it verifies |
|------|-----------------|
| Navigate to overview | From workflow list → click workflow → overview page loads |
| Browse stages | Click through stage cards, verify DAG changes |
| Drill into node | Click node → detail panel opens with correct data |
| Create stage | Add a stage, verify it appears in the strip |
| Assign node to stage | Drag node to a different stage (P1) |

## 13. Implementation Phases

### Phase 1 — Data layer (backend)
1. Migration: create `workflow_stage` table, add `stage_id` to `workflow_node`
2. sqlc: write queries in `workflow.sql`, regenerate Go code
3. API: stage CRUD handlers + node-stage assignment handler
4. Edge validation: enforce intra-stage constraint
5. Go tests

### Phase 2 — Frontend page (P0)
1. Route: `apps/web/app/.../workflows/[id]/overview/page.tsx`
2. `WorkflowOverviewPage` — fetch data, orchestrate sub-components
3. `StageCanvas` + `StageCard` — horizontal scrollable strip
4. `StageNodeDag` — read-only ReactFlow with reused node/edge renderers
5. `NodeDetailPanel` — drawer with all config sections
6. Loading, empty, error states
7. i18n strings
8. Component tests

### Phase 3 — Lightweight editing (P0)
1. `StageCreateDialog` — create/edit stage form
2. Stage delete with confirmation
3. Node-to-stage assignment (drag from one stage to another, or dropdown in detail panel)
4. Stage reorder (drag cards or dialog-based sort)

### Phase 4 — Polish (P1)
1. Stage canvas scroll animations
2. DAG expand/collapse transitions
3. Node detail panel animations
4. Responsive layout tuning
5. E2E tests

## 14. Risks & Open Questions

### Risks
- **Migration risk**: existing `workflow_node` rows will have `stage_id = NULL`. The "未分组" virtual card handles this gracefully.
- **Edge validation gap**: existing edges may cross what will become stage boundaries. Migration should log warnings for these; frontend treats cross-stage legacy edges as disconnected.
- **ReactFlow performance**: large stages (50+ nodes) may need virtualization. This is a P2 concern — current expected node counts per stage are <20.

### Open Questions (resolved during brainstorming)
- ~~Stage scope: templates only or all workflows?~~ → All workflows.
- ~~Cross-stage edges?~~ → No, edges are intra-stage only.
- ~~New page or modify existing?~~ → New page, keep existing editor.
- ~~Edit capability on overview page?~~ → Mixed mode: light editing for stages, read-only for nodes.
- ~~Route design?~~ → `/workflows/[id]/overview`.
- ~~DB modeling: FK or mapping table?~~ → FK (`stage_id` on `workflow_node`).

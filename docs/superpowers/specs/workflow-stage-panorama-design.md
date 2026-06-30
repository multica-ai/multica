# Workflow 阶段全景图设计

**Status:** Implemented
**Last updated:** 2026-06-25（合并自 3 份设计文档，以代码实现为准）
**Source documents:**
- `2026-06-19-workflow-stage-overview-design.md`（Stage 数据模型 + API）
- `2026-06-23-workflow-panorama-design.md`（全景图默认视图）
- `2026-06-24-workflow-panorama-flow-canvas-design.md`（流程图画布重构）

## 1. 概述

Multica Workflow 系统具备底层编排能力：节点（`workflow_node`）、边（`workflow_edge`）、执行（`workflow_run`）。本设计引入 **Stage（阶段）** 作为 Workflow 的一等概念，并提供「全景图」作为 `/workflows/[id]` 的默认视图——以泳道卡片风格展示 Stage → Node → Agent/Plugin 三层架构，用 SVG 连线呈现连续流程图画布。

## 2. 数据模型

### 2.1 `multica_workflow_stage` 表

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
CREATE INDEX idx_workflow_stage_sort_order ON multica_workflow_stage(workflow_id, sort_order);
```

### 2.2 `multica_workflow_node` 扩展

```sql
ALTER TABLE multica_workflow_node
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_workflow_node_stage_id ON multica_workflow_node(stage_id);
```

`stage_id` 可为空：存量节点迁移后为 NULL，前端渲染为"未分组"；删除 stage 时节点回退为 NULL（`ON DELETE SET NULL`）。

### 2.3 约束规则

- 边只在阶段内部连接节点（intra-stage DAG），跨 stage 的边创建返回 400。
- `stage.sort_order` 决定阶段的宏观执行顺序。
- 一个 stage 包含零到多个 node；一个 node 属于零或一个 stage。
- 阶段间数据流是隐式的：前一阶段所有终点节点的输出自动成为下一阶段起点节点的输入。

### 2.4 TypeScript 类型

```typescript
// packages/core/types/workflow.ts
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

// WorkflowNode 新增可选字段:
// stageId?: string | null;
```

### 2.5 迁移

Migration `125_workflow_stage`：
1. 创建 `multica_workflow_stage` 表
2. `workflow_node` 新增 `stage_id` 列
3. 创建索引

## 3. API 设计

### 3.1 响应扩展

`GET /api/workflows/{id}` 响应包含 `stages` 数组和节点的 `stage_id`：

```json
{
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

### 3.2 Stage CRUD

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/workflows/{id}/stages` | 创建 stage |
| `PUT` | `/api/workflows/{id}/stages/{stageId}` | 更新 stage（name, description） |
| `DELETE` | `/api/workflows/{id}/stages/{stageId}` | 删除 stage（节点 → NULL） |
| `PUT` | `/api/workflows/{id}/stages/reorder` | 批量更新 `sort_order` |
| `PUT` | `/api/workflows/{id}/nodes/{nodeId}/stage` | 将节点分配到 stage（或移除） |

### 3.3 边校验

`CreateWorkflowEdge` / `UpdateWorkflowEdge` 校验 `source_node_id` 和 `target_node_id` 属于同一 `stage_id`，跨 stage 边返回 400。

## 4. 页面架构

### 4.1 路由与视图切换

`/workflows/[id]` 默认展示全景图。

| 路由 | 说明 |
|------|------|
| `/workflows/[id]` | 全景图（默认视图） |
| `/workflows/[id]/editor` | 编辑器视图 |
| `/workflows/[id]/runs` | 运行历史（不变） |
| `/workflows/[id]/runs/[runId]` | 运行详情（不变） |

| Aspect | Panorama (default) | Editor |
|--------|--------------------|--------|
| 主视图 | stage 泳道 + SVG 流程连线 | 全局节点 DAG 编辑器 |
| 节点 DAG | 只读，连续流程图画布 | 可编辑，ReactFlow |
| 阶段管理 | 添加/删除/排序/分配节点 | `NodeConfigPanel` 含 stage 下拉 |
| 节点配置 | 只读查看（右侧滑出面板） | 可编辑 worker/critic/schema |

### 4.2 文件结构

```
packages/views/workflows/components/overview/
├── index.ts                                ← 统一导出
├── workflow-panorama-page.tsx              ← 全景图主页面（默认视图）
├── stage-lane.tsx                          ← 阶段泳道行（半透明背景）
├── compact-node-card.tsx                   ← 紧凑节点卡片（224×64px, h-16 w-56）
├── critic-badge.tsx                        ← 评估器小卡片（虚线边框, 144×48px, h-12 w-36）
├── panorama-svg-overlay.tsx                ← SVG 连线 overlay（核心）
├── architecture-detail-panel.tsx           ← 右侧滑出详情面板（520px, w-[520px]）
│
├── workflow-overview-page.tsx              ← 原始概览页（保留，非默认）
├── stage-canvas.tsx                        ← 横向阶段卡片条
├── stage-card.tsx                          ← 阶段卡片
├── stage-node-dag.tsx                      ← 只读 ReactFlow DAG
├── node-detail-panel.tsx                   ← 节点详情抽屉
├── stage-create-dialog.tsx                 ← 阶段创建/编辑对话框
│
└── *.test.tsx                              ← 各组件测试
```

原始 `WorkflowOverviewPage` 及其子组件（`stage-canvas`, `stage-card`, `stage-node-dag`, `node-detail-panel`）保留作为次要视图，全景图作为默认视图。

## 5. 全景图组件架构

### 5.1 页面布局

```
┌─────────────────────────────────────────────────────┐
│  PageHeader (shrink-0)                               │
│  Workflow title            [viewToggle]              │
├─────────────────────────────────────────────────────┤
│  PanoramaCanvas (flex-1, flex flex-col,              │
│                  min-h-0, overflow-auto, relative)   │
│                                                      │
│  ┌─ PanoramaSvgOverlay (absolute, pointer-events: none) ─┐
│  │  <svg>                                              │ │
│  │    edge paths + arrowhead markers                   │ │
│  │  </svg>                                             │ │
│  └────────────────────────────────────────────────────┘ │
│                                                      │
│  ┌── 8px 渐变过渡带 (h-2) ────────────────────────┐  │
│  ┌─ StageLane: Intake ─────────────────────────────┐ │
│  │  [CompactNodeCard] → [CompactNodeCard]           │ │
│  │    ↓ critic (inline, compact)                    │ │
│  └────────────────────────────────────────────────┘ │
│  ┌── 8px 渐变过渡带 (h-2) ────────────────────────┐  │
│  ┌─ StageLane: Analysis ──────────────────────────┐ │
│  │  [CompactNodeCard] → [CompactNodeCard]           │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
└─────────────────────────────────────────────────────┘
```

### 5.2 组件树

```
WorkflowPanoramaPage                              ← entry point
├── PageHeader + viewToggle
├── PanoramaCanvas (relative, overflow-auto)
│   ├── PanoramaSvgOverlay (absolute, pointer-events: none)
│   │   └── <svg> edge paths + markers
│   ├── StageTransitionBar (8px gradient, h-2)
│   ├── StageLane[]
│   │   ├── Stage header（两行垂直堆叠：Stage N 用 text-[10px]，名称用 text-xs）
│   │   ├── CompactNodeCard[]（横向排列，节点 row 内 gap-8，justify-evenly）
│   │   │   └── click → onCardClick(nodeId, "worker")
│   │   └── CriticBadge[]（inline, 虚线边框, gap-5）
│   │       └── click → onCardClick(nodeId, "critic")
│   └── StageTransitionBar
└── ArchitectureDetailPanel（右侧滑出, 520px）
    ├── Plugin 详情 (名称, slug, bundle, skills)
    └── 关联 Agent 全量信息 (名称, 描述, 运行时, 状态, 模型, etc.)
```

## 6. 核心组件规范

### 6.1 StageLane

**视觉**：
- 半透明色带背景（6 色循环），前两个 `/70` 不透明度，后四个 `/45`
- 头部两行垂直堆叠：上行 `Stage {sort_order+1}` 用 `text-[10px] font-medium uppercase`，下行 stage 名称用 `text-xs font-semibold`，两行间用 `mt-1` 间隔
- 头部左侧有 `border-r border-border/50` 竖线分隔，右侧为节点区域（`grid-cols-[112px_minmax(960px,1fr)]`）
- 内边距：`px-3 py-3`，边框：`border-y border-border/60`
- 无圆角、无阴影
- `data-testid="stage-lane-{id}"`

**色系**（来自 `stage-lane.tsx` 的 `STAGE_BG_COLORS`）：
```typescript
const STAGE_BG_COLORS = [
  "bg-slate-50/70",
  "bg-stone-50/70",
  "bg-blue-50/45",
  "bg-rose-50/45",
  "bg-violet-50/45",
  "bg-amber-50/45",
] as const;
```

### 6.2 渐变过渡带

相邻 Stage Lane 之间使用 `h-2`（8px）渐变过渡条（从上一 stage 色到下一 stage 色），`data-testid="stage-transition-gradient"`：

```typescript
const STAGE_TRANSITION_GRADIENTS = [
  "bg-gradient-to-b from-slate-50/40 to-stone-50/40",
  "bg-gradient-to-b from-stone-50/40 to-blue-50/35",
  "bg-gradient-to-b from-blue-50/35 to-rose-50/35",
  "bg-gradient-to-b from-rose-50/35 to-violet-50/35",
  "bg-gradient-to-b from-violet-50/35 to-amber-50/35",
  "bg-gradient-to-b from-amber-50/35 to-slate-50/40",
] as const;
```

注意：过渡带的不透明度值（`/40`, `/35`）与 StageLane 背景色（`/70`, `/45`）是独立的两组常量，设计上允许差异。

连线会自然穿过过渡带区域。

### 6.3 CompactNodeCard

**视觉**（来自 `compact-node-card.tsx`）：
- 尺寸：224×64px（`h-16 w-56`）
- 圆角卡片（`rounded-lg`），白色背景，带边框（`border border-slate-300/90`）
- 阴影：`shadow-[0_1px_2px_rgba(15,23,42,0.08)]`
- 内边距：`p-2.5`，内容垂直排列（`flex flex-col gap-1.5`）
- 显示：插件名（truncated, `text-xs font-semibold`）、Agent 状态点（`h-1.5 w-1.5 rounded-full`）+ Agent 名称/类型（`text-[11px] text-muted-foreground`）
- 不再显示：`ArrowUpRight` 图标、Plugin badge、描述文字、model 名称
- hover：`-translate-y-0.5` 上移 + 边框变 `border-primary/45` + 加深阴影
- `data-testid="compact-node-card-{id}"`
- `aria-pressed` 无障碍属性

**选中态**：
```css
border-primary/55
bg-background
shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]
```

### 6.4 CriticBadge

**视觉**（来自 `critic-badge.tsx`）：
- 尺寸：144×48px（`h-12 w-36`）
- 虚线边框（`border border-dashed border-border/70`），半透明背景（`bg-muted/30`）
- 内边距：`p-1.5`，圆角 `rounded-md`
- 顶部标签行：`ShieldAlert` 图标 + "Critic" 文字（`text-[10px] font-medium uppercase`）
- 底部名称行：truncated `text-xs font-semibold`
- `data-testid="critic-badge-{id}"`
- 与 Worker 卡片的连线由 PanoramaSvgOverlay 统一管理

### 6.5 PanoramaSvgOverlay

**职责**：根据节点 DOM 位置绘制所有 edge 连线。

- 绝对定位 `absolute inset-0`，覆盖画布容器
- `pointer-events: none`，不干扰节点点击
- 通过 `ResizeObserver` + `getBoundingClientRect` 获取节点位置
- 使用 `<svg>` + `<path>` 绘制连线，带 `<marker>` 箭头

**连线规则**（来自 `computeEdgePaths()` 函数，全部使用 `L` (lineto) 正交直线段）：

| 连线类型 | 画法 |
|----------|------|
| 同 stage 内相邻节点 | 正交水平线：`M x1 y1 L midX y1 L midX y2 L x2 y2` |
| 同 stage 内非相邻（arc） | 上方绕行正交线：从源右边缘 → 上方通道 → 目标左边缘 |
| 跨 stage edge | 正交通道线：`M x1 y1 L x1 channelY L channelX channelY L x2 channelY L x2 y2`，多线自动错开（±18px） |
| Worker → Critic | 短直线：`M x1 y1 L x2 y2` |

**连线视觉参数**：
- 线宽 `strokeWidth={1.5}`
- 颜色 `stroke="currentColor"`，透明度 `opacity="0.35"`，继承所属 stage 色系
- critic 分支线 `strokeDasharray="4 3"`
- 所有路径均为正交直线段（`L` 命令），无贝塞尔曲线

### 6.6 ArchitectureDetailPanel

**视觉**（来自 `architecture-detail-panel.tsx`）：
- **触发**：点击 CompactNodeCard 或 CriticBadge
- **位置**：右侧滑出抽屉，宽度 520px（`w-[520px]`），`fixed right-0 top-0 bottom-0 z-50`
- **背景**：`bg-background/98` + `backdrop-blur` + 阴影
- **遮罩**：`fixed inset-0 z-40 bg-slate-950/18 backdrop-blur-[1px]`，点击关闭
- **关闭**：遮罩点击、× 按钮、`Esc` 键
- **内容上半部分**：Plugin 详情（名称、slug、bundle、skills）
- **内容下半部分**：关联 Agent 全量信息（名称、描述、运行时模式、状态、模型、思考级别、可见性、最大并发数、指令、MCP 配置等）
- **底部按钮**：「在编辑器中打开」切换到 Editor 视图

## 7. 数据流与状态管理

### 7.1 查询层

```typescript
// 全景图使用的查询（来自 packages/core/workflows/queries.ts）
useQuery(workflowOverviewOptions(wsId, workflowId))   // workflow 基本信息
useQuery(workflowStagesOptions(wsId, workflowId))      // stages 列表
useQuery(workflowNodesOptions(wsId, workflowId))       // nodes 列表
useQuery(workflowEdgesOptions(wsId, workflowId))       // edges 列表
useQuery(agentListOptions(wsId))                        // agents（含 plugin 信息）
useQuery(builtinPluginListOptions(wsId))               // builtin plugins

// Stage 变更 mutations
useCreateStage()
useUpdateStage()
useDeleteStage()
useReorderStages()
useAssignNodeToStage()
```

### 7.2 页面级状态（useState）

| State | Type | 说明 |
|-------|------|------|
| `selection` | `{ nodeId, focus: "worker" \| "critic" } \| null` | 当前选中（触发 detail panel） |

纯浏览态，不持久化，不放入 Zustand。

### 7.3 视图切换（Zustand）

`useWorkflowViewStore` 管理当前 workflow 详情页的视图模式（`panorama` vs `editor`），per workspace 持久化。

## 8. 间距规范

| 层级 | 值 |
|------|-----|
| 画布边缘 | 12px（`p-3`） |
| 节点 row 内卡片间距 | 32px（`gap-8`, `justify-evenly`） |
| 节点卡片内间距 | 10px（`p-2.5`） |
| Worker-Critic 垂直间距 | 20px（`gap-5`） |
| Stage 过渡带 | 8px（`h-2`） |
| Stage 内边距（水平/垂直） | 12px / 12px（`px-3 py-3`） |
| 容器最大宽 | 无限制（自适应） |
| StageLane 最小高 | 108px（`min-h-[108px]`） |
| 节点 row 最小宽 | 960px（`min-w-[960px]`） |

## 9. 数据映射

| 视觉元素 | 数据来源 | 说明 |
|----------|----------|------|
| 泳道行 (Stage) | `multica_workflow_stage` | 按 `sort_order` 排列 |
| 节点卡片 | `multica_workflow_node` | 每个 node = 一个卡片，通过 `worker_id` → agent → plugin 获取 plugin 信息 |
| Agent 信息 | `multica_agent` | 在详情面板中显示 |
| 评估器 | `multica_workflow_node` 中 `critic_id` 或 `critic_api_url` 非空 | 虚线边框小卡片 |
| 连线 | `multica_workflow_edge` | 跨 stage edge 和 stage 内 edge 均由 SVG overlay 绘制 |

## 10. 边界情况

| 场景 | 处理 |
|------|------|
| Workflow 无 stage | 画布居中显示"尚未定义阶段" + 引导按钮 |
| Stage 无节点 | 紧凑空状态提示（`h-16`, `text-[11px]`）："No plugins in this stage" |
| 节点无 worker/critic 配置 | 详情面板显示"未配置" |
| 旧节点（stage_id = NULL） | 单独渲染"未分组"虚拟行 |
| 大量节点（stage 内 >6） | `overflow-x: auto`，横向滚动 |
| 跨 stage 无 edge | 不画隐含连线 |
| resize | ResizeObserver 自动重新测量和绘制 SVG |
| 删除含节点的 stage | 确认弹窗后节点移入"未分组" |
| Workflow 不存在 | 标准 404 |
| 无 workspace 访问 | 标准 NoAccessPage |

## 11. i18n

关键 key（命名空间 `workflows`）：

```json
{
  "panorama": {
    "stage_n_of_m": "Stage {{n}}/{{m}}",
    "nodes_count": "{{count}} node",
    "nodes_count_plural": "{{count}} nodes",
    "empty_stage": "No plugins in this stage",
    "empty_all": "Add nodes in the editor",
    "unassigned": "Unassigned",
    "detail_panel": {
      "title": "Node Details",
      "worker": "Worker",
      "critic": "Critic",
      "format_schema": "Format Schema",
      "relations": "Relations",
      "plugins": "Plugins",
      "skills": "Skills",
      "not_configured": "Not configured",
      "open_in_editor": "Open in editor"
    },
    "stage_dialog": {
      "create_title": "Create Stage",
      "edit_title": "Edit Stage",
      "delete_confirm": "Deleting this stage will move its {{count}} node(s) to \"Unassigned\"."
    }
  }
}
```

## 12. 测试

### Go 后端测试

- Stage CRUD（create, update, delete, reorder）
- 节点分配到 stage / 取消分配
- 跨 stage edge 校验（返回 400）
- `GET /workflows/{id}` 响应包含 `stages` 数组
- `ON DELETE SET NULL`：删除 stage 后节点 stage_id 为 NULL
- 权限校验

### 前端组件测试（`packages/views/workflows/components/overview/`）

- `panorama-page.test.tsx`：stage lanes 渲染、节点渲染、SVG overlay 存在
- `stage-lane.test.tsx`：半透明背景、紧凑头部、节点排列
- `compact-node-card.test.tsx`：尺寸、精简内容、点击交互
- `panorama-svg-overlay.test.tsx`：连线路径生成逻辑
- `critic-badge.test.tsx`：尺寸变化适配
- `architecture-detail-panel.test.tsx`：plugin 详情 + agent 信息渲染

## 13. 复用

| 复用项 | 来源 | 用途 |
|--------|------|------|
| Query options（stages/nodes/edges） | `packages/core/workflows/queries.ts` | 数据查询 |
| Agent 查询 | `packages/core/agents/queries.ts` | agent + plugin 信息 |
| `useWorkflowViewStore` | `packages/core/workflows/stores/view-store.ts` | 视图切换持久化 |
| `NavigationAdapter` | `packages/views/navigation/` | 路由跳转 |

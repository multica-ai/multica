# Issue 详情页 — Workflow 执行全景图设计

**Status:** Implemented
**Last updated:** 2026-06-29
**Reference documents:**
- `docs/issue需求文档.md`（用户需求）
- `docs/issue-data-model-analysis.md`（数据模型分析）
- `docs/superpowers/specs/workflow-stage-panorama-design.md`（Workflow Panorama 参考实现）

## 1. 概述

当 Issue 关联了 Workflow（`assignee_type = 'workflow'`）时，Issue 详情页默认进入**全屏模式**——仅展示 Issue Header + Workflow 执行全景图，桌面端隐藏 `ResizablePanelGroup` 侧边栏，全景图以 `flex-1` 填充全部可用空间。

PageHeader 操作栏提供**全屏/详情切换按钮**（`Maximize2`/`Minimize2` 图标），仅在关联 Workflow 时显示。切换到**详情模式**后，全景图嵌入在**描述区块下方、子 Issue 列表上方**（类似旧 `WorkflowDagViewer` 的位置），描述、子 Issue 列表、评论、活动日志等内容全部恢复显示。

全景图是一个类似 Workflow Panorama 的阶段泳道视图，渲染该 Workflow **运行时**（`workflow_node_run`）的状态叠加在模板节点上的动态图。

未关联 Workflow 的 Issue 无切换按钮，保持现有详情页不变（描述 + 子 Issue + 评论 + 活动日志），始终显示右侧 Sidebar。

### 用户旅程

1. 用户进入 Issue 列表页（默认不显示 `origin_type = 'workflow'` 的自动生成子 Issue）
2. 点击一个 Issue 进入详情页
3. 如果该 Issue 关联了 Workflow，详情页**默认进入全屏模式**：仅展示 Issue Header + Workflow 执行全景图，桌面端无右侧 Sidebar
4. 全屏模式下全景图以 `flex-1` 填充可用空间，点击节点卡片 → 右侧滑出详情面板，查看 Worker/Critic 信息、产物、状态机详情
5. 点击 PageHeader 的 **全屏/详情切换按钮**（`Minimize2` 图标）切换到**详情模式**：全景图嵌入在描述和子 Issue 之间，右侧 Sidebar 恢复显示，描述、子 Issue、评论、活动日志等内容均可正常查看和交互
6. 详情模式下点击切换按钮（`Maximize2` 图标）回到全屏模式
7. 未关联 Workflow 的 Issue 无切换按钮，保持标准详情页布局

## 2. 数据模型

### 2.1 子 Issue 过滤

Issue 列表 API 默认排除 `origin_type = 'workflow'` 的自动生成子 Issue。API 内部支持 `include_workflow_origin` 参数（默认 `false`），前端暂不暴露切换开关。

### 2.2 API 设计

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/issues?include_workflow_origin=false` | Issue 列表（默认排除子 Issue） |

Issue 详情页的 Workflow 执行数据通过已有 API 获取，无需新增：

- `GET /api/workflows/{id}` → 模板 stages + nodes + edges
- `GET /api/workflows/{id}/runs/{runId}/node-runs` → 所有 node_run 运行时状态

## 3. 页面架构

### 3.1 Issue 详情页改造

当 Issue **关联 Workflow** 时，默认进入**全屏模式**：

```
┌──────────────────────────────────────────────────────────────────────┐
│ ← Issues                    Issue Detail          [Minimize2] [Sidebar]│
├──────────────────────────────────────────────────────────────────────┤
│                                                                       │
│ ┌─────────────────────────────────────────────────────────────────┐  │
│ │ Issue Header                                                     │  │
│ │ MUL-42 产品登录页重构                                              │  │
│ │ ◉ in_progress                                                    │  │
│ └─────────────────────────────────────────────────────────────────┘  │
│                                                                       │
│ ┌── 全景图 (flex-1, 填充剩余空间) ──────────────────────────────────┐ │
│ │                                                                    │ │
│ │ ┌─ 需求分析 ──────────────────────────────────────────────────┐   │ │
│ │ │ [需求收集]        [技术评审]                                  │   │ │
│ │ │  ✓ 小助手          ◉ 小助手                                   │   │ │
│ │ └──────────────────────────────────────────────────────────────┘   │ │
│ │ ┌─ 开发 ──────────────────────────────────────────────────────┐   │ │
│ │ │ [编码]            [Code Review]                               │   │ │
│ │ │  ✓ 后端            ⛔ blocked                                 │   │ │
│ │ └──────────────────────────────────────────────────────────────┘   │ │
│ │ SVG 连线层 (absolute)                                               │ │
│ └────────────────────────────────────────────────────────────────────┘ │
│                                                                       │
│ ┌────────────────────────────────────────────────────────────────┐   │
│ │ ExecutionDetailPanel (右侧滑出, 520px, 点击节点触发)              │   │
│ └────────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

**全屏模式特点**：
- 无线程侧边栏（`ResizablePanelGroup` 不渲染）
- 全景图以 `flex-1 min-h-0 flex flex-col` 填充 viewport
- 描述、子 Issue、评论、活动日志全部隐藏
- PageHeader 显示 `Minimize2` 图标，tooltip 显示"详情模式"

点击切换按钮后进入**详情模式**：

```
┌────────────────────────────────────────────────────────────┐
│ ← Issues                    Issue Detail          [Maximize2] │
├────────────────────────────────────────────────────────────┤
│                                                             │
│ ┌────────────────────────────┐ ┌─────────────────────────┐ │
│ │ Issue Header               │ │ 右侧 Sidebar            │ │
│ │ MUL-42 产品登录页重构       │ │ ┌─ Properties ────────┐ │ │
│ │ ◉ in_progress              │ │ │ Status              │ │ │
│ ├────────────────────────────┤ │ │ Assignee (workflow) │ │ │
│ │                            │ │ │ Stage               │ │ │
│ │ ┌── 描述区 (max-w-5xl) ───┐│ │ │ Project             │ │ │
│ │ │ TitleEditor              ││ │ │ Priority            │ │ │
│ │ │ Description              ││ │ │ Due Date            │ │ │
│ │ │ ReactionBar              ││ │ │ Labels              │ │ │
│ │ └──────────────────────────┘│ │ └─────────────────────┘ │ │
│ │                            │ │                          │ │
│ │ ┌── 全景图 (全宽) ─────────┐│ │ ┌─ Activity ──────────┐ │ │
│ │ │[border-y bg-muted/20]    ││ │ │ ...                  │ │ │
│ │ │                          ││ │ └──────────────────────┘ │ │
│ │ │ ┌─ 需求分析 ──────────┐  ││ │                           │ │
│ │ │ │ [需求收集] [技术评审]  │  ││ │                           │ │
│ │ │ │  ✓ 小助手    ◉ 小助手  │  ││ │                           │ │
│ │ │ └──────────────────────┘  ││ │                           │ │
│ │ │ ┌─ 开发 ──────────────┐  ││ │                           │ │
│ │ │ │ [编码]   [Code Review] │  ││ │                           │ │
│ │ │ │  ✓ 后端   ⛔ blocked   │  ││ │                           │ │
│ │ │ └──────────────────────┘  ││ │                           │ │
│ │ │ SVG 连线层 (absolute)     ││ │                           │ │
│ │ └──────────────────────────┘│ │                           │ │
│ │                            │ │                           │ │
│ │ ┌── 协作区 (max-w-5xl) ───┐│ │                           │ │
│ │ │ Sub-issues               ││ │                           │ │
│ │ │ Activity / Comments      ││ │                           │ │
│ │ │ CommentInput             ││ │                           │ │
│ │ └──────────────────────────┘│ │                           │ │
│ └────────────────────────────┘ └─────────────────────────┘ │
│                                                             │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ ExecutionDetailPanel (右侧滑出, 520px, 点击节点触发)    │   │
│ └──────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────┘
```

**详情模式特点**：
- 标准 `ResizablePanelGroup` 布局，右侧 Sidebar 正常渲染
- 全景图以 `border-y bg-muted/20 py-6` 背景条带嵌入，在描述区和协作区之间
- 文本区域（描述、子 Issue、评论）使用 `max-w-5xl` 左对齐
- PageHeader 显示 `Maximize2` 图标，tooltip 显示"全屏模式"

当 Issue **未关联 Workflow** 时，全景图不渲染，切换按钮不显示，布局为标准详情页（描述 → 子 Issue → 评论 + 右侧 Sidebar）。

### 3.2 文件结构

```
packages/views/issues/components/execution/
├── index.ts                              ← 统一导出
├── execution-panorama-page.tsx           ← 执行全景图主组件（替换 WorkflowDagViewer）
├── runtime-node-card.tsx                 ← 运行时节点卡片（WorkflowNode + NodeRun 叠加）
├── node-run-status-icon.tsx              ← 16 状态 → 图标映射
├── execution-detail-panel.tsx            ← 右侧滑出详情面板（节点下钻）
├── artifact-list.tsx                     ← 产物列表子组件
└── *.test.tsx                            ← 各组件测试

```

**复用 Panorama 组件**（`packages/views/workflows/components/overview/`）：

| 组件 | 复用/改造 |
|------|----------|
| `StageLane` | 复用结构 + Props 扩展支持 Issue 模式 |
| `StageTransitionBar` | 直接复用 |
| `PanoramaSvgOverlay` | 复用连线逻辑，适配 node_run 节点位置 |

不复用的组件（信息密度/交互不同，新建）：
- `RuntimeNodeCard` ← 替代 `CompactNodeCard`
- `ExecutionDetailPanel` ← 替代 `ArchitectureDetailPanel`

### 3.3 组件树

```
IssueDetail
├── hasWorkflow = issue.assignee_type === "workflow" && !!issue.assignee_id
├── isFullscreen = useState(hasWorkflow), 跟随 hasWorkflow 变化自动同步
├── PageHeader（全宽, flex-shrink-0）
│   ├── 面包屑 + 标题 + 操作按钮
│   ├── [hasWorkflow] 全屏/详情切换按钮 ← 新增
│   │   ├── 全屏模式：Minimize2 图标，tooltip "详情模式"
│   │   └── 详情模式：Maximize2 图标，tooltip "全屏模式"
│   └── 其他操作按钮（Pin、Sidebar 切换等）
│
├── [全屏模式] detailContent（无 ResizablePanelGroup，无 sidebar）
│   └── scroll container (relative flex-1 overflow-y-auto)
│       ├── IssueHeader
│       └── 全景图容器 (flex-1 min-h-0 flex flex-col)
│           └── <div flex-1 min-h-0 py-6>
│               └── ExecutionPanoramaPage
│                   ├── PanoramaSvgOverlay（absolute, pointer-events: none）
│                   ├── StageTransitionBar[]
│                   ├── StageLane[]（mode="runtime"）
│                   │   ├── StageHeader
│                   │   ├── RuntimeNodeCard[]
│                   │   └── EmptyStageHint
│                   └── UnassignedLane
│
└── [详情模式] ResizablePanelGroup (orientation="horizontal", flex-1 min-h-0)
    ├── ResizablePanel id="content" minSize="50%"
    │   └── detailContent
    │       └── scroll container (relative flex-1 overflow-y-auto)
    │           ├── [!isFullscreen] 描述区 (w-full max-w-5xl px-8 pt-8)
    │           │   ├── TitleEditor
    │           │   ├── parent issue link (如果有)
    │           │   ├── originNodeRun controls (如果有)
    │           │   ├── ContentEditor (Description)
    │           │   └── ReactionBar + FileUploadButton
    │           ├── {hasWorkflow ? (
    │           │     <全景图容器 (w-full border-y bg-muted/20 py-6)>
    │           │       <div px-6>
    │           │         <ExecutionPanoramaPage />
    │           │           ├── ...（同上）
    │           │       </div>
    │           │     </全景图容器>
    │           │   ) : null}
    │           └── [!isFullscreen] 协作区 (w-full max-w-5xl px-8 pb-8)
    │               ├── Sub-issues list
    │               ├── Divider
    │               ├── Activity section (AgentLiveCard + Timeline + Comments)
    │               └── CommentInput
    └── ResizablePanel id="sidebar" minSize="25%" defaultSize="35%" maxSize="50%"
        └── IssueSidebar

ExecutionDetailPanel（fixed right-0 top-0 bottom-0 w-[520px] z-50, 点击节点触发）
```

**关键条件渲染逻辑**：
- `isFullscreen` 默认值 = `hasWorkflow`，Workflow 变化时自动同步
- 全屏模式：`!isFullscreen` 为 false，描述区和协作区不渲染；ResizablePanelGroup 不渲染
- 详情模式：`!isFullscreen` 为 true，描述区和协作区正常渲染；ResizablePanelGroup 正常渲染
- 非 Workflow Issue：`hasWorkflow` 为 false，无切换按钮，始终走详情模式路径

## 4. 核心组件规格

### 4.1 RuntimeNodeCard

**数据来源**：`WorkflowNode`（模板） + `WorkflowNodeRun`（运行时状态）。`nodeRun` 为 `null` 时（含 `workflow_run_id` 为空、或节点尚未被推进）显示"未启动"：左侧无色带，状态图标为空心 `Circle`，Worker/Critic 行显示 `--`。

**视觉**：
- 尺寸：最小 240×104px（`min-w-[240px] min-h-[104px]`），内容溢出时自动增高
- 白色背景，圆角 `rounded-lg`，边框 `border border-border/80`
- 阴影：`shadow-[0_1px_2px_rgba(15,23,42,0.06)]`
- 内边距：`p-3`
- 左侧 3px 状态色带（无 node_run 时不显示）
- `data-testid="runtime-node-card-{nodeId}"`

**左侧色带颜色**（由 node_run 状态决定）：

| 状态分类 | 颜色 | NodeRun 状态 |
|----------|------|-------------|
| 完成 | `green-500` | `completed`, `critic_approved` |
| 进行中 | `blue-500` | `format_checking`, `working`, `critic_reviewing` |
| 等待/就绪 | `amber-500` | `pending`, `format_ok`, `worker_assigned`, `awaiting_input`, `awaiting_critic` |
| 驳回重做 | `orange-500` | `critic_rework` |
| 失败/阻塞 | `red-500` | `failed`, `blocked`, `format_failed` |
| 跳过/取消 | `muted` | `skipped`, `cancelled` |

**内容布局**：

```
┌──────────────────────────────────┐
│ ∎ 需求收集                ✓ 已完成│  ← 节点名称 + 状态
│                                  │
│ [Bot] Worker: agent 小助手   ✓    │  ← Worker 行（类型图标 + 名称 + 状态）
│ [User] Critic: human 张伟    ✓    │  ← Critic 行（有 critic 配置时）
│                                  │
│ 产物：需求文档.md, 技术方案评审    │  ← 产物名称列表（artifact_count > 0）
└──────────────────────────────────┘
```

**各行规格**：

| 行 | 内容 | 样式 |
|----|------|------|
| 第一行 | 节点名称 + 状态徽章 | `text-sm font-medium truncate` + 右侧状态 icon |
| Worker 行 | 类型图标（`Bot` / `User`）+ `agent/member` 标签 + 名称 + 状态 icon | `text-[11px]`，gap-2，h-6 |
| Critic 行 | 同上，仅当 `critic_type` 非空时显示 | `text-[11px]`，gap-2，h-6 |
| 产物行 | 附件图标 + 产物名称列表（逗号分隔，单行截断） | `text-[11px] text-muted-foreground`，仅当 artifact_count > 0 |

**交互**：hover 上移 + 边框变色；点击触发 ExecutionDetailPanel。

### 4.2 NodeRun 状态图标映射

16 种 NodeRun 状态 → 11 种视觉态：

| 视觉态 | 图标 | 颜色类 | NodeRun 状态 |
|--------|------|--------|-------------|
| 等待 | `Circle`（空心） | `text-muted-foreground/40` | `pending` |
| 校验中 | `Loader2`（旋转） | `text-blue-500` | `format_checking` |
| 校验通过 | `CheckCircle2` | `text-amber-500` | `format_ok` |
| 操作进行中 | `Loader2`（旋转） | `text-blue-500` | `working`, `critic_reviewing` |
| 已分配 | `UserCheck` | `text-amber-500` | `worker_assigned` |
| 暂停等待 | `Clock` | `text-amber-500` | `awaiting_input`, `awaiting_critic` |
| 驳回重做 | `RotateCcw` | `text-orange-500` | `critic_rework` |
| 审核通过 | `CheckCircle2` | `text-green-500` | `critic_approved` |
| 失败/阻塞 | `AlertCircle` | `text-red-500` | `failed`, `blocked`, `format_failed` |
| 完成 | `CheckCircle2` | `text-green-500` | `completed` |
| 跳过/取消 | `MinusCircle` | `text-muted-foreground` | `skipped`, `cancelled` |

### 4.3 StageLane（扩展）

在 Workflow Panorama `StageLane` 基础上新增 props：

```typescript
interface StageLaneProps {
  // 现有 Panorama props
  stage: WorkflowStage;
  nodes: WorkflowNode[];
  agentMap: Map<string, Agent>;
  // 新增 Issue 模式 props
  mode?: "template" | "runtime";        // 默认 "template"
  nodeRuns?: Map<string, WorkflowNodeRun>; // nodeId → nodeRun
  onNodeClick?: (nodeId: string) => void;
}
```

`mode="runtime"` 时，`StageLane` 渲染 `RuntimeNodeCard` 替代 `CompactNodeCard`。

### 4.4 ExecutionDetailPanel

**触发**：点击 `RuntimeNodeCard`

**位置**：右侧滑出抽屉，宽度 520px（`w-[520px]`），`fixed right-0 top-0 bottom-0 z-50`

**背景**：`bg-background/98` + `backdrop-blur` + 左侧阴影

**遮罩**：`fixed inset-0 z-40 bg-slate-950/18 backdrop-blur-[1px]`，点击/ESC 关闭

**内容分区**（从上到下）：

| 区块 | 内容 |
|------|------|
| Header | 节点名称 + 状态徽章 + 关闭按钮 |
| 状态机路径 | 当前状态在 Format → Worker → Critic 流水线中的位置（可视化步骤条） |
| Worker | 类型 + 名称 + 状态 + 输出摘要（截断 3 行） |
| Critic | 类型 + 名称 + 审核结果/评论（截断 3 行），无配置时显示"未配置" |
| 产物列表 | 按来源分组：Worker 输出 / Critic 输出 / 附件 |
| 元数据 | 开始时间、结束时间、耗时、重试次数、错误信息 |
| 底部操作 | "查看完整 Issue" 链接 + 条件操作按钮（解除阻塞/重试等） |

### 4.5 产物列表 (ArtifactList)

**数据来源**：`node_run.worker_output`（JSONB）+ `node_run.critic_output`（JSONB）+ `multica_attachment`（关联该 Issue 且来源为此 node_run）。

每条产物展示：
- 名称（truncated）
- 来源标签（`output` / `file`，Tiny badge `text-[10px]`）
- 生成者（agent/member 名称）
- 生成时间（relative time）
- 可点击：JSONB output → 展开查看，attachment → 下载/预览

## 5. 状态管理

### 5.1 数据查询

```typescript
// ExecutionPanoramaPage 中的查询
useQuery(workflowDetailOptions(wsId, issue.workflow_id))     // Workflow 模板
useQuery(workflowStagesOptions(wsId, issue.workflow_id))     // 模板阶段定义
useQuery(workflowNodesOptions(wsId, issue.workflow_id))      // 模板节点
useQuery(workflowEdgesOptions(wsId, issue.workflow_id))      // 节点间连线
useQuery(workflowNodeRunsOptions(wsId, issue.workflow_id, issue.workflow_run_id))
  // node_run 运行时状态（非终端状态每 5s 自动刷新）
useQuery(agentListOptions(wsId))                              // Agent 信息
useQuery(builtinPluginListOptions(wsId))                     // Plugin 信息
```

### 5.2 页面内状态

| State | 类型 | 说明 |
|-------|------|------|
| `isFullscreen` | `boolean` | 全屏/详情模式切换，默认值 = `hasWorkflow`。Workflow Issue 变化时通过 `useEffect` 同步 |
| `hasWorkflow` | `boolean` | 派生值：`issue?.assignee_type === "workflow" && !!issue?.assignee_id` |
| `selectedNodeId` | `string \| null` | 触发 detail panel 的节点 |

### 5.3 渲染分支

| 条件 | 渲染 |
|------|------|
| `hasWorkflow && isFullscreen` | 无 `ResizablePanelGroup`，无 sidebar；全景图 `flex-1` 填充 viewport |
| `hasWorkflow && !isFullscreen` | `ResizablePanelGroup` + sidebar；描述区、全景图(全宽条带)、协作区依次排列 |
| `!hasWorkflow` | `ResizablePanelGroup` + sidebar；描述区、子 Issue、协作区依次排列，无切换按钮 |

## 6. 布局与间距规范

### 整体布局

| 层级 | 值 |
|------|-----|
| 整体容器 | `flex flex-1 min-h-0`（全屏模式无需 ResizablePanelGroup） |
| 切换按钮位置 | PageHeader 操作栏，`variant="ghost" size="icon-sm"`，紧跟最后一个操作按钮 |
| 切换按钮图标 | 全屏模式 → `Minimize2`（tooltip: 详情模式）；详情模式 → `Maximize2`（tooltip: 全屏模式） |
| 文本内容区最大宽度 | 1024px（`max-w-5xl`） |
| 文本内容区对齐 | 左对齐（`w-full`，无 `mx-auto`） |
| 全景图容器宽度 (详情模式) | 全宽（`w-full`） |
| 全景图容器 (全屏模式) | `flex-1 min-h-0 flex flex-col`，无 border/背景条带 |
| 全景图容器背景 (详情模式) | `bg-muted/20` + `border-y` |
| 全景图容器纵向内边距 (详情模式) | 24px（`py-6`） |
| 全景图容器横向内边距 (详情模式) | 24px（`px-6`） |
| 全景图容器纵向内边距 (全屏模式) | 24px（`py-6`，在内部 div 上） |
| 全景图容器横向内边距 (全屏模式) | 无（全屏模式全景图撑满宽度） |
| 统一横向内边距 | 32px（`px-8`，描述区和协作区） |

### 全景图内部间距

| 层级 | 值 |
|------|-----|
| 画布内边距 | 无（`relative`，无边距） |
| 节点卡片间距 | 32px（`gap-8`, `justify-evenly`，同 Panorama） |
| 卡片外边距 | 12px（`p-3`） |
| 卡片内行间垂直间距 | 8px（`gap-2`，flex-col 行间距） |
| 卡片内行内水平间距 | 8px（`gap-2`，Worker/Critic 行内元素间距） |
| Stage 过渡带 | 8px（`h-2`） |
| Stage 内边距 | 12px / 12px（`px-3 py-3`） |
| StageLane 最小高 | 108px（`min-h-[108px]`，同 Panorama） |
| RuntimeNodeCard 最小宽 | 240px（`min-w-[240px]`） |
| RuntimeNodeCard 最小高 | 104px（`min-h-[104px]`） |
| DetailPanel 宽 | 520px（`w-[520px]`） |

### 滚动行为

| 场景 | 处理 |
|------|------|
| 页面垂直滚动 | 外层 scroll container 的 `overflow-y-auto` 统一处理（全屏和详情模式均适用） |
| 全屏模式全景图 | 内容超出 viewport 时由外层 scroll container 处理垂直滚动 |
| 阶段内水平溢出 | StageLane 内部 `overflow-x-auto` 处理 |
| 全景图画布 | **不再有** `overflow-auto`，由页面统一管理滚动 |

## 7. 边界情况

| 场景 | 处理 |
|------|------|
| Issue 无 `workflow_id` | 全景图不渲染，切换按钮不显示，保持标准详情页布局（描述 + 子Issue + 评论 + 日志 + Sidebar） |
| Issue 有 workflow 但无 `workflow_run_id`（已分配未启动） | 渲染全景图，RuntimeNodeCard（`nodeRun={null}`），全部显示"未启动"（空心 Circle + `--`），无 SVG 连线。默认全屏模式 |
| Issue 有 workflow | 默认全屏模式：仅 Issue Header + 全景图，无 sidebar。切换到详情模式后描述、子 Issue、评论、活动日志全部恢复 |
| 全屏模式 → 详情模式切换 | 描述区、协作区、Sidebar 恢复渲染；全景图容器从 `flex-1` 切换到 `border-y bg-muted/20 py-6` 条带模式 |
| 详情模式 → 全屏模式切换 | 描述区、协作区、Sidebar 隐藏；全景图容器切换到 `flex-1 min-h-0 flex flex-col` 填充模式 |
| 非 Workflow Issue 导航到 Workflow Issue | `useEffect` 检测 `hasWorkflow` 变为 true，自动设置 `isFullscreen = true` |
| Workflow Issue 导航到非 Workflow Issue | `hasWorkflow` 变为 false，切换按钮消失，`isFullscreen` 同步为 false |
| Workflow 无 stage 定义 | 所有节点渲染在 UnassignedLane 中（同 Panorama 行为） |
| Stage 无节点 | 紧凑空状态提示："此阶段暂无节点"（`text-[11px]`） |
| 节点无 worker 配置 | Worker 行显示"未配置"（`text-[11px] text-muted-foreground italic`） |
| 节点无 critic 配置 | 不渲染 Critic 行 |
| 产物为空 | 不显示产物行 |
| 大量节点（>6 同一 stage） | StageLane 内部 `overflow-x-auto` 水平滚动 |
| resize / 窗口变化 | ResizeObserver 触发 SVG 重新绘制（复用 PanoramaSvgOverlay 逻辑） |
| 实时状态更新 | WS 事件 → `invalidateQueries` + 非终端状态 5s 轮询 |
| Issue 列表过滤 | API 默认排除 `origin_type = 'workflow'`，`include_workflow_origin` 参数可用但暂不暴露 |
| 节点被删除 | node_run 仍存在但 node_title 为快照值，卡片显示灰色"已删除"标记 |

## 8. 数据映射

| 视觉元素 | 数据来源 |
|----------|----------|
| Stage 泳道行 | `multica_workflow_stage`（模板阶段） |
| 节点卡片 | `multica_workflow_node`（模板）+ `multica_workflow_node_run`（运行时） |
| 卡片左侧色带 | `node_run.status` → 颜色映射 |
| Worker 信息 | `node_run.worker_type` + `worker_id` → `multica_agent` / `multica_member` |
| Critic 信息 | `node_run.critic_type` + `critic_id` → `multica_agent` / `multica_member` |
| SVG 连线 | `multica_workflow_edge`（复用 PanoramaSvgOverlay） |
| Detail Panel 产物 | `node_run.worker_output` + `node_run.critic_output` + `multica_attachment` |
| Detail Panel 元数据 | `node_run.started_at`, `completed_at`, `retry_count`, `error` |

## 9. i18n

关键 key（命名空间 `issues`）：

```json
{
  "detail": {
    "fullscreen_mode": "Fullscreen",
    "detail_mode": "Detail"
  },
  "execution": {
    "panorama": {
      "not_started": "Not started",
      "no_worker": "No worker configured",
      "no_run": "Workflow not started yet",
      "empty_stage": "No nodes in this stage",
      "unassigned": "Unassigned"
    },
    "card": {
      "worker_label": "Worker",
      "critic_label": "Critic",
      "artifacts_label": "Artifacts"
    },
    "detail_panel": {
      "title": "Node Detail",
      "status_path": "Status Path",
      "worker": "Worker",
      "critic": "Critic",
      "worker_output": "Worker Output",
      "critic_output": "Critic Output",
      "attachments": "Attachments",
      "not_configured": "Not configured",
      "no_output": "No output yet",
      "review_comment": "Review Comment",
      "metadata": "Metadata",
      "started_at": "Started",
      "completed_at": "Completed",
      "duration": "Duration",
      "retry_count": "Retries",
      "error": "Error",
      "view_full_issue": "View full issue"
    },
    "status": {
      "pending": "Pending",
      "format_checking": "Format Checking",
      "format_ok": "Format OK",
      "format_failed": "Format Failed",
      "worker_assigned": "Assigned",
      "working": "Working",
      "awaiting_input": "Awaiting Input",
      "awaiting_critic": "Awaiting Critic",
      "critic_reviewing": "Reviewing",
      "critic_approved": "Approved",
      "critic_rework": "Rework",
      "completed": "Completed",
      "failed": "Failed",
      "blocked": "Blocked",
      "skipped": "Skipped",
      "cancelled": "Cancelled"
    }
  }
}
```

## 10. 测试

### 10.1 Go 后端测试

| 测试 | 内容 |
|------|------|
| Issue 列表默认排除 child issues | `GET /api/issues` 不包含 `origin_type=workflow` |
| Issue 列表显式包含 | `GET /api/issues?include_workflow_origin=true` 包含所有 |

### 10.2 前端组件测试（`packages/views/issues/components/execution/`）

| 测试文件 | 关键用例 |
|----------|----------|
| `execution-panorama-page.test.tsx` | 有 workflow 渲染全景图；无 workflow 不渲染；无 workflow_run_id 显示未启动 |
| `runtime-node-card.test.tsx` | 各种状态映射正确色带；Worker/Critic 信息渲染；Critic 无配置时不显示 Critic 行；产物名称列表显示/不显示；点击事件 |
| `node-run-status-icon.test.tsx` | 16 状态全覆盖；未知状态 fallback |
| `execution-detail-panel.test.tsx` | 状态机路径渲染；Worker/Critic 区块；产物列表分组；遮罩/ESC 关闭 |
| `artifact-list.test.tsx` | Worker/Critic/Attachment 分组；空状态 |


## 11. 复用

| 复用项 | 来源 | 用途 |
|--------|------|------|
| `StageLane` | `packages/views/workflows/components/overview/stage-lane.tsx` | 扩展 mode prop 支持 runtime 模式 |
| `StageTransitionBar` | panorama 组件 | 直接复用 |
| `PanoramaSvgOverlay` | panorama 组件 | 复用连线绘制引擎 |
| `PanoramaCanvas` | panorama 组件 | 复用画布容器 |
| `StageLane` 色系常量 | panorama 组件 | 复用 STAGE_BG_COLORS |
| Query options | `packages/core/workflows/queries.ts` | 数据查询 |
| Agent 查询 | `packages/core/agents/queries.ts` | Agent 名称查询 |
| `parseWithFallback` | `packages/core/api/schemas.ts` | API 响应安全解析 |

## 12. 实施范围

### 包含
- 后端：Issue 列表默认过滤子 Issue
- 前端：ExecutionPanoramaPage + RuntimeNodeCard + ExecutionDetailPanel + StageLane 扩展
- 全屏/详情模式切换：Workflow Issue 默认全屏，PageHeader 切换按钮（Maximize2/Minimize2），桌面端全屏模式隐藏 ResizablePanelGroup
- i18n key（中英文），含 `fullscreen_mode` / `detail_mode`
- 单元测试（Go + TypeScript）

### 不包含
- 前端子 Issue 筛选切换开关（API 参数可用，UI 暂不暴露）
- 拖拽排序
- 实时动画
- Batch 操作

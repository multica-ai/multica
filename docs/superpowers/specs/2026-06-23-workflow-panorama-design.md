# Workflow 研发全景图 — 实现计划

## Context

用户需要一个全新的「研发全景图」页面，作为 workflow 详情页 `/workflows/[id]` 的新默认视图。该页面以泳道卡片风格展示单个 workflow 的 Stage → Agent → Plugin 三层架构，参考 `docs/cospowers-architecture.drawio` 的视觉风格。

**核心决策**：
- 新增 ArchitecturePage 为默认视图（不修改现有 Overview 页面）
- 数据来自 workflow 的 Stage/Node/Agent 模型
- 泳道卡片风格（Stage = 水平行，Plugin = 卡片，Agent = 关联信息）
- 点击 Agent/Plugin 弹出右侧滑出面板

---

## 设计方案

### 1. 数据映射

| 视觉元素 | 数据来源 | 说明 |
|---|---|---|
| 泳道行 (Stage) | `multica_workflow_stage` | 按 `sort_order` 排列 |
| Plugin 卡片 | `multica_workflow_node` (每个 node = 一个 plugin 卡片) | node 通过 worker_id → agent → plugin_id 获取 plugin 信息 |
| Agent 信息 | `multica_agent` (点击卡片后展示) | 作为 plugin 卡片的关联信息，在详情面板中显示 |
| 评估器 | `multica_workflow_node` 中 `critic_type` 非空的节点 | 显示为虚线边框小卡片 |
| 阶段间箭头 | `multica_workflow_edge` | 跨 stage 的 edge 表示数据流 |

### 2. 页面布局

```
┌─────────────────────────────────────────────────────┐
│  [Workflow Title]              [切换到编辑器 ▼]      │
├─────────────────────────────────────────────────────┤
│                                                      │
│  ╔══════════ 需求接入 ══════════════════════════╗    │
│  ║  ┌──────────┐  ┌──────────┐  ┌──────────┐   ║    │
│  ║  │brainstorm│  │session-  │  │using-spec│   ║    │
│  ║  │  ing     │  │context   │  │developer │   ║    │
│  ║  └──────────┘  └──────────┘  └──────────┘   ║    │
│  ╚══════════════════════════════════════════════╝    │
│           ↓ 数据流                                     │
│  ╔══════════ 需求分析 ══════════════════════════╗    │
│  ║  ┌──────────┐         ┌──────────┐          ║    │
│  ║  │requirement│  ────→  │system-   │          ║    │
│  ║  │ analysis │         │requirement│          ║    │
│  ║  └──────────┘         └──────────┘          ║    │
│  ║  ┌─── 评估器 ──┐  ┌─── 评估器 ──┐           ║    │
│  ║  │aireq-      │  │sysreq-     │           ║    │
│  ║  │evaluator   │  │evaluator   │           ║    │
│  ║  └────────────┘  └────────────┘           ║    │
│  ╚══════════════════════════════════════════════╝    │
│           ↓                                           │
│  ... 更多阶段 ...                                     │
│                                                      │
│  点击 Plugin 卡片 → 右侧滑出面板：                    │
│  ┌─────────────────────────────────────────────┐     │
│  │  Plugin 详情                                 │     │
│  │  ├ 名称: cospowers-requirements             │     │
│  │  ├ Slug: ...                                │     │
│  │  ├ Bundle: ...                              │     │
│  │  └ Skills: 9 个                             │     │
│  │                                              │     │
│  │  关联 Agent（全量信息）                      │     │
│  │  ├ 名称: 需求分析                            │     │
│  │  ├ 描述: ...                                │     │
│  │  ├ 头像: ...                                │     │
│  │  ├ 运行时模式: cloud                         │     │
│  │  ├ 运行时: ...                               │     │
│  │  ├ 状态: idle / working / offline           │     │
│  │  ├ 模型: claude-sonnet-4-6                  │     │
│  │  ├ 思考级别: medium                          │     │
│  │  ├ 可见性: workspace / private              │     │
│  │  ├ 最大并发: 1                               │     │
│  │  ├ 指令: ...                                 │     │
│  │  ├ 自定义环境: ...                           │     │
│  │  ├ 自定义参数: ...                           │     │
│  │  ├ MCP 配置: ...                             │     │
│  │  └ 内置: true / false                        │     │
│  └─────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────┘
```

### 3. 组件拆分

| 组件 | 职责 | 文件位置 |
|---|---|---|
| `ArchitecturePage` | 页面主容器，作为默认视图 | `packages/views/workflows/components/overview/architecture-page.tsx` (新建) |
| `StageSwimlane` | 单个阶段的泳道行容器 | `packages/views/workflows/components/overview/stage-swimlane.tsx` (新建) |
| `PluginCard` | Plugin 卡片（每个 node = 一个 plugin 卡片），显示 plugin 名称+描述 | `packages/views/workflows/components/overview/plugin-card.tsx` (新建) |
| `CriticBadge` | 评估器小卡片（虚线边框） | `packages/views/workflows/components/overview/critic-badge.tsx` (新建) |
| `DataFlowArrow` | 阶段间/节点间的连接箭头 | `packages/views/workflows/components/overview/data-flow-arrow.tsx` (新建) |
| `ArchitectureDetailPanel` | 右侧滑出详情面板（显示 plugin 详情 + 关联 agent 信息） | `packages/views/workflows/components/overview/architecture-detail-panel.tsx` (新建) |

### 4. 数据流

```
WorkflowDetailShell
  └── ArchitecturePage (默认视图)
        ├── useQuery(workflowStagesOptions) → stages[]
        ├── useQuery(workflowNodesOptions) → nodes[]
        ├── useQuery(workflowEdgesOptions) → edges[]
        ├── 按 stage_id 分组 nodes
        ├── 对每个 node，通过 worker_id 查询 agent 详情（含 plugin_id）
        ├── 对每个 agent，通过 plugin_id 查询 plugin 信息
        └── 渲染 StageSwimlane[]
              └── PluginCard[] (每个 node = 一个 plugin 卡片)
                    ├── 显示 plugin 名称/描述
                    └── 点击 → ArchitectureDetailPanel (展示 plugin 详情 + 关联 agent)
```

### 5. 交互细节

**Plugin 卡片点击**（每个 node = 一个 plugin 卡片）：
- 右侧滑出 `ArchitectureDetailPanel`
- 上半部分：Plugin 详情（名称、slug、bundle、skills_namespaces）
- 下半部分：关联 Agent 全量信息（名称、描述、头像、运行时模式、运行时、状态、模型、思考级别、可见性、最大并发数、指令、自定义环境、自定义参数、MCP 配置、是否内置）
- 底部按钮：「在编辑器中打开」切换到 Editor 视图

**评估器卡片点击**：
- 同一滑出面板，显示评估器详情
- 显示：评估维度数、评估标准摘要

**阶段间箭头**：
- 从 `workflow_edge` 中筛选跨 stage 的边
- 用 SVG 或 CSS 绘制连接线

### 6. 复用现有代码

| 复用项 | 来源 | 用途 |
|---|---|---|
| `workflowStagesOptions` | `packages/core/workflows/queries.ts` | 查询 stages |
| `workflowNodesOptions` | 同上 | 查询 nodes |
| `workflowEdgesOptions` | 同上 | 查询 edges |
| `useWorkflowViewStore` | `packages/core/workflows/stores/view-store.ts` | 视图切换 |
| `WorkflowDetailShell` | `packages/views/workflows/components/workflow-detail-shell.tsx` | 页面壳 |
| Agent 查询 | `packages/core/agents/queries.ts` | 查询 agent 详情含 plugin |

### 7. 样式规范

- **Stage 泳道**：带颜色区分的水平容器，阶段名称在顶部居中显示
- **Plugin 卡片**：圆角卡片，白色背景，带边框
- **评估器**：虚线边框，浅黄背景（与参考图一致）
- **数据流箭头**：灰色实线箭头，跨 stage 时带标签
- **响应式**：窄屏时泳道内卡片自动换行

---

## 实现步骤

### Step 1: 新建组件文件
- 创建 `stage-swimlane.tsx`、`plugin-card.tsx`、`critic-badge.tsx`、`data-flow-arrow.tsx`、`architecture-detail-panel.tsx`

### Step 2: 实现 StageSwimlane
- 渲染单个 stage 的泳道行
- 接收 stage、nodes、edges 作为 props
- 内部渲染 PluginCard 列表（每个 node = 一个 plugin 卡片）

### Step 3: 实现 PluginCard
- 每个 workflow node 渲染为一个 plugin 卡片
- 通过 node.worker_id → agent → plugin_id 获取 plugin 信息
- 显示 plugin 名称、描述
- 点击触发详情面板

### Step 4: 实现 ArchitectureDetailPanel
- 右侧滑出面板（380px）
- 上半部分：Plugin 详情（来自 agent.plugin_id）
- 下半部分：关联 Agent 信息
- 复用现有 panel 动画样式

### Step 5: 新建 ArchitecturePage
- 在 `architecture-page.tsx` 中新建页面组件
- 查询数据 → 按 stage 分组 → 渲染 StageSwimlane 列表
- 管理选中状态和详情面板

### Step 6: 实现 DataFlowArrow
- 从 edges 中筛选跨 stage 的边
- 用 CSS/SVG 绘制连接线

### Step 7: 路由和导航
- 更新 `WorkflowDetailShell` 中的默认视图
- 确保 `/workflows/[id]` 默认渲染新页面

---

## 验证方案

1. **类型检查**：`pnpm typecheck`
2. **单元测试**：为新组件编写测试
   - `pnpm --filter @multica/views exec vitest run workflows/overview/`
3. **视觉验证**：
   - 启动 dev server：`pnpm dev:web`
   - 进入任意 workflow 详情页，确认默认显示全景图
   - 验证 Stage 泳道、Plugin 卡片正确渲染
   - 点击 Agent/Plugin 验证滑出面板
   - 切换到 Editor 视图验证仍然可用

---

## 关键文件

| 文件 | 操作 |
|---|---|
| `packages/views/workflows/components/overview/architecture-page.tsx` | **新建** |
| `packages/views/workflows/components/overview/workflow-overview-page.tsx` | **不动**（保留现有概览页） |
| `packages/views/workflows/components/overview/stage-swimlane.tsx` | **新建** |
| `packages/views/workflows/components/overview/plugin-card.tsx` | **新建** |
| `packages/views/workflows/components/overview/critic-badge.tsx` | **新建** |
| `packages/views/workflows/components/overview/data-flow-arrow.tsx` | **新建** |
| `packages/views/workflows/components/overview/architecture-detail-panel.tsx` | **新建** |
| `packages/views/workflows/components/overview/index.ts` | **更新导出** |
| `packages/views/workflows/components/workflow-detail-shell.tsx` | **微调**（默认视图） |

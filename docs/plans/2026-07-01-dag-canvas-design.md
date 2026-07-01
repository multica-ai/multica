# DAG Canvas 工作流编排 — 设计文档

> 基于第一性原理推导，2026-07-01

---

## 1. 核心目标

将 Multica 的工作单元从「Issue 列表」升级为「DAG 画布」。

用户与 Analysis Agent 多轮对话确认 PRD 后，由 AI 生成初始 DAG（节点 + 边），人在画布上编辑调整，确认后分发任务给 Worker Agents 执行。画布实时展示每个 Agent 节点的运行状态和上下文用量。

---

## 2. 设计原则

从第一性原理推导的最小必要改动集：

1. **不改动执行层** — 直接复用现有的 Agent / AgentTask 体系
2. **不改动现有 Issue 表** — WorkflowNode 与 Issue 完全独立
3. **不改动现有视图** — DAG Canvas 作为新增顶级视图，不破坏现有功能
4. **画布渲染选型唯一解** — React Flow（React 项目的第一性选择）
5. **上下文传播取消** — 节点之间无产物传递，仅通过 prompt 描述依赖意图

---

## 3. 核心概念映射

| 现有系统 | → 新增概念 | 说明 |
|---------|-----------|------|
| Issue | WorkflowNode | DAG 中的执行单元，绑定到一个 Agent |
| 无 | Workflow | DAG 画布容器，对应一个确认后的 Plan |
| 无 | WorkflowEdge | 节点之间的执行顺序约束（无数据流） |
| 无 | Plan | PRD 对话确认后的快照，状态：draft → confirmed → running → done |
| Chat Session | — | Plan 的对话过程，复用现有表 |
| AgentTask | — | WorkflowNode 的执行层，复用现有表 |

---

## 4. 数据模型

### 4.1 Plan（新增）

```sql
CREATE TABLE plan (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id),
  creator_id UUID NOT NULL REFERENCES member(id),
  title TEXT NOT NULL,              -- PRD 标题
  content TEXT,                     -- PRD 完整内容
  status TEXT NOT NULL DEFAULT 'draft'  -- draft | confirmed | running | done | cancelled
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

状态流转：

```
draft → confirmed → running → done
                    ↓
               cancelled
```

### 4.2 Workflow（新增）

```sql
CREATE TABLE workflow (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  plan_id UUID NOT NULL REFERENCES plan(id),
  title TEXT NOT NULL,             -- 可编辑，如 "需求分解"
  status TEXT NOT NULL DEFAULT 'draft'  -- draft | running | paused | done
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

> 一个 Plan 对应一个 Workflow。Plan 确认后，Workflow 状态从 draft → running。

### 4.3 WorkflowNode（新增）

```sql
CREATE TABLE workflow_node (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_id UUID NOT NULL REFERENCES workflow(id),
  agent_id UUID NOT NULL REFERENCES agent(id),
  title TEXT NOT NULL,             -- 节点显示名称，如 "Researcher"
  prompt TEXT NOT NULL,            -- 该节点的任务指令
  position_x FLOAT NOT NULL DEFAULT 0,  -- Canvas 坐标
  position_y FLOAT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending'  -- pending | queued | running | completed | failed | skipped
  task_id UUID REFERENCES agent_task(id), -- 关联的执行任务
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**status 复用了 agent_task.status 的语义**，通过 `task_id` 关联：

| node.status | task_id | 含义 |
|------------|---------|------|
| pending | NULL | 待分发，人工未确认 |
| queued | 有 | 已认领，等待执行 |
| running | 有 | 执行中 |
| completed | 有 | 成功完成 |
| failed | 有 | 执行失败 |
| skipped | NULL | 人为跳过 |

### 4.4 WorkflowEdge（新增）

```sql
CREATE TABLE workflow_edge (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_id UUID NOT NULL REFERENCES workflow(id),
  source_node_id UUID NOT NULL REFERENCES workflow_node(id),
  target_node_id UUID NOT NULL REFERENCES workflow_node(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT no_self_edge CHECK (source_node_id != target_node_id),
  CONSTRAINT no_duplicate_edge UNIQUE (source_node_id, target_node_id)
);
```

> **无循环约束由应用层保证**。WorkflowEdge 表达的是「source 完成后，target 才能开始」。

### 4.5 不新增表：产出文件

节点产出文件直接使用现有的 **agent_task** 关联的产物（Issue Comment / 文件上传）。不新增 `output_files` 表，**取消上下文传播机制**。

---

## 5. 架构

### 5.1 整体架构

```
┌─────────────────────────────────────────────────────────┐
│                    React Frontend                         │
│                                                          │
│  packages/views/                                         │
│  ├── workflows/                    packages/core/        │
│  │   ├── canvas/                   ├── types/           │
│  │   │   ├── workflow-canvas.tsx   │   └── workflow.ts  │
│  │   │   ├── agent-node.tsx        ├── api/             │
│  │   │   └── workflow-edge.tsx      │   └── workflows.ts│
│  │   └── plan/                      └── stores/          │
│  │       ├── plan-page.tsx             └── workflow-store.ts
│  │       └── plan-chat.tsx                               │
│                                                          │
└─────────────────────────────────────────────────────────┘
                          │
                          │ HTTP REST
                          ▼
┌─────────────────────────────────────────────────────────┐
│                    Go Backend                             │
│                                                          │
│  server/internal/handler/workflow.go   server/internal/service/
│  server/internal/handler/plan.go         └── workflow.go
│                                                          │
│  server/pkg/protocol/events.go             (新增 WebSocket 事件)
│                                                          │
│  server/pkg/db/generated/queries.sqlc.gen.go  (新增 query)
└─────────────────────────────────────────────────────────┘
                          │
                          │ WebSocket
                          ▼
┌─────────────────────────────────────────────────────────┐
│                 Local Daemon / Cloud Runtime              │
│  (现有 AgentTask 执行逻辑，无需改动)                      │
└─────────────────────────────────────────────────────────┘
```

### 5.2 前端模块划分

```
packages/views/workflows/
├── canvas/
│   ├── workflow-canvas.tsx       # React Flow 画布主组件
│   ├── agent-node.tsx            # Agent 节点 React 组件
│   ├── workflow-edge.tsx         # 自定义边（贝塞尔曲线 + 箭头）
│   ├── canvas-toolbar.tsx         # 工具栏（添加节点/保存/运行）
│   ├── node-editor-panel.tsx      # 节点编辑侧边栏
│   └── use-canvas-store.ts       # Canvas 局部状态（选中节点等）
├── plan/
│   ├── plan-page.tsx              # Plan 详情页（含 DAG 预览）
│   ├── plan-chat.tsx              # 对话区（AI 多轮交互）
│   └── plan-header.tsx            # 状态 + 操作按钮
└── routes.ts                      # 路由注册
```

> **packages/views 是无 `next/*` 和 `react-router-dom` 的共享包**，路由注册在 `apps/web/` 和 `apps/desktop/` 中分别完成。

### 5.3 核心类型（packages/core/types/workflow.ts）

```typescript
// 新增文件

export type PlanStatus = "draft" | "confirmed" | "running" | "done" | "cancelled";
export type WorkflowStatus = "draft" | "running" | "paused" | "done";
export type NodeStatus = "pending" | "queued" | "running" | "completed" | "failed" | "skipped";

export interface Plan {
  id: string;
  workspace_id: string;
  creator_id: string;
  title: string;
  content: string;
  status: PlanStatus;
  workflow_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface Workflow {
  id: string;
  plan_id: string;
  title: string;
  status: WorkflowStatus;
  created_at: string;
  updated_at: string;
}

export interface WorkflowNode {
  id: string;
  workflow_id: string;
  agent_id: string;
  title: string;
  prompt: string;
  position_x: number;
  position_y: number;
  status: NodeStatus;
  task_id: string | null;
  // 关联数据（由 API 展开）
  agent?: Agent;
  task?: AgentTask;
  // 上下文用量（由 task 展开）
  context_tokens?: number;
  created_at: string;
  updated_at: string;
}

export interface WorkflowEdge {
  id: string;
  workflow_id: string;
  source_node_id: string;
  target_node_id: string;
}
```

---

## 6. 页面与交互流程

### 6.1 路由

```
/{workspace}/plans              # Plan 列表（新增）
/{workspace}/plans/new          # 创建新 Plan
/{workspace}/plans/:id          # Plan 详情页（含 Canvas）

/{workspace}/canvas/:id         # 直接进入 Canvas 视图（别名路由）
```

> Canvas 作为 `/plans/:id` 的主内容区展示（不是独立页面），Plan 列表作为顶级导航入口。

### 6.2 完整用户流程

```
[Step 1] 用户在 /plans/new 创建 Plan
         → 输入标题 + 初始需求描述
         → 创建 Plan（status = draft） + Workflow（status = draft）

[Step 2] 用户在 Plan 详情页与 Analysis Agent 多轮对话
         → PlanChat 组件加载 Chat Session
         → Analysis Agent 生成节点建议（通过 API）
         → 对话内容持久化到 Chat Session（复用现有表）

[Step 3] 用户点击「生成 DAG」
         → API 返回 AI 生成的 nodes[] + edges[]
         → 存入 workflow_node + workflow_edge 表
         → 展示在 Canvas 上（status = draft）
         → 人进入编辑模式

[Step 4] 人在 Canvas 上编辑 DAG
         → 拖拽调整节点位置（React Flow 内置）
         → 从 Agent 列表拖入新节点
         → 点击连线 → 拖入 → 创建 Edge
         → 点击节点 → 右侧栏编辑 prompt / 重新分配 agent
         → 选中节点 → Delete 删除
         → 选中边 → Delete 删除

[Step 5] 用户点击「确认并运行」
         → Workflow.status = running
         → 根据 DAG 拓扑顺序分发 AgentTask
           （无入边的节点 → 立即分发；
             有入边的节点 → 等所有上游节点 completed → 分发）
         → Plan.status = running

[Step 6] 执行过程中
         → WebSocket 推送 task:running / task:completed / task:failed
         → Canvas 节点状态实时更新
         → 上下文用量实时展示

[Step 7] 所有节点 done
         → Workflow.status = done
         → Plan.status = done
```

### 6.3 DAG 拓扑分发算法

```typescript
// server/internal/service/workflow.go

func DispatchNextNodes(workflowID uuid.UUID) error {
  // 1. 获取所有 pending 节点
  pendingNodes := getPendingNodes(workflowID)

  // 2. 对每个 pending 节点，检查所有上游是否完成
  for _, node := range pendingNodes {
    upstreamDone, err := areAllUpstreamsDone(node.id)
    if err != nil {
      return err
    }
    if !upstreamDone {
      continue
    }

    // 3. 分配给 agent，创建 AgentTask
    taskID, err := CreateTaskForNode(node)
    if err != nil {
      return err
    }

    // 4. 更新节点状态
    UpdateNodeStatus(node.id, "queued", taskID)
  }

  return nil
}

// 在 agent_task 完成时触发
func OnTaskCompleted(taskID uuid.UUID) {
  // 1. 更新对应节点状态为 completed
  // 2. 调用 DispatchNextNodes 检查下游节点
}
```

---

## 7. Canvas 组件设计

### 7.1 React Flow 集成

```typescript
// workflow-canvas.tsx

import ReactFlow, {
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
} from "reactflow";
import "reactflow/dist/style.css";
import { AgentNode } from "./agent-node";
import { WorkflowEdgeComponent } from "./workflow-edge";

const nodeTypes: NodeTypes = {
  agent: AgentNode,
};

const edgeTypes: EdgeTypes = {
  workflow: WorkflowEdgeComponent,
};

export function WorkflowCanvas({ workflowId }: { workflowId: string }) {
  const { nodes, setNodes, edges, setEdges, onNodeDragStop } =
    useWorkflowNodes(workflowId);

  return (
    <ReactFlowProvider>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={setNodes}
        onEdgesChange={setEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={{ type: "workflow" }}
        fitView
      >
        <Background />
        <Controls />
        <MiniMap
          nodeColor={(n) => getNodeColor(n.data?.status)}
          maskColor="rgba(0,0,0,0.1)"
        />
      </ReactFlow>
    </ReactFlowProvider>
  );
}
```

### 7.2 AgentNode 组件

每个节点卡片展示：

```
┌─────────────────────────────────────────┐
│ [Avatar] Researcher            [Status] │  ← 标题行
│ Context: 12,340 tokens                   │  ← 上下文用量
│ ─────────────────────────────────────── │
│ 「调研竞品的功能和定价策略...」            │  ← Prompt 摘要
│                                         │
│ [▶ Running...  0:42]                    │  ← 运行时状态
└─────────────────────────────────────────┘
```

| 节点状态 | 视觉 |
|---------|------|
| pending | 灰色边框，虚线 |
| queued | 蓝色边框，实线 |
| running | 蓝色边框 + 旋转 spinner + 计时 |
| completed | 绿色边框 + ✓ |
| failed | 红色边框 + ✗ |
| skipped | 灰色背景 + 删除线 |

**Context 用量**：从关联的 `AgentTask.usage.context_tokens` 读取，运行时每 5s 更新。

### 7.3 WorkflowEdge 组件

```
        贝塞尔曲线（source right → target left）
                 ↓
  ┌──────┐              ┌──────┐
  │  A   │ ──────────→ │  B   │
  └──────┘              └──────┘

边的样式：
- 默认：灰色 2px 贝塞尔曲线
- 高亮（hover 节点时）：蓝色 3px
- 动画：completed 节点发出脉冲动画沿边传播
```

### 7.4 Canvas 交互规则

| 操作 | 行为 |
|------|------|
| 拖拽节点 | 自由定位，自动保存 position_x / position_y |
| 点击节点 | 选中，右侧栏显示详情 |
| 双击节点 | 直接编辑 prompt 文本 |
| 从节点右侧锚点拖出 | 创建新边 |
| 点击边 | 选中，可删除 |
| 从 Agent 列表拖入画布 | 创建新节点（pending 状态）|
| 选中 + Delete | 删除节点或边 |
| Ctrl+Z | 撤销（基于操作历史的简单 undo）|
| 滚轮 | 缩放画布 |
| 右键节点 | 上下文菜单：编辑 / 删除 / 跳过 |

---

## 8. API 设计

### 8.1 REST Endpoints

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/plans` | 创建 Plan + Workflow |
| GET | `/api/plans/:id` | 获取 Plan + Workflow + Nodes + Edges |
| PATCH | `/api/plans/:id` | 更新 Plan（title, content, status）|
| POST | `/api/plans/:id/generate` | AI 生成初始 DAG |
| PATCH | `/api/workflows/:id` | 更新 Workflow（title, status）|
| GET | `/api/workflows/:id/nodes` | 获取所有节点（含关联 agent/task）|
| POST | `/api/workflows/:id/nodes` | 添加节点 |
| PATCH | `/api/workflows/:id/nodes/:nodeId` | 更新节点（title, prompt, agent_id, position）|
| DELETE | `/api/workflows/:id/nodes/:nodeId` | 删除节点 |
| POST | `/api/workflows/:id/edges` | 添加边 |
| DELETE | `/api/workflows/:id/edges/:edgeId` | 删除边 |
| POST | `/api/workflows/:id/confirm` | 确认 DAG，开始执行 |

### 8.2 响应格式

```typescript
// GET /api/plans/:id
interface PlanDetailResponse {
  plan: Plan;
  workflow: Workflow & {
    nodes: WorkflowNode[];   // 含展开的 agent + task
    edges: WorkflowEdge[];
  };
}
```

### 8.3 AI 生成节点

```
POST /api/plans/:id/generate
Body: { prompt: string }  // 用户当前对话中给出的需求描述

Response: {
  nodes: Array<{
    agent_id: string;
    title: string;
    prompt: string;
    position_x: number;  // 自动布局计算
    position_y: number;
  }>;
  edges: Array<{
    source_title: string;  // 按 title 匹配节点
    target_title: string;
  }>;
}
```

> AI 生成逻辑在服务端调用 LLM，prompt 注入当前 Plan 内容 + Agent 角色列表 + 节点数量限制（建议 ≤ 7 个）。

---

## 9. WebSocket 事件

复用现有 `server/pkg/protocol/events.go` 中的事件类型，通过现有 Client WS（`/ws`）推送。

新增两个事件：

```typescript
// server/pkg/protocol/events.go
const (
  // 现有...
  // 新增：
  EventWorkflowNodeUpdated = "workflow:node_updated"
  EventWorkflowStatusChanged = "workflow:status_changed"
)

// packages/core/types/events.ts
interface WorkflowNodeUpdatedPayload {
  workflow_id: string;
  node_id: string;
  status: NodeStatus;
  context_tokens?: number;   // 运行时更新
  task_id?: string;
}

interface WorkflowStatusChangedPayload {
  workflow_id: string;
  status: WorkflowStatus;
  plan_status: PlanStatus;
}
```

**订阅房间**：`workflow:{workflow_id}`

**触发时机**：
- `task:running` → 更新对应 `workflow_node.status = "running"`
- `task:completed` → 更新为 `"completed"` + context_tokens + 触发 `DispatchNextNodes`
- `task:failed` → 更新为 `"failed"`
- `task:progress` → 更新 `context_tokens`

---

## 10. 与现有系统的边界

### 10.1 复用的部分

| 现有组件 | 复用方式 |
|---------|---------|
| Agent 模型 | WorkflowNode 绑定 agent_id |
| AgentTask 执行 | 直接复用，task_id 关联 |
| WebSocket Hub | 复用 `/ws`，新增事件类型 |
| Chat Session | Plan 的对话过程复用现有表 |
| Issue 创建 | 不受影响，独立体系 |
| Existing Views | Board/List/SwimLane 完全保留 |

### 10.2 新增的边界

| 新增代码 | 所在包 |
|---------|-------|
| Plan / Workflow 类型 | `packages/core/types/workflow.ts` |
| Canvas 组件 | `packages/views/workflows/canvas/` |
| Plan 页面 | `packages/views/workflows/plan/` |
| API 路由 | `server/internal/handler/plan.go` |
| 业务逻辑 | `server/internal/service/workflow.go` |
| DB Migrations | `server/pkg/db/migrations/` |

---

## 11. 实现顺序（风险递增）

### Phase 1: 最小可行闭环
1. 数据库 Migration（4 张新表）
2. Go CRUD handler + service（无 AI 生成）
3. React Flow 画布（仅展示 + 手动添加节点 + 连线 + 删除）
4. 手动「确认运行」→ 创建 AgentTask → 节点状态更新
5. 路由注册 + 页面框架

**Phase 1 结束**：人可以手动创建 DAG，手动运行，无 AI 建议。

### Phase 2: AI 增强
6. AI 生成 DAG（POST /api/plans/:id/generate）
7. Plan Chat 页面（多轮对话 + 生成按钮）
8. 拓扑分发算法（DispatchNextNodes）
9. WebSocket 事件推送

**Phase 2 结束**：完整流程跑通，AI 辅助生成 DAG。

### Phase 3: UX 打磨
10. Canvas 拖拽从 Agent 列表添加节点
11. 撤销/重做历史
12. Context 用量实时展示
13. MiniMap + 小地图导航
14. 节点完成时边的脉冲动画

---

## 12. 技术风险

| 风险 | 等级 | 缓解措施 |
|------|------|---------|
| React Flow 学习曲线 | 中 | 先用最小 API 快速跑通 MVP，再逐步定制 |
| DAG 拓扑分发并发 | 中 | AgentTask 有原子认领机制，数据库锁保证唯一执行 |
| 循环依赖 | 低 | 后端在创建 Edge 时检测并拒绝（应用层 + DB 约束）|
| AI 生成节点质量 | 中 | Phase 1 先跳过 AI，Phase 2 再迭代 prompt |
| Canvas 性能（节点过多）| 低 | React Flow 支持虚拟化，50+ 节点无压力 |

---

## 13. 不在 MVP 范围

以下功能有意排除，保持最小改动：

- ~~上下文传播~~（已取消）
- ~~节点间的数据流~~（无产物传递）
- ~~节点并行执行可视化~~（后端按拓扑顺序执行，前端只展示状态）
- ~~Canvas 持久化布局偏好~~（每次打开重新 layout）
- ~~子 Plan / 嵌套 DAG~~
- ~~Canvas 节点分组 / 子画布~~
- ~~Canvas 节点复制 / 模板~~

---

## 14. 文件清单

```
新增文件（按包分组）：

packages/core/
├── types/workflow.ts                    # 类型定义

packages/views/
├── workflows/
│   ├── index.ts                          # 导出
│   ├── canvas/
│   │   ├── workflow-canvas.tsx
│   │   ├── agent-node.tsx
│   │   ├── workflow-edge.tsx
│   │   ├── canvas-toolbar.tsx
│   │   ├── node-editor-panel.tsx
│   │   └── use-canvas-store.ts
│   └── plan/
│       ├── plan-page.tsx
│       ├── plan-chat.tsx
│       └── plan-header.tsx

server/
├── internal/handler/
│   ├── plan.go                           # Plan CRUD
│   └── workflow.go                       # Workflow/Node/Edge CRUD
├── internal/service/
│   └── workflow.go                       # DAG 分发 + AI 生成
├── internal/daemonws/                    # 无需改动
├── pkg/protocol/
│   └── events.go                         # 新增 workflow 事件常量
└── pkg/db/queries/
    ├── plan.sql                          # 新增 SQL
    ├── workflow.sql                      # 新增 SQL
    └── queries.sqlc.yaml                 # 更新
```

---

*文档版本：v0.1 | 基于第一性原理推导，2026-07-01*

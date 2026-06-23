# Workflow 全景视图 (Panorama View) 设计文档

## 概述

在 Multica Workflow 现有三视图（Editor、Swimlane、Overview）基础上，新增第四种视图"全景视图"，以水平泳道方式展示 Stage → Agent → Plugin 的层级关系，Agent 和 Plugin 均可点击查看详情。

## 背景

- 需求文档：`docs/req.md`
- 参考效果：`docs/cospowers-architecture.drawio`
- 数据模型分析：`docs/workflow-data-model-analysis.md`
- 接口示例：`docs/model.md`

## 数据模型关系

```
Workflow
  └── Stage (multica_workflow_stage, 按 sort_order 排序)
       └── Node (multica_workflow_node, worker_type/worker_id, critic_type/critic_id)
            ├── Worker Agent (multica_agent, 含 plugin_id)
            │    └── Plugin (外部 API /api/plugins/builtin, 含 skills_namespaces)
            └── Critic Agent (multica_agent, 含 plugin_id)
                 └── Plugin
```

## API 设计

### 新增端点

```
GET /api/workflows/{workflowId}/panorama
```

### 响应结构

```json
{
  "workflow": {
    "id": "uuid",
    "title": "string",
    "status": "string"
  },
  "stages": [
    {
      "id": "uuid",
      "name": "string",
      "sort_order": 0,
      "nodes": [
        {
          "id": "uuid",
          "title": "string",
          "description": "string",
          "worker_type": "agent",
          "worker": {
            "id": "uuid",
            "name": "string",
            "description": "string",
            "runtime_mode": "string",
            "plugin": {
              "id": "uuid",
              "name": "string",
              "description": "string",
              "category": "string",
              "skills": ["string"]
            }
          },
          "critic_type": "agent",
          "critic": {
            "id": "uuid",
            "name": "string",
            "description": "string",
            "runtime_mode": "string",
            "plugin": null
          }
        }
      ]
    }
  ]
}
```

### 设计要点

- Worker 和 Critic 都作为嵌套 Agent 对象返回，前端无需二次拼装
- Plugin 通过 `plugin_id` 关联查询 `/api/plugins/builtin`，仅返回精简信息（名称、描述、分类、skills 列表）
- Agent 无 Plugin 时（`plugin_id = null`），plugin 字段为 `null`
- Plugin 外部 API 不可用时，plugin 字段为 `null`，整体响应仍为 200
- Skills 只返回 namespace 字符串列表，不含完整定义

### 后端实现

| 层 | 文件 | 说明 |
|---|---|---|
| Handler | `server/internal/handler/workflow.go` | 新增 `GetWorkflowPanorama` |
| Service | `server/internal/service/workflow.go` | 聚合 Stage → Node → Agent → Plugin |
| Router | `server/cmd/server/router.go` | 注册 `GET /api/workflows/{workflowId}/panorama` |

Service 层聚合逻辑：

1. 查询 `multica_workflow` + 验证 workspace 权限
2. 查询 `multica_workflow_stage`，按 `sort_order` 排序
3. 对每个 Stage 查询关联的 `multica_workflow_node`，按 `sort_order` 排序
4. 收集所有 `worker_id` + `critic_id`，批量查询 `multica_agent`
5. 收集所有 Agent 的非空 `plugin_id`，批量查询 Plugin 信息（复用 `/api/plugins/builtin` 逻辑或底层函数）
6. 组装嵌套响应

## 前端设计

### 视图模式集成

在 `ViewMode` 枚举中新增 `panorama`：

```
ViewMode: 'editor' | 'swimlane' | 'overview' | 'panorama'
```

通过已有视图切换器切换，不修改现有三视图代码。

### 组件树

```
WorkflowPanoramaView          ← 视图根组件
├── PanoramaStageRow          ← 每个 Stage 对应一行水平泳道
│   ├── PanoramaNodeCard      ← Node 卡片（可点击）
│   │   ├── AgentBadge        ← Worker Agent 标签 → 点击打开 Drawer
│   │   ├── PluginBadge       ← Plugin 标签 → 点击打开 Drawer（plugin 为 null 时隐藏）
│   │   └── CriticLabel       ← Critic Agent 小字标注
│   └── ...
├── PanoramaConnector         ← Stage 间连接箭头（阶段流转）
└── PanoramaDetailDrawer      ← 右侧详情抽屉
    ├── AgentDetail           ← Agent 完整信息
    └── PluginDetail          ← Plugin 信息 + Skills 列表
```

### 文件规划

| 文件 | 说明 |
|---|---|
| `packages/views/workflows/components/panorama/panorama-view.tsx` | 全景视图根组件 |
| `packages/views/workflows/components/panorama/stage-row.tsx` | 单条 Stage 泳道行 |
| `packages/views/workflows/components/panorama/node-card.tsx` | Node 卡片 |
| `packages/views/workflows/components/panorama/detail-drawer.tsx` | 详情抽屉 |
| `packages/views/workflows/components/panorama/agent-detail.tsx` | Agent 详情面板 |
| `packages/views/workflows/components/panorama/plugin-detail.tsx` | Plugin 详情面板 |
| `packages/views/workflows/components/panorama/connector.tsx` | Stage 间连接箭头 |
| `packages/core/workflows/queries.ts` | 新增 `useWorkflowPanorama` query hook |
| `packages/core/api/schemas.ts` | 新增 panorama schema + `parseWithFallback` |
| `packages/core/types/workflow.ts` | 新增 Panorama 相关 TS 类型 |
| `packages/core/workflows/stores/view-store.ts` | ViewMode 枚举新增 `panorama` |

### 数据流

```
useWorkflowPanorama(workflowId)
  ↓ TanStack Query
GET /api/workflows/{workflowId}/panorama
  ↓
cache key: ['workflow', workflowId, 'panorama']
  ↓
WS 事件 workflow:node_updated / workflow:stage_updated → invalidate cache
  ↓
响应 → 直接渲染
```

### 布局

- **Stage 行**：水平泳道，按 `sort_order` 自上而下排列，行高自适应
- **Node 卡片**：在 Stage 行内从左到右排列（按 `sort_order`），含 AgentBadge + PluginBadge + CriticLabel
- **Stage 间连接**：上一 Stage 末尾 → 下一 Stage 起始，实线箭头
- **溢出**：卡片过多时水平滚动

### 交互

| 触发 | 行为 |
|---|---|
| 点击 AgentBadge | 打开 DetailDrawer → AgentDetail |
| 点击 PluginBadge | 打开 DetailDrawer → PluginDetail |
| 点击画布空白 | 关闭 DetailDrawer |

## 错误处理与边界

### API 响应容错

遵循项目 CLAUDE.md 的 API Response Compatibility 规则，使用 `parseWithFallback` + zod schema，所有字段 optional + default fallback，解析失败时降级为安全默认值且不 throw。

### 边界场景

| 场景 | 处理方式 |
|---|---|
| Stage 没有 Node | 空行 + 占位文本 |
| Node 的 worker_type 非 agent | 显示对应类型图标和名称，不渲染 Plugin |
| Agent 无 Plugin | PluginBadge 隐藏 |
| Plugin API 不可用 | plugin=null，Agent 部分正常展示，Plugin 区域占位提示 |
| Workflow 无 Stage | 降级：Node 列表作为单一阶段渲染 |
| Panorama API 5xx | TanStack Query error + 重试按钮 |

## 测试策略

### 后端（Go）

- Panorama 正常响应：Stage + Node + Agent 完整链路
- 空 Workflow（无 Stage）：返回空 stages 数组
- Node 的 Agent 不存在：worker=null，不 panic
- Plugin 外部 API 超时：plugin=null，请求仍 200

### 前端（TypeScript）

- Panorama 数据正常渲染（jsdom）
- 点击 AgentBadge 打开 Drawer（jsdom）
- 点击 PluginBadge 打开 Plugin 详情（jsdom）
- Stage 为空时的空态（jsdom）
- Agent 无 Plugin 时 PluginBadge 不渲染（jsdom）
- 非 agent worker_type 的渲染（jsdom）
- Panorama schema 容错畸形数据（Node，不少于 2 个 case）

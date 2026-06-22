# Workflow 阶段可视化需求文档

> 从《多阶段工作流与 Issue 可视化需求文档》拆分出的 Workflow 独立需求文档。
> 相关设计规格：`docs/superpowers/specs/2026-06-19-workflow-stage-overview-design.md`

## 1. 文档目标

- 提升 Workflow 的信息完整度，避免"只看到流程名，看不到阶段划分、节点能力和配置细节"
- 引入"阶段（Stage）"作为 Workflow 的一等概念：Workflow 层面定义可配置的业务阶段（如需求、设计、编码、测试、部署），每个阶段包含一个或多个 Workflow Node
- 支持逐层下钻：从阶段画布 → 阶段内节点 DAG → 节点配置详情（agent/plugin/skill/format_schema/critic）

## 2. 背景与问题

当前系统已具备底层编排数据，但 Workflow 前端展示仍存在以下问题：

- Workflow 页面信息展示不足，缺少"阶段"这一业务层面的组织概念，plugin、agent、skill、节点配置等关键能力未被充分暴露
- 缺少"阶段"概念的统一建模，Workflow 无法表达业务阶段的划分
- 用户只看得到流程名，无法直观了解内部结构和能力组成

## 3. 目标用户

- TL / 管理者：关注流程包含哪些阶段、每个阶段做什么、由什么能力组成
- 执行成员：关注流程中与自己相关的节点配置、上下游关系
- 产品/运营/观察者：希望低成本理解一个流程能做什么

## 4. 总体设计原则

- **新建独立页面**：新增 `/workflows/[id]/overview` 作为阶段概览页，保留现有 `WorkflowDetailPage` 编辑器不变
- **阶段画布**：以横向滚动的阶段卡片条作为用户认知主视角，点击阶段后在其下方展开该阶段内的节点 DAG，再下钻到节点配置详情
- **混合交互模式**：页面以查看/浏览为主（阶段画布、节点 DAG 只读），阶段层面支持轻量编辑（添加/删除/排序/节点分配到阶段）；节点内部配置编辑保留在原编辑器
- **查看优先**：阶段画布侧重"能力说明"和"结构理解"，非编辑
- **所有 Workflow 均有阶段**：阶段不是模板专属概念，所有 Workflow（模板和实例）都保留阶段结构
- **边只在阶段内部**：节点之间的边不跨阶段连接，阶段间的数据流通过 `stage.sort_order` 隐式传递
- 优先展示用户最关心的信息：阶段划分、节点能力、执行者配置
- 支持逐层展开，不在首屏一次性暴露全部底层细节

## 5. 术语定义

- **Workflow**：流程定义，描述一类任务的标准执行路径。由多个**阶段**组成，每个阶段包含一个或多个 Workflow Node
- **阶段（Stage）**：Workflow 的可配置业务阶段划分（如：需求、设计、编码、测试、部署），是节点的容器/分组。阶段按 `sort_order` 顺序执行，前一阶段所有终点节点的输出隐式成为下一阶段起点节点的输入
- **Workflow Node（节点）**：Workflow 中某个阶段内的基本组成单元，定义了一个执行步骤的配置 —— 包括 worker（执行者 agent/squad/人工）、critic（审核者 agent/squad/人工）、format_schema（输入输出格式）、关联的 plugin 和 skill。节点之间通过边在**同一阶段内部**形成 DAG，不允许跨阶段连线
- **父子关系**：阶段是"父"，阶段内的节点是"子"。当前不引入节点内部的复合嵌套（即节点不再包含子节点），保持阶段 → 节点两层结构
- **Worker**：节点的执行者配置，可以是 agent、squad 或人工
- **Critic**：节点的审核者配置，可以是 agent、squad 或人工
- **Format Schema**：节点的输入/输出格式定义
- **Plugin**：agent 关联的能力插件
- **Skill**：节点使用的技能定义

## 6. 信息架构

```
┌─────────────────────────────────────────────────┐
│  路由: /workflows/[id]/overview                   │
│                                                  │
│  ┌──────────────────────────────────────────┐   │
│  │         阶段画布（Stage Canvas）          │   │  ← 第一层：横向滚动卡片条
│  │  [阶段1] [阶段2 ✓] [阶段3] [阶段4] ...   │   │     HTML/CSS 实现
│  └──────────────────────────────────────────┘   │
│                    │ 点击阶段                     │
│                    ▼                             │
│  ┌──────────────────────────────────────────┐   │
│  │       阶段内节点 DAG（Stage Node DAG）    │   │  ← 第二层：只读 ReactFlow 画布
│  │   ┌──┐    ┌──┐    ┌──┐                  │   │     复用现有 WorkflowNode/Edge 渲染器
│  │   │A │───▶│B │───▶│C │                  │   │
│  │   └──┘    └──┘    └──┘                  │   │
│  └──────────────────────────────────────────┘   │
│                    │ 点击节点                     │
│                    ▼                             │
│  ┌──────────────────────────────────────────┐   │
│  │     节点配置详情（Node Detail Panel）     │   │  ← 第三层：侧边抽屉面板
│  │  Worker / Critic / Schema / Relations    │   │
│  │  Agent / Plugin / Skill                  │   │
│  └──────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

## 7. 页面需求

### 7.1 页面定位

新建独立页面 `/workflows/[id]/overview`，与现有 `WorkflowDetailPage`（编辑器，路由 `/workflows/[id]`）并行存在：

| 维度 | Overview（新） | DetailPage（现有） |
|------|---------------|-------------------|
| 路由 | `/workflows/[id]/overview` | `/workflows/[id]` |
| 主视图 | 阶段卡片条 + 阶段内节点 DAG | 全局节点 DAG 编辑器 |
| 节点 DAG | 只读，单阶段 | 可编辑，全 Workflow |
| 阶段管理 | 轻量编辑（添加/删除/排序/分配节点） | 不在编辑器中管理阶段 |
| 节点配置 | 只读查看（抽屉面板） | 编辑 worker/critic/schema |
| 定位 | 理解流程结构 | 编辑流程配置 |

### 7.2 首屏展示要求

页面顶部展示 Workflow 的**阶段画布**。阶段画布使用 **HTML/CSS 横向滚动卡片条**实现（不使用 ReactFlow），阶段卡片按 `sort_order` 从左到右排列。阶段画布是用户的第一认知入口。

每个阶段卡片至少展示：

- 阶段名称（如"需求"、"设计"、"编码"、"测试"、"部署"）
- 阶段序号，例如"阶段 2/5"
- 该阶段包含的节点数量（API 返回的聚合字段 `node_count`）
- 选中态高亮（边框 + 背景色变化）

阶段画布支持轻量编辑：

- 末尾"+"按钮 → 弹出创建阶段对话框（名称 + 描述）
- 每张卡片右上角 ┇ 菜单 → 编辑名称 / 删除阶段
- 卡片支持拖拽排序（P1）

阶段卡片条边界情况：

- **无阶段**：显示空占位"尚未定义阶段"，提供"创建第一个阶段"CTA
- **旧数据兼容**：`stage_id = NULL` 的节点自动归入虚拟卡片"未分组"，收纳所有未分配节点
- **阶段数量多**（>10）：横向滚动 + 两端渐变遮罩

参考结构：

```
┌──────────────────────────────────────────────────────────┐
│  [阶段 1/5: 需求]  [阶段 2/5: 设计 ✓]  [阶段 3/5: 编码]  ...  │
│  [  3 个节点   ]  [   2 个节点    ]  [  4 个节点   ]       │
│                       ▔▔▔▔▔▔▔▔▔▔▔                         │
│                       选中态高亮                             │
└──────────────────────────────────────────────────────────┘
```

### 7.3 阶段内节点 DAG

点击某个阶段卡片后，在该卡片下方展开**该阶段内的节点 DAG**。节点 DAG 使用 **ReactFlow 只读模式**渲染，复用现有的 `WorkflowNode` / `WorkflowEdge` 渲染器。

展开行为：

- 展开/收起动画：画布区 `max-height: 0 → 500px`，300ms ease-out 过渡
- `fitView` 自动适配画布缩放
- 只读模式：节点可点选（打开详情面板），但不可拖拽、不可连线、无编辑快捷键
- 空阶段显示占位："此阶段暂无节点，可在编辑器中添加"
- 同一时刻只有一个阶段处于展开状态（点击另一个阶段时切换）

每个节点卡片至少展示（复用现有 `WorkflowNode` 渲染器的信息和状态着色）：

- 节点名称
- 节点序号，例如"节点 2/3"（阶段内序号，前端计算）
- Worker 类型与执行者（agent 名称 / squad 名称 / 人工）
- Critic 类型与审核者（agent 名称 / squad 名称 / 人工）
- 依赖的上游节点数量（阶段内）
- 点击可下钻查看配置详情

### 7.4 节点详情下钻

点击某个节点后，在画布右侧滑出**抽屉面板**（desktop: 380px，移动端: 全屏底部抽屉）。

下钻区展示内容：

- **基本信息**：节点名称、描述、阶段内序号（如"节点 2/4"）
- **Worker 配置**：类型（agent/squad/human）、具体执行者名称、关联的 plugin 列表
- **Critic 配置**：类型（agent/squad/human）、具体审核者名称
- **Format Schema**：格式化展示 JSON Schema，空时显示"无格式约束"
- **关联的 Skill 列表**：技能名称与描述
- **上下游节点关系**：从该节点出发的边（下游）、指向该节点的边（上游），均在当前阶段内
- **底部固定按钮**："在编辑器中打开" → 跳转到 `/workflows/[id]`（原编辑器）

未配置字段以灰色文字显示"未配置"。

### 7.5 信息补充要求

对于 Workflow，页面需要展示以下关联信息（通过展开阶段和下钻节点获取，不要求首屏展示全部）：

- **每个阶段**包含的节点列表及节点数量
- **每个节点**绑定的 agent（worker 和 critic 的 agent/squad 名称）
- agent 关联的 plugin
- **每个节点**使用的 skill
- **每个节点**的配置摘要（worker_type、critic_type、format_schema 要点）
- **每个节点**的输入与输出摘要（基于 format_schema）

## 8. 关键功能需求

### 8.1 阶段数据建模

系统需要在数据库层面新增阶段建模：

- 新建 `multica_workflow_stage` 表（`id`, `workflow_id`, `name`, `description`, `sort_order`, timestamps）
- 在 `multica_workflow_node` 上新增 `stage_id` 列（FK → `workflow_stage.id`, `ON DELETE SET NULL`）
- 阶段是 Workflow 的固有属性，不限于模板；所有 Workflow 均可定义阶段

### 8.2 阶段配置（API）

阶段 CRUD API 端点：

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/api/workflows/{id}/stages` | 创建阶段 |
| `PUT` | `/api/workflows/{id}/stages/{stageId}` | 更新阶段 |
| `DELETE` | `/api/workflows/{id}/stages/{stageId}` | 删除阶段（节点回退到 NULL） |
| `PUT` | `/api/workflows/{id}/stages/reorder` | 批量更新排序 |
| `PUT` | `/api/workflows/{id}/nodes/{nodeId}/stage` | 将节点分配到阶段 |

`GET /api/workflows/{id}` 响应中新增 `stages[]` 数组，每项包含 `node_count` 聚合字段。

### 8.3 边约束

- 边只在同一阶段内的节点之间连接
- 创建/更新边时，后端验证 `source_node_id` 和 `target_node_id` 的 `stage_id` 必须相同，否则返回 400
- 阶段间的数据流通过 `stage.sort_order` 隐式传递

### 8.4 父子关系

阶段与节点之间是父子关系：阶段是"父"，节点是"子"。当前不引入节点内部的复合嵌套（即节点不再包含子节点），保持阶段 → 节点两层结构的简洁性。如果未来需要更复杂的嵌套，再评估是否引入复合节点。

## 9. 数据需求

基于数据库新增 `workflow_stage` 表 + `workflow_node.stage_id` FK，前端通过 TanStack Query 获取和缓存数据。不引入单独的 ViewModel 层——数据转换通过 colocated 辅助函数完成（与项目现有模式一致）。

至少需要以下数据能力：

- **Workflow 的阶段定义**（阶段名称、顺序、包含的节点数 `node_count`）
- `workflow_node` 的 `stage_id` 关联
- node → workflow_node 执行顺序与依赖关系（边）
- agent / plugin / skill 关联关系（通过现有 API 获取）
- 每个节点的 worker、critic、format_schema 配置（通过现有 `GET /api/workflows/{id}` 返回的 nodes 数组）

### 前端状态管理

| 数据 | 归属 | 说明 |
|------|------|------|
| Workflow + Stages + Nodes + Edges | **TanStack Query** (`workflowDetailOptions`) | 与编辑器共享同一 cache key，自动同步 |
| `selectedStageId` | 页面级 `useState` | 当前展开的阶段 |
| `selectedNodeId` | 页面级 `useState` | 当前选中节点（打开详情面板） |
| `editingStage` | 页面级 `useState` | 对话框瞬时态 |

不建全局 Zustand store——选中状态是纯浏览期的瞬时 UI 状态，不跨页面保持。

## 10. 非功能要求

- 页面结构应支持未来阶段数量扩展，不限定固定数量（横向滚动 + 渐变遮罩）
- 展示方案需要兼容阶段为空、节点为空、旧节点 `stage_id = NULL` 等边界情况
- 首屏信息密度高但可快速扫读，避免堆叠原始 JSON 或过多技术字段
- 下钻过程应保持上下文连续，用户知道自己当前正在看哪个阶段、哪个节点（面包屑或选中态指示）
- 阶段画布（HTML 卡片条）与节点画布（ReactFlow）均需支持流畅的交互体验

## 11. 组件架构决策

**选择方案：双层画布（HTML 阶段卡片条 + ReactFlow 节点 DAG）**

评估结论：

- ReactFlow 不原生支持在画布内嵌入可展开的子图/分组节点结构
- 阶段画布使用 HTML/CSS 横向滚动卡片条，实现成本低、可访问性好、响应式友好
- 节点 DAG 使用 ReactFlow 只读模式，复用现有 `WorkflowNode` / `WorkflowEdge` 渲染器，只需处理阶段内的单层 DAG
- 不引入新的绘图库依赖

## 12. 验收标准

- 用户可在首屏看清一个 Workflow 的阶段画布（阶段卡片及顺序）
- 用户可点击阶段展开内部节点 DAG，看清节点间的顺序和依赖关系
- 用户可识别节点总数（按阶段统计）
- 用户可下钻查看每个节点的完整配置（worker、critic、format_schema、plugin、skill）
- 用户可查看每个节点关联的 agent、plugin、skill、配置等详细信息
- 用户可理解该 Workflow 的阶段划分、整体能力和执行路径
- 用户可在阶段画布上创建/编辑/删除阶段（轻量编辑）
- 旧数据兼容：无 `stage_id` 的节点自动归入"未分组"
- 现有 `WorkflowDetailPage` 编辑器功能不受影响

## 13. 建议实施优先级

### P0（核心交付）

- Workflow 阶段数据建模（数据库 migration + `workflow_stage` 表 + `stage_id` 列 + sqlc + API）
- 阶段画布（HTML 横向滚动卡片条，含空状态和"未分组"兼容）
- 阶段内节点 DAG（ReactFlow 只读画布，复用现有渲染器）
- 节点详情下钻抽屉面板（Worker / Critic / Schema / Relations）

### P1（增强）

- 阶段轻量编辑（创建/编辑/删除对话框 + 节点分配到阶段 + 阶段排序）
- 节点关联 agent/plugin/skill 信息的展示（从现有 API 拉取 agent/plugin/skill 元数据）
- 响应式适配（移动端纵向手风琴布局）

### P2（优化）

- 阶段卡片拖拽排序
- 节点在阶段间拖拽分配
- 展开/收起动画优化
- E2E 测试

## 14. 总结

Workflow 阶段可视化的本质，是引入"阶段"作为 Workflow 的一等概念，新建独立的概览页（`/workflows/[id]/overview`）让用户能理解一个 Workflow 的阶段划分和每个阶段的能力组成。

关键决策（brainstorming 确认）：

- **新建页面 + 保留原编辑器**，路由 `/workflows/[id]/overview`
- **所有 Workflow 均有阶段**，不限于模板
- **边只在阶段内部**，阶段间通过 `sort_order` 隐式传递
- **双层画布**（HTML 卡片条 + ReactFlow DAG），混合模式（查看为主 + 阶段轻量编辑）
- **DB 建模**：`workflow_stage` 表 + `workflow_node.stage_id` FK（`ON DELETE SET NULL`）

最终交付应满足核心场景：

- 用户先看到阶段画布，展开后看到节点 DAG，看得懂"这个 Workflow 有哪些阶段、每个阶段做什么、由什么能力组成"
- 用户可下钻到节点级别，了解每个节点的 worker、critic、plugin、skill、format_schema 等完整配置
- 用户可在阶段层面进行轻量编辑（添加/删除/排序阶段、调整节点所属阶段）

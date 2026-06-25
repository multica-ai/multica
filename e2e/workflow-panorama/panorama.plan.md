# Workflow Panorama View (研发全景图) — E2E Test Plan

> 基于: `docs/superpowers/specs/2026-06-23-workflow-panorama-design.md`
> 
> Seed: `e2e/workflow-panorama/seed-panorama.ts`
> 
> 账号: kdemo648@gmail.com / demo111 workspace

## Application Overview

研发全景图 (R&D Panorama) 是 workflow 详情页的默认视图，替代原有的 Overview 页面。
采用泳道卡片 (swimlane-card) 风格，以 Stage → Node (Plugin) → Agent 三层架构展示单个 workflow 的完整研发管线。

页面包含：
- **Stage 泳道行**: 按 `sort_order` 水平排列的阶段容器，阶段名称居中显示
- **Plugin 卡片**: 每个 workflow node 渲染为一个 plugin 卡片，显示名称和描述
- **Critic 评估器徽章**: 虚线边框小卡片，标识评估节点
- **Data Flow 箭头**: 阶段间的数据流连接线
- **右侧详情面板**: 点击卡片/徽章滑出，展示 Plugin 详情 + 关联 Agent 信息

## 技术上下文

- **路由**: `/[workspaceSlug]/workflows/[id]` → `WorkflowDetailShell` → 默认 panorama 视图
- **视图存储**: `useWorkflowViewStore` 支持三种模式: `panorama` (默认), `overview`, `editor`
- **数据结构**: stages (来自 `workflowStagesOptions`), nodes (来自 `workflowNodesOptions`), edges (来自 `workflowEdgesOptions`)
- **组件映射**: `StageSwimlane` → `PluginCard` → `CriticBadge` + `ArchitectureDetailPanel` + `DataFlowArrow`

## Test Scenarios

### 1. Page Shell & Navigation

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 1.1. panorama-is-default-view

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 登录并导航到 workflow 详情页 `/[slug]/workflows/[id]`
    - expect: URL 停留在 workflow 详情页（不重定向到 /overview）
    - expect: 页面显示全景图视图（非 editor 视图）
  2. 验证页面壳结构
    - expect: 页面标题可见
    - expect: 视图切换按钮可见

#### 1.2. view-toggle-switches-between-views

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击视图切换下拉按钮
    - expect: 下拉菜单显示 Editor / Overview 选项
  2. 选择 Editor 视图
    - expect: 页面切换到 ReactFlow 编辑器视图（DAG 画布可见）
  3. 切回 Panorama 视图
    - expect: 全景图视图重新可见

#### 1.3. workflow-title-displayed-in-header

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 导航到 workflow 详情页
    - expect: 页面 header 显示 workflow 标题

---

### 2. Stage Swimlane Rendering

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 2.1. correct-number-of-swimlanes

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 创建一个包含 4 个 stage 的 workflow
  2. 导航到该 workflow 的全景图视图
    - expect: 页面渲染 4 个泳道行（每个 stage 一个）

#### 2.2. stage-names-displayed

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 导航到包含多个 stage 的 workflow
    - expect: 每个泳道顶部居中显示对应 stage 名称
    - expect: "需求接入"、"需求分析"、"编码实现"、"测试发布" 均可见

#### 2.3. stages-ordered-by-sort-order

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 导航到包含按 sort_order 排列 stage 的 workflow
    - expect: DOM 中泳道顺序与 sort_order 一致（升序排列）

#### 2.4. swimlanes-have-distinct-styling

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 检查泳道容器的 CSS 样式
    - expect: 泳道有可见的边框或背景色区分

#### 2.5. empty-stage-renders-correctly

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 创建包含空 stage（无 node）的 workflow
  2. 导航到全景图
    - expect: 空 stage 泳道正常渲染（不崩溃）
    - expect: 泳道内显示空状态提示或至少不报错

---

### 3. Plugin Card Rendering

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 3.1. plugin-cards-render-in-swimlanes

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 导航到包含 10 个 node 的 workflow 全景图
    - expect: Plugin 卡片在对应 stage 泳道内渲染
    - expect: 至少渲染 8 个卡片（不含仅 critic 节点时）

#### 3.2. plugin-cards-display-name-and-description

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 检查 plugin 卡片内容
    - expect: 卡片显示 plugin/agent 名称（如 "brainstorming", "frontend-dev"）
    - expect: 卡片显示描述文字（如果存在）

#### 3.3. plugin-cards-have-proper-styling

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 检查 plugin 卡片的 CSS 样式
    - expect: 卡片有圆角 (border-radius ≠ 0)
    - expect: 卡片有边框和背景色
    - expect: 卡片使用 CSS 变量 token（非硬编码颜色）

#### 3.4. plugin-cards-wrap-on-narrow-viewport

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 设置窄视口 (480x900)
  2. 导航到 workflow 全景图
    - expect: 卡片在泳道内自动换行
    - expect: 页面不产生水平滚动条

---

### 4. Critic Badge Rendering

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 4.1. critic-badges-render-for-critic-nodes

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 创建一个包含 critic_type 非空 node 的 workflow
  2. 导航到全景图
    - expect: 评估器节点渲染为 CriticBadge 组件
    - expect: 至少 2 个 critic badge 可见

#### 4.2. critic-badges-have-dashed-border

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 检查 critic badge 的 CSS 样式
    - expect: 边框样式为 dashed（虚线）

#### 4.3. critic-badges-show-evaluator-names

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 检查 critic badge 内容
    - expect: 显示评估器名称（如 "aireq-evaluator", "sysreq-evaluator"）
    - expect: 显示 "Critic" 标签

---

### 5. Data Flow Arrows

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 5.1. cross-stage-arrows-render

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 创建包含跨 stage 边的 workflow
  2. 导航到全景图
    - expect: 阶段间数据流箭头/连线可见

#### 5.2. edge-labels-displayed

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 创建包含带标签边的 workflow
  2. 导航到全景图
    - expect: 边标签正确显示（如果存在）

---

### 6. Architecture Detail Panel

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 6.1. click-plugin-card-opens-detail-panel

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击一个 plugin 卡片
    - expect: 右侧滑出详情面板（约 380px 宽）
    - expect: 面板包含 data-testid="architecture-detail-panel" 或类似标识

#### 6.2. panel-shows-plugin-information

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击 "brainstorming" plugin 卡片
    - expect: 详情面板显示 plugin 名称
    - expect: 面板包含 plugin 相关字段（slug, bundle, skills 等）

#### 6.3. panel-shows-agent-information

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击一个有关联 agent 的 plugin 卡片
    - expect: 详情面板显示关联 Agent 信息
    - expect: 包含 name, description, runtime mode, status, model 等字段
    - expect: 包含 thinking level, visibility, max concurrency 信息

#### 6.4. panel-close-button-works

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 打开详情面板
  2. 点击关闭按钮
    - expect: 详情面板消失或隐藏

#### 6.5. clicking-different-plugin-switches-content

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击 "brainstorming" 卡片 → 面板显示 brainstorming 信息
  2. 点击 "frontend-dev" 卡片
    - expect: 面板内容切换到 frontend-dev 的信息

#### 6.6. open-in-editor-button-switches-view

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 打开详情面板
  2. 点击 "Open in Editor" 或 "在编辑器中打开" 按钮
    - expect: 视图切换到 Editor 模式
    - expect: ReactFlow 画布可见

---

### 7. Critic Detail Panel

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 7.1. click-critic-badge-opens-detail-panel

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击一个 critic badge
    - expect: 右侧详情面板滑出
    - expect: 面板显示评估器相关信息（评估维度、评估标准等）

---

### 8. Error & Edge Cases

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 8.1. no-stages-empty-state

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 创建无 stage 的 workflow
  2. 导航到全景图
    - expect: 显示空状态提示（而非崩溃白屏）
    - expect: 提示文字如 "No stages" / "暂无阶段" / "empty"

#### 8.2. loading-skeleton-displays

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 在慢速网络下导航到 workflow 详情页
    - expect: 数据加载期间显示骨架屏或 loading 动画

#### 8.3. api-error-shows-retry

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 导航到不存在的 workflow ID
    - expect: 显示错误状态（"not found" / "未找到"）
    - expect: 显示重试按钮或返回按钮

#### 8.4. unauthorized-workspace

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 使用无权访问 workspace 的账号导航到 workflow
    - expect: 显示无权限提示

#### 8.5. rapid-card-clicking-no-error

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 快速连续点击多个 plugin 卡片
    - expect: 页面不崩溃、不白屏
    - expect: 没有错误 toast/alert 出现
    - expect: 全景图容器仍然可见

---

### 9. View Mode Persistence & State

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 9.1. view-mode-persists-after-reload

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 切换到 Editor 视图
  2. 刷新页面
    - expect: 页面正常加载（不崩溃）
    - expect: 视图模式持久化或回退到 panorama（取决于实现）

#### 9.2. legacy-overview-url-redirects

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 直接导航到 `/[slug]/workflows/[id]/overview` 旧路由
    - expect: 自动重定向到 `/[slug]/workflows/[id]`
    - expect: 全景图视图正常渲染

---

### 10. Keyboard & Accessibility

**Seed:** `e2e/workflow-panorama/seed-panorama.ts`

#### 10.1. keyboard-focus-and-activate-plugin-cards

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 使用 Tab 键聚焦 plugin 卡片
  2. 按 Enter 键激活
    - expect: 页面不崩溃
    - expect: 详情面板可能打开（取决于实现）

#### 10.2. escape-closes-detail-panel

**File:** `e2e/workflow-panorama/panorama-view.spec.ts`

**Steps:**
  1. 点击 plugin 卡片打开详情面板
  2. 按 Escape 键
    - expect: 详情面板关闭

---

## 实现状态

| 组件 | 状态 | data-testid |
|------|------|-------------|
| StageSwimlane | ❌ 未实现 | `stage-swimlane-{id}` |
| PluginCard | ✅ 已实现 | `plugin-card-{id}` |
| CriticBadge | ✅ 已实现 | `critic-badge-{id}` |
| DataFlowArrow | ❌ 未实现 | `data-flow-arrow-{id}` |
| ArchitectureDetailPanel | ❌ 未实现 | `architecture-detail-panel` |
| ArchitecturePage (全景图主容器) | ❌ 未实现 | `panorama-view` |

## 运行测试

```bash
# 确保后端和前端都在运行
# Terminal 1: make server
# Terminal 2: pnpm dev:web

# 运行所有 panorama E2E 测试
pnpm exec playwright test e2e/workflow-panorama/

# 运行单个测试
pnpm exec playwright test e2e/workflow-panorama/panorama-view.spec.ts

# Debug 模式
pnpm exec playwright test e2e/workflow-panorama/ --debug

# 查看报告
pnpm exec playwright show-report
```

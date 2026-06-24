# Workflow Panorama 流程图画布重构 — 设计文档

## Context

`WorkflowPanoramaPage` 是 workflow 详情页的默认全景视图。当前实现采用"按 stage 纵向堆叠 + stage 之间插入独立箭头块"的方式渲染，更像"分段卡片列表"而非"连续流程图"。本次重构的目标是将页面从"分段列表"转变为"连续流程图画布"。

**基准设计**：`docs/superpowers/specs/2026-06-23-workflow-panorama-design.md`（原始 panorama 设计）

**核心决策**：
- 保持纵向推进方向（top → bottom）
- 用 SVG overlay 替代装饰性短箭头，绘制节点到节点的真实连线
- Stage 边界从"独立卡片面板"弱化为"半透明泳道背景带"
- 节点卡片从 176×112px 压缩到 ~120×72px，提升首屏信息密度
- 不新增外部布局库依赖

---

## 设计方案

### 1. 组件变更总览

| 文件 | 动作 | 说明 |
|------|------|------|
| `workflow-panorama-page.tsx` | **重写** | 去掉 `overflow-auto`/`max-w-[1440px]`，引入 SVG overlay 和视口适配 |
| `stage-swimlane.tsx` | **重写为 `stage-lane.tsx`** | 去掉厚边框/圆角/阴影，改为半透明色带 |
| `plugin-card.tsx` | **重写为 `compact-node-card.tsx`** | 缩小到 ~120×72px，精简展示信息 |
| `critic-badge.tsx` | **修改** | 缩小尺寸，SVG 连线由 overlay 统一管理 |
| `data-flow-arrow.tsx` | **删除** | 功能由 SVG overlay 替代 |
| **新建** `panorama-svg-overlay.tsx` | **新建** | 核心连线组件，根据节点 DOM 位置绘制所有 edge |
| `architecture-detail-panel.tsx` | **不动** | 保持现有实现 |
| `panorama-page.test.tsx` | **更新** | 适配新组件结构 |
| `stage-swimlane.test.tsx` | **更新** | 适配 stage-lane |
| `plugin-card.test.tsx` | **更新** | 适配 compact-node-card |
| `data-flow-arrow.test.tsx` | **删除** | 对应组件删除 |
| `critic-badge.test.tsx` | **更新** | 适配尺寸变化 |

### 2. 新布局结构

```
┌─────────────────────────────────────────────────────┐
│  PageHeader (shrink-0)                               │
│  Workflow title            [viewToggle]              │
├─────────────────────────────────────────────────────┤
│  PanoramaCanvas (flex-1, flex flex-col,             │
│                  min-h-0, overflow-auto)            │
│                                                      │
│  ┌─ PanoramaSvgOverlay (absolute, pointer-events: none) ─┐
│  │  <svg>                                              │ │
│  │    all edge paths + arrowhead markers               │ │
│  │  </svg>                                             │ │
│  └────────────────────────────────────────────────────┘ │
│                                                      │
│  ┌─ StageLane: Intake ─────────────────────────────┐ │
│  │  [CompactNodeCard] → [CompactNodeCard]           │ │
│  │    ↓ critic (inline, small)                      │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
│  ┌─ StageLane: Analysis ──────────────────────────┐ │
│  │  [CompactNodeCard] → [CompactNodeCard]           │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
│  ┌─ StageLane: Output ─────────────────────────────┐ │
│  │  [CompactNodeCard]                                │ │
│  └────────────────────────────────────────────────┘ │
│                                                      │
└─────────────────────────────────────────────────────┘
```

### 3. StageLane（替代 StageSwimlane）

**视觉变更**：
- 去除 `rounded-2xl border shadow-[...]`
- 去除 `border-l-[6px]` 厚色条
- 背景改为半透明色带：`bg-{color}-50/40`（约 40% 不透明度）
- 色系保持当前 6 色循环
- 头部压缩为单行：`Stage N · Name`，字号 `text-xs` 或 `text-sm`
- 去除描述文字和统计数字
- 内边距从 `px-4 py-4` 压缩到 `px-3 py-2`
- 去除 `space-y-3` 间距层级

**测试适配**：
- `data-testid` 从 `stage-swimlane-{id}` 改为 `stage-lane-{id}`
- 空状态 `data-testid` 从 `stage-swimlane-empty` 改为 `stage-lane-empty`
- 描述文字和统计数字的断言需要移除
- 色系相关的 `data-testid` 保持可用

### 4. CompactNodeCard（替代 PluginCard）

**视觉变更**：
- 最小尺寸从 176×112px 压缩到 120×72px
- 保留：插件名（truncated）、Agent 状态点 + Agent 名称
- 去除：`ArrowUpRight` 图标、Plugin badge 标签、描述文字、model 名称
- hover/selected 样式保持（边框高亮、轻微上移、阴影）
- 保留 `aria-pressed` 无障碍属性

**Agent 信息展示**：
- 状态圆点 + Agent 名称在卡片底部一行展示
- 去除 Agent 信息区域的独立边框背景（原 `border bg-muted/45`），改为纯文本行
- Agent 名称截断，tooltip 可后续考虑

**测试适配**：
- `data-testid` 从 `plugin-card-{id}` 改为 `compact-node-card-{id}`
- Plugin badge 和描述文字的断言需要移除
- 点击交互断言保持（`fireEvent.click(screen.getByTestId(...))` 仍有效）

### 5. CriticBadge 调整

**视觉变更**：
- 最小尺寸从 168×96px 压缩到约 120×64px
- 保持虚线边框 + 警告色系
- 去除 ArrowUpRight 图标
- 与 Worker 卡片的虚线连接线改为由 SVG overlay 绘制

### 6. PanoramaSvgOverlay（新建核心组件）

**职责**：根据节点 DOM 位置绘制所有 edge 连线。

**实现要点**：
- 绝对定位 (`absolute inset-0`)，覆盖画布容器
- `pointer-events: none`，不干扰节点点击
- 通过 `ResizeObserver` + `getBoundingClientRect` 获取每个节点和 critic 卡片的实际位置
- 坐标计算时扣除画布容器的 `scrollTop`/`scrollLeft` 偏移量（`getBoundingClientRect` 返回视口坐标，需转换为 SVG 坐标系）
- 使用 `<svg>` + `<path>` 绘制连线，带 `<marker>` 箭头
- 连线样式：`stroke="currentColor"`（继承 stage 色系），`strokeWidth={1.5}`，虚线 `strokeDasharray="4 3"` 用于 critic 分支

**连线规则**：
- **同 stage 内相邻节点**（sort_order 相邻）：从源节点右边缘中心 → 目标节点左边缘中心，水平线
- **同 stage 内分支/合并**（非相邻但有 edge）：二次贝塞尔曲线弧线，从源节点上方/下方绕出再绕入
- **跨 stage edge**：从源节点底部中心 → 目标节点顶部中心，使用二次贝塞尔曲线 (`Q` 命令) 连接，曲线控制点位于两节点垂直中点偏右 20px，形成自然弧度。曲线自动穿过 stage 过渡带
- **Worker → Critic 连接**：从 worker 卡片底部 → critic 卡片顶部，短竖线（替代当前虚线 div）

**连线视觉参数**：
- 线宽 `strokeWidth={1.5}`
- 颜色 `stroke="currentColor"`，透明度 `opacity="0.35"`，继承所属 stage 色系
- 箭头使用 `<marker>` 定义，`refX={6} refY={4}`，大小 8×8
- critic 分支线使用虚线 `strokeDasharray="4 3"`
- 所有折线转角改为贝塞尔曲线，避免生硬的直角转折

**位置测量**：
- 复用当前 `StageSwimlane` 中已有的 `useLayoutEffect` + `ResizeObserver` 模式
- 将测量逻辑提升到 panorama page 层级，所有节点位置统一管理
- 使用 `useRef<Map<string, HTMLElement>>` 收集所有可见节点的 DOM 引用
- 每次 resize 时重新计算所有连线路径

### 7. WorkflowPanoramaPage 改造

**布局变更**：
- 画布容器：去掉 `max-w-[1440px] mx-auto`
- 内边距：`p-6` → `p-3`
- 容器改为 `relative`（为 SVG overlay 提供定位上下文）
- Stage 之间间距：`gap-2` → `gap-1`（连线会自然穿过）

**节点位置收集**：
- 新增 `nodeElementRefs`：`useRef<Map<string, HTMLElement>>`，通过 callback ref 收集
- 新增 `criticElementRefs`：同上，收集 critic 卡片的 DOM 位置
- 将 refs 和 onCardClick 传递给 StageLane

**数据流不变**：
- 查询逻辑完全不变（stages / nodes / edges / agents / plugins）
- 排序分组逻辑不变
- 选中状态和 detail panel 逻辑不变

### 8. 间距和尺寸对比

| 属性 | 当前值 | 目标值 |
|------|--------|--------|
| 画布 padding | `p-6` (24px) | `p-3` (12px) |
| stage 间隔 | `gap-2` + DataFlowArrow 占用 ~48px | 6px 渐变过渡带（无 gap） |
| 节点卡片最小宽 | 176px | 120px |
| 节点卡片最小高 | 112px | 72px |
| 节点横向间距 | `gap-3` (12px) | `gap-2.5` (10px) |
| stage 头部高度 | ~60-80px（名称+描述+统计） | ~24px（单行） |
| stage 内边距 | `px-4 py-4` | `px-3 py-2.5` |
| critic 卡片最小高 | 96px | 64px |
| 容器最大宽 | `max-w-[1440px]` | 无限制（自适应） |

### 9. 视觉精炼

**9.1 Stage 过渡带**：相邻 Stage Lane 之间不使用 `gap` 间距，改为 6px 高的渐变过渡条。渐变从上一 stage 的半透明背景色淡出到下一 stage 的半透明背景色：

```css
background: linear-gradient(
  to bottom,
  var(--prev-stage-bg) 0%,
  var(--next-stage-bg) 100%
);
```

过渡带高度 6px，无内边距。连线会自然穿过这个区域。

**9.2 节点选中态**：不采用外发光 `ring` 方案，改用内阴影 + 极淡外阴影：

```css
/* 选中态 */
border-primary/55 
bg-background 
shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]
```

效果是卡片看起来"嵌入"画布而非"浮起发光"。

**9.3 空状态**：Stage lane 内无节点时，显示紧凑引导行（高度 ~32px）：
- 图标（小号） + "No plugins in this stage"
- 如果 workflow 全局无任何节点，改为引导文案 "Add nodes in the editor"

**9.4 间距节奏锁**：
| 层级 | 间距值 |
|------|--------|
| 画布边缘 | 12px |
| 节点卡片之间 | 10px |
| Stage 过渡带 | 6px |
| Stage 内边距（水平/垂直） | 12px / 10px |

### 10. 色系方案

保持当前 `STAGE_STYLES` 的 6 色循环方案，但简化为半透明变体：

```typescript
const STAGE_COLORS = [
  "bg-slate-50/40",
  "bg-stone-50/40",
  "bg-blue-50/40",
  "bg-rose-50/40",
  "bg-violet-50/40",
  "bg-amber-50/40",
] as const;
```

头部标签色保持原色系用于文字和节点内连线颜色。

### 11. 边界情况

- **空 stages**：StageLane 渲染紧凑空状态提示（高度 ~40px）
- **0 个 stage**：画布显示居中空状态文案
- **单 stage**：无跨 stage 连线，仅 stage 内节点连线
- **大量节点**（stage 内 >6 个节点）：stage lane 内部设置 `overflow-x: auto`，节点自身不换行，横向滚动查看
- **跨 stage 无 edge**：源节点到下一 stage 第一个节点画虚线"隐含流"（可选，默认不画）
- **resize**：SVG overlay 通过 ResizeObserver 自动重新测量和绘制

---

## 验证方案

1. **类型检查**：`pnpm typecheck`
2. **单元测试**：`pnpm --filter @multica/views exec vitest run workflows/components/overview/`
   - 更新 panorama-page 测试：验证 stage lanes 渲染、节点渲染、SVG overlay 存在
   - 更新 stage-lane 测试：验证半透明背景、紧凑头部、节点排列
   - 更新 compact-node-card 测试：验证尺寸、精简内容、点击交互
   - 删除 data-flow-arrow 测试
   - 新增 panorama-svg-overlay 测试：验证连线路径生成逻辑
3. **视觉验证**：`pnpm dev:web`
   - 进入 workflow 详情页确认默认全景视图
   - 确认 stage 泳道无硬边框、半透明背景
   - 确认节点到节点有真实 SVG 连线
   - 确认跨 stage 长连线可见
   - 确认首屏无需滚动即可看到主流程
   - 点击节点确认 detail panel 仍正常
4. **完整检查**：`make check`

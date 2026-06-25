# Workflow Panorama 流程图画布重构 — 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 WorkflowPanoramaPage 从"分段卡片列表"重构为"连续流程图画布"，用 SVG overlay 绘制节点到节点的真实连线，弱化 stage 边界为半透明泳道，压缩节点卡片尺寸以提升首屏信息密度。

**Architecture:** 新建 `StageLane`（替代 `StageSwimlane`）、`CompactNodeCard`（替代 `PluginCard`）、`PanoramaSvgOverlay`（核心连线组件），修改 `CriticBadge` 缩小尺寸，删除 `DataFlowArrow`。数据查询和 detail panel 逻辑不变。

**Tech Stack:** React + TypeScript, Vitest + @testing-library/react (jsdom), Tailwind CSS, lucide-react icons

## 全局约束

- 不新增外部布局库依赖
- 保持纵向推进方向（top → bottom）
- CSS 仅使用 Tailwind 工具类（不新增 CSS 文件）
- 遵循现有代码命名规范（English 注释、cn() 工具函数）
- `packages/views/` 不引入 `next/*` 或 `react-router-dom` 导入
- 节点内连接线、跨 stage 连线、critic 连线均由 SVG overlay 统一管理
- 空状态空 stage 时显示紧凑引导行（高度 ~32px）

---

### Task 1: 创建 CompactNodeCard 组件

**Files:**
- Create: `packages/views/workflows/components/overview/compact-node-card.tsx`
- Create: `packages/views/workflows/components/overview/compact-node-card.test.tsx`

**Interfaces:**
- Consumes: `WorkflowNode`, `Agent`, `BuiltinPlugin` 类型（来自 `@multica/core/types` 和 `@multica/core/api/schemas`）
- Produces: `CompactNodeCard` 组件及 `CompactNodeCardProps` 接口

`CompactNodeCardProps`:
```typescript
export interface CompactNodeCardProps {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string, focus: "worker") => void;
  isSelected?: boolean;
  /** callback ref 用于 SVG overlay 测量位置 */
  elementRef?: (el: HTMLDivElement | null) => void;
}
```

- [ ] **Step 1: 编写 compact-node-card.test.tsx 测试文件**

```typescript
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { CompactNodeCard } from "./compact-node-card";
import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_NODE: WorkflowNode = {
  id: "node-1",
  workflow_id: "wf-1",
  title: "brainstorming",
  description: "Brainstorming session",
  position_x: 0, position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-1",
  critic_type: "human",
  critic_id: null,
  critic_api_url: null,
  sort_order: 0,
  stage_id: "stage-1",
  created_at: "", updated_at: "",
};

const MOCK_AGENT: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "Brainstorm Agent",
  description: "Runs brainstorming",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6",
  thinking_level: "medium",
  plugin_id: "plugin-1",
  is_builtin: false,
  owner_id: null,
  skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-1",
  name: "Cospowers Brainstorming",
  description: "Brainstorming plugin",
  slug: "cospowers-brainstorming",
  version: "1.0.0",
  category: "engineering",
};

describe("CompactNodeCard", () => {
  it("renders plugin name from plugin lookup", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Cospowers Brainstorming")).toBeInTheDocument();
  });

  it("falls back to node title when plugin is null", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={null} onClick={onClick} />);
    expect(screen.getByText("brainstorming")).toBeInTheDocument();
  });

  it("does not render plugin description text", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.queryByText("Brainstorming plugin")).not.toBeInTheDocument();
  });

  it("renders agent name and status dot", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Brainstorm Agent")).toBeInTheDocument();
  });

  it("does not render agent model text", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.queryByText(/claude-sonnet/)).not.toBeInTheDocument();
  });

  it("fires onClick with node id and 'worker' focus when clicked", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("compact-node-card-node-1"));
    expect(onClick).toHaveBeenCalledWith("node-1", "worker");
  });

  it("renders without agent info when agent is null", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText("brainstorming")).toBeInTheDocument();
    expect(screen.getByTestId("compact-node-card-node-1")).toBeInTheDocument();
  });

  it("applies selected styling when isSelected is true", () => {
    const onClick = vi.fn();
    render(<CompactNodeCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} isSelected />);
    const card = screen.getByTestId("compact-node-card-node-1");
    expect(card.getAttribute("aria-pressed")).toBe("true");
  });

  it("calls elementRef callback with the DOM element", () => {
    const onClick = vi.fn();
    const refs: (HTMLDivElement | null)[] = [];
    render(<CompactNodeCard node={MOCK_NODE} agent={null} plugin={null} onClick={onClick} elementRef={(el) => refs.push(el)} />);
    expect(refs.length).toBeGreaterThan(0);
    expect(refs[0]).toBeInstanceOf(HTMLDivElement);
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/compact-node-card.test.tsx
```

Expected: FAIL — `CompactNodeCard` 尚未定义

- [ ] **Step 3: 实现 CompactNodeCard 组件**

```typescript
"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";

export interface CompactNodeCardProps {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string, focus: "worker") => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLDivElement | null) => void;
}

const statusDotColors: Record<string, string> = {
  working: "bg-[var(--success)]",
  idle: "bg-[var(--info)]",
  blocked: "bg-[var(--warning)]",
  error: "bg-destructive",
  offline: "bg-muted-foreground/40",
};

export function CompactNodeCard({
  node,
  agent,
  plugin,
  onClick,
  isSelected = false,
  elementRef,
}: CompactNodeCardProps) {
  const displayName = plugin?.name ?? node.title;

  return (
    <button
      type="button"
      data-testid={`compact-node-card-${node.id}`}
      onClick={() => onClick(node.id, "worker")}
      ref={elementRef}
      className={cn(
        "group flex min-h-[72px] min-w-[120px] shrink-0 flex-col gap-1.5 rounded-xl border bg-card/95 p-2.5 text-left transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-primary/45 hover:bg-background hover:shadow-[0_8px_20px_rgba(15,23,42,0.06)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected &&
          "border-primary/55 bg-background shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]",
      )}
      aria-pressed={isSelected}
    >
      <span className="block truncate text-xs font-semibold text-foreground">
        {displayName}
      </span>

      {agent && (
        <div className="mt-auto flex items-center gap-1.5">
          <span
            className={cn(
              "inline-block h-1.5 w-1.5 shrink-0 rounded-full",
              statusDotColors[agent.status] ?? "bg-muted-foreground/40",
            )}
          />
          <span className="truncate text-[11px] text-muted-foreground">
            {agent.name}
          </span>
        </div>
      )}
    </button>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/compact-node-card.test.tsx
```

Expected: PASS — 所有 8 个测试通过

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/compact-node-card.tsx packages/views/workflows/components/overview/compact-node-card.test.tsx
git commit -m "feat(workflows): add CompactNodeCard component for panorama flow canvas"
```

---

### Task 2: 更新 CriticBadge 组件

**Files:**
- Modify: `packages/views/workflows/components/overview/critic-badge.tsx`
- Modify: `packages/views/workflows/components/overview/critic-badge.test.tsx`

**Interfaces:**
- Consumes: 现有 `CriticBadgeProps`（不变）
- Produces: 更新后的 `CriticBadgeProps`（增加可选 `elementRef`）

`CriticBadgeProps` 新增字段:
```typescript
elementRef?: (el: HTMLButtonElement | null) => void;
```

- [ ] **Step 1: 更新 critic-badge.test.tsx — 添加尺寸和 elementRef 测试，移除 ArrowUpRight/MODEL 断言**

```typescript
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { CriticBadge } from "./critic-badge";
import type { WorkflowNode, Agent } from "@multica/core/types";

const MOCK_CRITIC_NODE: WorkflowNode = {
  id: "critic-1",
  workflow_id: "wf-1",
  title: "evaluator",
  description: "",
  position_x: 0, position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-critic-1",
  critic_type: "agent",
  critic_id: "agent-critic-2",
  critic_api_url: null,
  sort_order: 0,
  stage_id: "stage-1",
  created_at: "", updated_at: "",
};

const MOCK_CRITIC_AGENT: Agent = {
  id: "agent-critic-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "Reviewer",
  description: "Code reviewer",
  instructions: "", avatar_url: null,
  runtime_mode: "cloud", runtime_config: {},
  custom_env: {}, custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace", status: "idle",
  max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6",
  thinking_level: "",
  plugin_id: null,
  is_builtin: true,
  owner_id: null, skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

describe("CriticBadge", () => {
  it("renders with dashed border style", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    const btn = screen.getByTestId("critic-badge-critic-1");
    expect(btn.className).toContain("border-dashed");
  });

  it("renders critic agent name", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
  });

  it("falls back to node title when critic agent is null", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={null} onClick={onClick} />);
    expect(screen.getByText("evaluator")).toBeInTheDocument();
  });

  it("does not render model text (removed in compact version)", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    expect(screen.queryByText(/claude-sonnet/)).not.toBeInTheDocument();
  });

  it("does not render ArrowUpRight icon (removed in compact version)", () => {
    const onClick = vi.fn();
    const { container } = render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    // ArrowUpRight icon no longer present
    const arrows = container.querySelectorAll("svg");
    const hasArrowUpRight = Array.from(arrows).some(
      (svg) => svg.outerHTML.includes("ArrowUpRight") || svg.getAttribute("class")?.includes("lucide-arrow-up")
    );
    expect(hasArrowUpRight).toBe(false);
  });

  it("fires onClick when clicked", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("critic-badge-critic-1"));
    expect(onClick).toHaveBeenCalledWith("critic-1", "critic");
  });

  it("calls elementRef callback with the DOM element", () => {
    const onClick = vi.fn();
    const refs: (HTMLButtonElement | null)[] = [];
    render(
      <CriticBadge
        node={MOCK_CRITIC_NODE}
        criticAgent={MOCK_CRITIC_AGENT}
        onClick={onClick}
        elementRef={(el) => refs.push(el)}
      />,
    );
    expect(refs.length).toBeGreaterThan(0);
    expect(refs[0]).toBeInstanceOf(HTMLButtonElement);
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/critic-badge.test.tsx
```

Expected: FAIL — 现有实现仍有 ArrowUpRight + model 文字，新测试会失败

- [ ] **Step 3: 更新 CriticBadge 实现 — 缩小尺寸、去除 ArrowUpRight 和 model、添加 elementRef**

```typescript
"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ShieldAlert } from "lucide-react";

export interface CriticBadgeProps {
  node: WorkflowNode;
  criticAgent: Agent | null;
  onClick: (nodeId: string, focus: "critic") => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLButtonElement | null) => void;
}

export function CriticBadge({
  node,
  criticAgent,
  onClick,
  isSelected = false,
  elementRef,
}: CriticBadgeProps) {
  const displayName = criticAgent?.name ?? node.title;

  return (
    <button
      type="button"
      data-testid={`critic-badge-${node.id}`}
      onClick={() => onClick(node.id, "critic")}
      ref={elementRef}
      className={cn(
        "group flex min-h-[64px] min-w-[120px] shrink-0 flex-col gap-1.5 rounded-xl border-2 border-dashed border-[var(--warning)]/45 bg-[var(--warning)]/6 p-2.5 text-left transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-[var(--warning)]/75 hover:bg-[var(--warning)]/11 hover:shadow-[0_8px_18px_rgba(245,158,11,0.10)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected &&
          "border-[var(--warning)]/85 bg-[var(--warning)]/12 ring-1 ring-[var(--warning)]/20",
      )}
      aria-pressed={isSelected}
    >
      <span className="inline-flex items-center gap-1 rounded-full bg-[var(--warning)]/12 px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-amber-800">
        <ShieldAlert className="h-3 w-3" strokeWidth={1.9} />
        Critic
      </span>
      <span className="block truncate text-xs font-semibold text-foreground">
        {displayName}
      </span>
    </button>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/critic-badge.test.tsx
```

Expected: PASS — 所有 7 个测试通过

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/critic-badge.tsx packages/views/workflows/components/overview/critic-badge.test.tsx
git commit -m "feat(workflows): compact CriticBadge for panorama flow canvas"
```

---

### Task 3: 创建 StageLane 组件

**Files:**
- Create: `packages/views/workflows/components/overview/stage-lane.tsx`
- Create: `packages/views/workflows/components/overview/stage-lane.test.tsx`

**Interfaces:**
- Consumes: `WorkflowStage`, `WorkflowNode`, `Agent`, `WorkflowEdge`, `BuiltinPlugin` 类型
- Produces: `StageLane` 组件及 `StageLaneProps` 接口

`StageLaneProps`:
```typescript
export interface StageLaneProps {
  stage: WorkflowStage;
  nodeIds: string[];  // nodes belonging to this stage, sorted by sort_order
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string, focus: "worker" | "critic") => void;
  selectedCard?: { nodeId: string; focus: "worker" | "critic" } | null;
  /** callback refs for SVG overlay position measurement */
  nodeElementRefs: Map<string, (el: HTMLDivElement | null) => void>;
  criticElementRefs: Map<string, (el: HTMLButtonElement | null) => void>;
}
```

Note: StageLane 不再自行查询 edges 和渲染节点间连线/arc edges —— 这些由 PanoramaSvgOverlay 负责。StageLane 仅负责渲染阶段头部 + 节点卡片列表。

- [ ] **Step 1: 编写 stage-lane.test.tsx 测试**

```typescript
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { StageLane } from "./stage-lane";
import type { WorkflowStage, WorkflowNode, Agent, WorkflowEdge } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_STAGE: WorkflowStage = {
  id: "stage-1",
  workflow_id: "wf-1",
  name: "Intake",
  description: "First workflow stage",
  sort_order: 0,
  node_count: 2,
  created_at: "", updated_at: "",
};

const MOCK_NODES: WorkflowNode[] = [
  {
    id: "n1", workflow_id: "wf-1", title: "brainstorming", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: "agent-1",
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "",
  },
  {
    id: "n2", workflow_id: "wf-1", title: "session-context", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: "agent-2",
    critic_type: "agent", critic_id: "agent-critic", critic_api_url: null,
    sort_order: 1, stage_id: "stage-1", created_at: "", updated_at: "",
  },
];

const MOCK_AGENT: Agent = {
  id: "agent-1", workspace_id: "ws-1", runtime_id: "rt-1",
  name: "Brainstorm Agent", description: "Plans",
  instructions: "", avatar_url: null,
  runtime_mode: "cloud", runtime_config: {},
  custom_env: {}, custom_args: [], custom_env_redacted: false,
  visibility: "workspace", status: "idle", max_concurrent_tasks: 1,
  model: "claude-sonnet-4-6", thinking_level: "",
  plugin_id: "plugin-1", is_builtin: false, owner_id: null,
  skills: [], created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-1", name: "Cospowers Brainstorming",
  description: "Brainstorming plugin", slug: "cospowers-brainstorming",
  version: "1.0.0", category: "engineering",
};

describe("StageLane", () => {
  const agentLookup = new Map<string, Agent | null>([["agent-1", MOCK_AGENT], ["agent-2", null]]);
  const pluginLookup = new Map<string, BuiltinPlugin | null>([["plugin-1", MOCK_PLUGIN]]);
  const emptyRefs = new Map();

  it("renders stage name as compact header", () => {
    const onCardClick = vi.fn();
    render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={MOCK_NODES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    expect(screen.getByText("Intake")).toBeTruthy();
  });

  it("does not render stage description", () => {
    const onCardClick = vi.fn();
    const { container } = render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={MOCK_NODES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    expect(container.textContent).not.toContain("First workflow stage");
  });

  it("renders CompactNodeCard for each node", () => {
    const onCardClick = vi.fn();
    render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={MOCK_NODES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    expect(screen.getByTestId("compact-node-card-n1")).toBeTruthy();
    expect(screen.getByTestId("compact-node-card-n2")).toBeTruthy();
  });

  it("renders CriticBadge for nodes with critic attachment", () => {
    const onCardClick = vi.fn();
    render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={MOCK_NODES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    expect(screen.getByTestId("critic-badge-n2")).toBeTruthy();
  });

  it("fires onCardClick when a node card is clicked", () => {
    const onCardClick = vi.fn();
    render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={MOCK_NODES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    fireEvent.click(screen.getByTestId("compact-node-card-n1"));
    expect(onCardClick).toHaveBeenCalledWith("n1", "worker");
  });

  it("shows compact empty state when no nodes", () => {
    const onCardClick = vi.fn();
    render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={[]}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    expect(screen.getByTestId("stage-lane-empty")).toBeTruthy();
  });

  it("has no card border or shadow on stage container", () => {
    const onCardClick = vi.fn();
    render(
      <StageLane
        stage={MOCK_STAGE}
        nodeIds={MOCK_NODES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
        nodeElementRefs={emptyRefs}
        criticElementRefs={emptyRefs}
      />,
    );
    const section = screen.getByTestId("stage-lane-stage-1");
    expect(section.className).not.toContain("border-l-[6px]");
    expect(section.className).not.toContain("rounded-2xl");
    expect(section.className).not.toContain("shadow-");
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/stage-lane.test.tsx
```

Expected: FAIL — `StageLane` 尚未定义

- [ ] **Step 3: 实现 StageLane 组件**

```typescript
"use client";

import { useMemo } from "react";
import type { WorkflowStage, WorkflowNode, Agent, WorkflowEdge } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import { CompactNodeCard } from "./compact-node-card";
import { CriticBadge } from "./critic-badge";

export interface StageLaneProps {
  stage: WorkflowStage;
  nodeIds: WorkflowNode[];
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string, focus: "worker" | "critic") => void;
  selectedCard?: { nodeId: string; focus: "worker" | "critic" } | null;
  nodeElementRefs: Map<string, (el: HTMLDivElement | null) => void>;
  criticElementRefs: Map<string, (el: HTMLButtonElement | null) => void>;
}

const STAGE_BG_COLORS = [
  "bg-slate-50/40",
  "bg-stone-50/40",
  "bg-blue-50/40",
  "bg-rose-50/40",
  "bg-violet-50/40",
  "bg-amber-50/40",
] as const;

const STAGE_LABEL_COLORS = [
  "text-slate-800",
  "text-stone-800",
  "text-blue-900",
  "text-rose-900",
  "text-violet-900",
  "text-amber-900",
] as const;

export function StageLane({
  stage,
  nodeIds,
  agentLookup,
  pluginLookup,
  onCardClick,
  selectedCard = null,
  nodeElementRefs,
  criticElementRefs,
}: StageLaneProps) {
  const { t } = useT("workflows");
  const colorIndex = Math.abs(stage.sort_order) % STAGE_BG_COLORS.length;
  const stageBg = STAGE_BG_COLORS[colorIndex] ?? STAGE_BG_COLORS[0];
  const labelColor = STAGE_LABEL_COLORS[colorIndex] ?? STAGE_LABEL_COLORS[0];

  const hasCriticAttachment = (node: WorkflowNode) =>
    Boolean(node.critic_id || node.critic_api_url);

  const sortedNodes = useMemo(
    () => [...nodeIds].sort((a, b) => a.sort_order - b.sort_order),
    [nodeIds],
  );

  return (
    <section
      data-testid={`stage-lane-${stage.id}`}
      className={cn("px-3 py-2.5", stageBg)}
    >
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Stage {stage.sort_order + 1}
        </span>
        <span className={cn("text-xs font-semibold tracking-tight", labelColor)}>
          {stage.name}
        </span>
      </div>

      {sortedNodes.length === 0 ? (
        <div
          data-testid="stage-lane-empty"
          className="flex h-8 items-center text-[11px] text-muted-foreground"
        >
          No plugins in this stage
        </div>
      ) : (
        <div className="flex items-start gap-2.5 flex-nowrap overflow-x-auto">
          {sortedNodes.map((node) => {
            const agent = agentLookup.get(node.worker_id ?? "") ?? null;
            const plugin = agent?.plugin_id
              ? pluginLookup.get(agent.plugin_id) ?? null
              : null;
            const criticAgent = node.critic_id
              ? agentLookup.get(node.critic_id) ?? null
              : null;

            return (
              <div key={node.id} className="flex flex-col items-start gap-1.5">
                <CompactNodeCard
                  node={node}
                  agent={agent}
                  plugin={plugin}
                  onClick={onCardClick}
                  isSelected={
                    selectedCard?.nodeId === node.id &&
                    selectedCard.focus === "worker"
                  }
                  elementRef={nodeElementRefs.get(node.id)}
                />
                {hasCriticAttachment(node) && (
                  <div className="ml-4">
                    <CriticBadge
                      node={node}
                      criticAgent={criticAgent}
                      onClick={onCardClick}
                      isSelected={
                        selectedCard?.nodeId === node.id &&
                        selectedCard.focus === "critic"
                      }
                      elementRef={criticElementRefs.get(node.id)}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/stage-lane.test.tsx
```

Expected: PASS — 所有 7 个测试通过

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/stage-lane.tsx packages/views/workflows/components/overview/stage-lane.test.tsx
git commit -m "feat(workflows): add StageLane component for panorama flow canvas"
```

---

### Task 4: 创建 PanoramaSvgOverlay 组件

**Files:**
- Create: `packages/views/workflows/components/overview/panorama-svg-overlay.tsx`
- Create: `packages/views/workflows/components/overview/panorama-svg-overlay.test.tsx`

**Interfaces:**
- Consumes: `WorkflowEdge`, `WorkflowNode` 类型
- Produces: `PanoramaSvgOverlay` 组件及 `PanoramaSvgOverlayProps` 接口

`PanoramaSvgOverlayProps`:
```typescript
export interface PanoramaSvgOverlayProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  nodePositions: Map<string, DOMRect>;
  criticPositions: Map<string, DOMRect>;
  containerRef: React.RefObject<HTMLElement | null>;
}
```

`PanoramaSvgOverlay`:
- `position: absolute; inset: 0; pointer-events: none`
- 接收预计算的 node/critic 位置（由父组件通过 ResizeObserver 计算）
- 在 `<svg>` 内根据 edge 关系生成 `<path>` 元素
- 连线逻辑：
  - 同 stage 内相邻节点 → 水平线
  - 同 stage 内非相邻分支 → 贝塞尔弧线
  - 跨 stage edge → 竖线贝塞尔曲线
  - worker → critic → 短竖线（虚线）

- [ ] **Step 1: 编写 panorama-svg-overlay.test.tsx 测试**

由于 SVG overlay 依赖 DOM 位置测量，测试应聚焦于路径生成逻辑的纯函数。将路径计算逻辑提取为可测试的辅助函数。

```typescript
// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { computeEdgePaths, type EdgePath } from "./panorama-svg-overlay";
import type { WorkflowEdge, WorkflowNode } from "@multica/core/types";

const MOCK_NODES: WorkflowNode[] = [
  {
    id: "n1", workflow_id: "wf-1", title: "A", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "",
  },
  {
    id: "n2", workflow_id: "wf-1", title: "B", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 1, stage_id: "stage-1", created_at: "", updated_at: "",
  },
  {
    id: "n3", workflow_id: "wf-1", title: "C", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 0, stage_id: "stage-2", created_at: "", updated_at: "",
  },
];

const MOCK_EDGES: WorkflowEdge[] = [
  {
    id: "e1", workflow_id: "wf-1",
    source_node_id: "n1", target_node_id: "n2",
    condition: null, created_at: "",
  },
  {
    id: "e2", workflow_id: "wf-1",
    source_node_id: "n2", target_node_id: "n3",
    condition: null, created_at: "",
  },
];

function fakeRect(x: number, y: number, w: number, h: number): DOMRect {
  return { x, y, left: x, top: y, right: x + w, bottom: y + h, width: w, height: h, toJSON() { return this; } };
}

describe("computeEdgePaths", () => {
  it("returns empty array when positions are empty", () => {
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, new Map(), new Map());
    expect(paths).toEqual([]);
  });

  it("computes horizontal path for same-stage adjacent nodes", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), MOCK_NODES, positions, new Map());
    expect(paths.length).toBe(1);
    expect(paths[0]!.type).toBe("horizontal");
    expect(paths[0]!.d).toContain("M");
  });

  it("computes cross-stage bezier path for edges between stages", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
      ["n3", fakeRect(130, 200, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, positions, new Map());
    const crossStage = paths.filter((p) => p?.type === "cross-stage");
    expect(crossStage.length).toBe(1);
    expect(crossStage[0]!.d).toContain("Q");
  });

  it("computes critic dashed line for worker-critic pairs", () => {
    const nodePositions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const criticPositions = new Map<string, DOMRect>([
      ["n1", fakeRect(20, 80, 120, 64)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), MOCK_NODES, nodePositions, criticPositions);
    const criticPaths = paths.filter((p) => p?.type === "critic");
    expect(criticPaths.length).toBe(1);
    expect(criticPaths[0]!.dashed).toBe(true);
  });

  it("returns empty for edges with missing node positions", () => {
    const positions = new Map<string, DOMRect>([["n1", fakeRect(0, 0, 120, 72)]]);
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, positions, new Map());
    expect(paths.length).toBe(0);
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/panorama-svg-overlay.test.tsx
```

Expected: FAIL — `computeEdgePaths` 尚未定义

- [ ] **Step 3: 实现 PanoramaSvgOverlay 组件 + computeEdgePaths 纯函数**

```typescript
"use client";

import { useMemo } from "react";
import type { WorkflowEdge, WorkflowNode } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export interface EdgePath {
  edgeId: string;
  d: string;
  type: "horizontal" | "cross-stage" | "arc" | "critic";
  dashed: boolean;
  colorIndex: number;
}

export interface PanoramaSvgOverlayProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  nodePositions: Map<string, DOMRect>;
  criticPositions: Map<string, DOMRect>;
  className?: string;
}

/** Pure function: compute SVG path strings from edge + position data. */
export function computeEdgePaths(
  edges: WorkflowEdge[],
  nodes: WorkflowNode[],
  nodePositions: Map<string, DOMRect>,
  criticPositions: Map<string, DOMRect>,
): EdgePath[] {
  const nodeMap = new Map(nodes.map((n) => [n.id, n]));
  const results: EdgePath[] = [];

  for (const edge of edges) {
    const sourceNode = nodeMap.get(edge.source_node_id);
    const targetNode = nodeMap.get(edge.target_node_id);
    if (!sourceNode || !targetNode) continue;

    const sourceRect = nodePositions.get(edge.source_node_id);
    const targetRect = nodePositions.get(edge.target_node_id);
    if (!sourceRect || !targetRect) continue;

    const colorIndex = Math.abs(sourceNode.sort_order) % 6;
    const isSameStage = sourceNode.stage_id === targetNode.stage_id;
    const isAdjacent = Math.abs(sourceNode.sort_order - targetNode.sort_order) === 1;

    if (isSameStage && isAdjacent) {
      // Horizontal: source right edge center -> target left edge center
      const x1 = sourceRect.right;
      const y1 = sourceRect.top + sourceRect.height / 2;
      const x2 = targetRect.left;
      const y2 = y1;
      results.push({
        edgeId: edge.id,
        d: `M ${x1} ${y1} L ${x2} ${y2}`,
        type: "horizontal",
        dashed: false,
        colorIndex,
      });
    } else if (!isSameStage) {
      // Cross-stage: source bottom center -> target top center, bezier curve
      const x1 = sourceRect.left + sourceRect.width / 2;
      const y1 = sourceRect.bottom;
      const x2 = targetRect.left + targetRect.width / 2;
      const y2 = targetRect.top;
      const cpX = (x1 + x2) / 2 + 20;
      const cpY = (y1 + y2) / 2;
      results.push({
        edgeId: edge.id,
        d: `M ${x1} ${y1} Q ${cpX} ${cpY} ${x2} ${y2}`,
        type: "cross-stage",
        dashed: false,
        colorIndex,
      });
    } else {
      // Same stage, non-adjacent: arc bezier
      const x1 = sourceRect.right;
      const y1 = sourceRect.top + sourceRect.height / 2;
      const x2 = targetRect.left;
      const y2 = targetRect.top + targetRect.height / 2;
      const cpY = y1 - 30;
      results.push({
        edgeId: edge.id,
        d: `M ${x1} ${y1} Q ${x1 + 20} ${cpY} ${x2 - 20} ${cpY} Q ${x2} ${cpY} ${x2} ${y2}`,
        type: "arc",
        dashed: false,
        colorIndex,
      });
    }
  }

  // Critic connections: worker card bottom -> critic card top
  for (const [nodeId, criticRect] of criticPositions) {
    const nodeRect = nodePositions.get(nodeId);
    if (!nodeRect) continue;
    const x1 = nodeRect.left + nodeRect.width / 2;
    const y1 = nodeRect.bottom;
    const x2 = criticRect.left + criticRect.width / 2;
    const y2 = criticRect.top;
    results.push({
      edgeId: `critic-${nodeId}`,
      d: `M ${x1} ${y1} L ${x2} ${y2}`,
      type: "critic",
      dashed: true,
      colorIndex: 0,
    });
  }

  return results;
}

const STAGE_LINE_COLORS = [
  "text-slate-400",
  "text-stone-400",
  "text-blue-400",
  "text-rose-300",
  "text-violet-400",
  "text-amber-400",
] as const;

export function PanoramaSvgOverlay({
  edges,
  nodes,
  nodePositions,
  criticPositions,
  className,
}: PanoramaSvgOverlayProps) {
  const paths = useMemo(
    () => computeEdgePaths(edges, nodes, nodePositions, criticPositions),
    [edges, nodes, nodePositions, criticPositions],
  );

  if (paths.length === 0) return null;

  return (
    <svg
      className={cn("absolute inset-0 pointer-events-none", className)}
      width="100%"
      height="100%"
      aria-hidden="true"
    >
      <defs>
        <marker
          id="panorama-arrowhead"
          viewBox="0 0 10 10"
          refX={6}
          refY={5}
          markerWidth={8}
          markerHeight={8}
          orient="auto-start-reverse"
        >
          <path
            d="M 0 0 L 10 5 L 0 10 z"
            fill="currentColor"
            opacity={0.35}
          />
        </marker>
      </defs>
      {paths.map((path) => (
        <path
          key={path.edgeId}
          d={path.d}
          className={cn(
            STAGE_LINE_COLORS[path.colorIndex] ?? STAGE_LINE_COLORS[0],
            "opacity-35",
          )}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          strokeDasharray={path.dashed ? "4 3" : undefined}
          markerEnd="url(#panorama-arrowhead)"
        />
      ))}
    </svg>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/panorama-svg-overlay.test.tsx
```

Expected: PASS — 所有 5 个测试通过

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/panorama-svg-overlay.tsx packages/views/workflows/components/overview/panorama-svg-overlay.test.tsx
git commit -m "feat(workflows): add PanoramaSvgOverlay for edge line rendering"
```

---

### Task 5: 重写 WorkflowPanoramaPage

**Files:**
- Modify: `packages/views/workflows/components/overview/workflow-panorama-page.tsx`
- Modify: `packages/views/workflows/components/overview/panorama-page.test.tsx`

**Interfaces:**
- Consumes: PanoramaSvgOverlay, StageLane, ArchitectureDetailPanel; 现有查询 hooks
- Produces: 更新后的 `WorkflowPanoramaPage` 组件（接口不变）

关键变更：
1. 移除 `DataFlowArrow` 和 `StageSwimlane` 导入
2. 引入 `StageLane`、`PanoramaSvgOverlay`、`CompactNodeCard`、`CriticBadge`
3. 添加 `nodeElementRefs` / `criticElementRefs` callback ref maps
4. 添加 `useLayoutEffect` + `ResizeObserver` 用于测量节点位置
5. 画布容器改为 `relative`，去掉 `max-w-[1440px]`
6. Stage 过渡带使用 6px 渐变 div

- [ ] **Step 1: 更新 panorama-page.test.tsx 测试**

```typescript
// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, cleanup, screen, within } from "@testing-library/react";
import { renderWithI18n } from "../../../test/i18n";

const MOCK_WORKFLOW = { id: "wf-1", title: "Test Workflow" };

const MOCK_STAGES = [
  { id: "stage-1", workflow_id: "wf-1", name: "Intake", description: "", sort_order: 0, node_count: 2, created_at: "", updated_at: "" },
  { id: "stage-2", workflow_id: "wf-1", name: "Analysis", description: "", sort_order: 1, node_count: 1, created_at: "", updated_at: "" },
];

const MOCK_NODES = [
  { id: "n1", workflow_id: "wf-1", title: "brainstorming", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent" as const, worker_id: "agent-1", critic_type: "agent" as const, critic_id: "agent-2", critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n2", workflow_id: "wf-1", title: "session-context", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent" as const, worker_id: "agent-2", critic_type: "human", critic_id: null, critic_api_url: null, sort_order: 1, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n3", workflow_id: "wf-1", title: "requirement-analysis", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent" as const, worker_id: "agent-3", critic_type: "human", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-2", created_at: "", updated_at: "" },
];

const MOCK_EDGES = [
  { id: "e1", workflow_id: "wf-1", source_node_id: "n1", target_node_id: "n2", condition: null, created_at: "" },
  { id: "e2", workflow_id: "wf-1", source_node_id: "n2", target_node_id: "n3", condition: null, created_at: "" },
];

const MOCK_AGENTS = [
  { id: "agent-1", workspace_id: "ws-1", runtime_id: "rt-1", name: "Brainstorming Agent", description: "Brainstorms", instructions: "", avatar_url: null, runtime_mode: "cloud" as const, runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace" as const, status: "idle" as const, max_concurrent_tasks: 1, model: "claude-sonnet-4-6", thinking_level: "medium", plugin_id: "plugin-1", is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
  { id: "agent-2", workspace_id: "ws-1", runtime_id: "rt-1", name: "Session Agent", description: "Session context", instructions: "", avatar_url: null, runtime_mode: "cloud" as const, runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace" as const, status: "working" as const, max_concurrent_tasks: 1, model: "claude-opus-4-8", thinking_level: "", plugin_id: "plugin-2", is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
  { id: "agent-3", workspace_id: "ws-1", runtime_id: "rt-1", name: "Analysis Agent", description: "Requirements analysis", instructions: "", avatar_url: null, runtime_mode: "local" as const, runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace" as const, status: "idle" as const, max_concurrent_tasks: 2, model: "claude-haiku-4-5-20251001", thinking_level: "", plugin_id: null, is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
];

const MOCK_PLUGINS = {
  items: [
    { id: "plugin-1", name: "Cospowers Brainstorming", description: "Brainstorming plugin", slug: "cospowers-brainstorming", version: "1.0.0", category: "engineering" },
    { id: "plugin-2", name: "Cospowers Session", description: "Session context plugin", slug: "cospowers-session", version: "1.0.0", category: "engineering" },
  ],
  total: 2, page: 1, pageSize: 100, hasMore: false,
};

const mocks = vi.hoisted(() => ({
  workflowData: undefined as unknown,
  stagesData: undefined as unknown as unknown[],
  nodesData: undefined as unknown as unknown[],
  edgesData: undefined as unknown as unknown[],
  agentsData: undefined as unknown as unknown[],
  pluginsData: undefined as unknown,
  isLoading: false,
  isError: false,
  navigationPush: vi.fn(),
  setViewMode: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const key = opts.queryKey ?? [];
    if (Array.isArray(key) && key.includes("stages")) return { data: mocks.stagesData, isLoading: mocks.isLoading, isError: false };
    if (Array.isArray(key) && key.includes("nodes")) return { data: mocks.nodesData, isLoading: false };
    if (Array.isArray(key) && key.includes("edges")) return { data: mocks.edgesData, isLoading: false };
    if (Array.isArray(key) && key.includes("agents") && !key.includes("plugins")) return { data: mocks.agentsData, isLoading: false };
    if (Array.isArray(key) && key.includes("plugins")) return { data: mocks.pluginsData, isLoading: false };
    return { data: mocks.workflowData, isLoading: mocks.isLoading, isError: mocks.isError, refetch: vi.fn() };
  },
  useMutation: () => ({ mutateAsync: vi.fn(), mutate: vi.fn(), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/workflows/queries", () => ({
  workflowOverviewOptions: (wsId: string, id: string) => ({ queryKey: ["workflows", wsId, "detail", id] }),
  workflowStagesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "stages"] }),
  workflowNodesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "nodes"] }),
  workflowEdgesOptions: (wsId: string, workflowId: string) => ({ queryKey: ["workflows", wsId, workflowId, "edges"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({ queryKey: ["workspaces", wsId, "agents"] }),
  builtinPluginListOptions: () => ({ queryKey: ["plugins", "builtin"] }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ workflowDetail: (id: string) => `/ws-1/workflows/${id}`, workflows: () => "/ws-1/workflows" }),
}));

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({ push: mocks.navigationPush, replace: mocks.navigationPush }),
}));

vi.mock("@multica/core/workflows/stores/view-store", () => ({
  useWorkflowViewStore: (selector: (s: unknown) => unknown) =>
    selector({ viewMode: "panorama", setViewMode: mocks.setViewMode }),
}));

// Mock ResizeObserver for tests
class ResizeObserverMock {
  observe() {}
  disconnect() {}
  unobserve() {}
}
globalThis.ResizeObserver = ResizeObserverMock as typeof ResizeObserver;

// Provide minimal bounding rects for all nodes so SVG overlay can compute edges
const originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect;
HTMLElement.prototype.getBoundingClientRect = function mockRect() {
  const testId = (this as HTMLElement).getAttribute?.("data-testid") ?? "";
  if (testId.includes("compact-node-card-n1")) {
    return { x: 12, y: 62, left: 12, top: 62, right: 132, bottom: 134, width: 120, height: 72, toJSON() { return this; } };
  }
  if (testId.includes("compact-node-card-n2")) {
    return { x: 142, y: 62, left: 142, top: 62, right: 262, bottom: 134, width: 120, height: 72, toJSON() { return this; } };
  }
  if (testId.includes("compact-node-card-n3")) {
    return { x: 12, y: 218, left: 12, top: 218, right: 132, bottom: 290, width: 120, height: 72, toJSON() { return this; } };
  }
  if (testId.includes("critic-badge-n1")) {
    return { x: 24, y: 142, left: 24, top: 142, right: 144, bottom: 206, width: 120, height: 64, toJSON() { return this; } };
  }
  return originalGetBoundingClientRect.call(this);
};

import { WorkflowPanoramaPage } from "./workflow-panorama-page";

describe("WorkflowPanoramaPage", () => {
  beforeEach(() => {
    mocks.workflowData = MOCK_WORKFLOW;
    mocks.stagesData = MOCK_STAGES;
    mocks.nodesData = MOCK_NODES;
    mocks.edgesData = MOCK_EDGES;
    mocks.agentsData = MOCK_AGENTS;
    mocks.pluginsData = MOCK_PLUGINS;
    mocks.isLoading = false;
    mocks.isError = false;
    mocks.navigationPush = vi.fn();
    mocks.setViewMode = vi.fn();
    cleanup();
  });

  it("renders workflow title in header", () => {
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(container.querySelector("h1")?.textContent).toBe("Test Workflow");
  });

  it("renders stage lanes for each stage", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.getByTestId("stage-lane-stage-1")).toBeTruthy();
    expect(screen.getByTestId("stage-lane-stage-2")).toBeTruthy();
  });

  it("renders compact node cards with resolved names", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.getByTestId("compact-node-card-n1")).toBeTruthy();
    expect(screen.getByTestId("compact-node-card-n2")).toBeTruthy();
    expect(screen.getByTestId("compact-node-card-n3")).toBeTruthy();
  });

  it("no longer renders DataFlowArrow between stages", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.queryByTestId("data-flow-arrow")).toBeNull();
  });

  it("renders stage transition gradient between stages", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.queryAllByTestId("stage-transition-gradient").length).toBeGreaterThan(0);
  });

  it("opens detail panel on node card click", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    fireEvent.click(screen.getByTestId("compact-node-card-n1"));
    expect(screen.getByTestId("architecture-detail-panel")).toBeTruthy();
  });

  it("opens critic detail panel on critic badge click", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    fireEvent.click(screen.getByTestId("critic-badge-n1"));
    const panel = screen.getByTestId("architecture-detail-panel");
    expect(panel).toBeTruthy();
    expect(within(panel).getAllByText("Critic").length).toBeGreaterThan(0);
  });

  it("shows loading skeleton when loading", () => {
    mocks.isLoading = true;
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(container.querySelector('[data-testid="panorama-skeleton"]')).toBeTruthy();
  });

  it("shows error alert when workflow fails", () => {
    mocks.workflowData = undefined;
    mocks.isError = true;
    const { container } = renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(container.querySelector('[role="alert"]')).toBeTruthy();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/panorama-page.test.tsx
```

Expected: FAIL — `DataFlowArrow` 测试断言失败（`data-flow-arrow` testid 不再存在）

- [ ] **Step 3: 重写 WorkflowPanoramaPage**

```typescript
"use client";

import { useState, useMemo, useLayoutEffect, useRef, useCallback, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowOverviewOptions,
  workflowStagesOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
} from "@multica/core/workflows/queries";
import { agentListOptions, builtinPluginListOptions } from "@multica/core/workspace/queries";
import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { useNavigation } from "../../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PageHeader } from "../../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { AlertCircle, ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { useT } from "../../../i18n";
import { StageLane } from "./stage-lane";
import { PanoramaSvgOverlay } from "./panorama-svg-overlay";
import {
  ArchitectureDetailPanel,
  type ArchitectureDetailPanelData,
} from "./architecture-detail-panel";
import type { Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { PanelsTopLeft } from "lucide-react";

export interface WorkflowPanoramaPageProps {
  workflowId: string;
  viewToggle?: ReactNode;
}

type PanoramaSelection = {
  nodeId: string;
  focus: "worker" | "critic";
};

function PanoramaSkeleton() {
  return (
    <div className="flex flex-col gap-4 p-3" data-testid="panorama-skeleton">
      <Skeleton className="h-8 w-64" />
      {Array.from({ length: 3 }).map((_, i) => (
        <Skeleton key={i} className="h-24 w-full" />
      ))}
    </div>
  );
}

const STAGE_BG_COLORS = [
  "from-slate-50/40",
  "from-stone-50/40",
  "from-blue-50/40",
  "from-rose-50/40",
  "from-violet-50/40",
  "from-amber-50/40",
] as const;

export function WorkflowPanoramaPage({ workflowId, viewToggle }: WorkflowPanoramaPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();
  const setViewMode = useWorkflowViewStore((s) => s.setViewMode);

  const [selectedCard, setSelectedCard] = useState<PanoramaSelection | null>(null);

  // ── Queries ──
  const {
    data: workflow,
    isLoading: workflowLoading,
    isError: workflowError,
    refetch: workflowRefetch,
  } = useQuery(workflowOverviewOptions(wsId, workflowId));

  const { data: stages = [], isLoading: stagesLoading } = useQuery(
    workflowStagesOptions(wsId, workflowId),
  );

  const { data: nodes = [], isLoading: nodesLoading } = useQuery(
    workflowNodesOptions(wsId, workflowId),
  );

  const { data: edges = [] } = useQuery(
    workflowEdgesOptions(wsId, workflowId),
  );

  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const { data: pluginsData } = useQuery(builtinPluginListOptions());

  const isLoading = workflowLoading || stagesLoading || nodesLoading;

  // ── Derived lookups ──
  const agentLookup = useMemo(() => {
    const map = new Map<string, Agent | null>();
    for (const a of agents) map.set(a.id, a);
    return map;
  }, [agents]);

  const pluginLookup = useMemo(() => {
    const map = new Map<string, BuiltinPlugin | null>();
    const items = pluginsData?.items ?? [];
    for (const p of items) map.set(p.id, p);
    return map;
  }, [pluginsData]);

  // ── Node/critic position measurement for SVG overlay ──
  const containerRef = useRef<HTMLDivElement | null>(null);
  const nodeElementMap = useRef(new Map<string, HTMLDivElement>());
  const criticElementMap = useRef(new Map<string, HTMLButtonElement>());
  const [nodePositions, setNodePositions] = useState(new Map<string, DOMRect>());
  const [criticPositions, setCriticPositions] = useState(new Map<string, DOMRect>());

  const measurePositions = useCallback(() => {
    const containerRect = containerRef.current?.getBoundingClientRect();
    if (!containerRect) return;

    const nextNodePos = new Map<string, DOMRect>();
    nodeElementMap.current.forEach((el, id) => {
      const rect = el.getBoundingClientRect();
      // Convert viewport-relative to container-relative coordinates
      nextNodePos.set(id, new DOMRect(
        rect.left - containerRect.left + (containerRef.current?.scrollLeft ?? 0),
        rect.top - containerRect.top + (containerRef.current?.scrollTop ?? 0),
        rect.width,
        rect.height,
      ));
    });
    setNodePositions(nextNodePos);

    const nextCriticPos = new Map<string, DOMRect>();
    criticElementMap.current.forEach((el, id) => {
      const rect = el.getBoundingClientRect();
      nextCriticPos.set(id, new DOMRect(
        rect.left - containerRect.left + (containerRef.current?.scrollLeft ?? 0),
        rect.top - containerRect.top + (containerRef.current?.scrollTop ?? 0),
        rect.width,
        rect.height,
      ));
    });
    setCriticPositions(nextCriticPos);
  }, []);

  useLayoutEffect(() => {
    measurePositions();
    const observer = new ResizeObserver(() => measurePositions());
    if (containerRef.current) observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [nodes, stages, measurePositions]);

  // ── Create callback refs for nodes and critics ──
  const nodeElementRefs = useMemo(() => {
    const map = new Map<string, (el: HTMLDivElement | null) => void>();
    for (const node of nodes) {
      map.set(node.id, (el) => {
        if (el) nodeElementMap.current.set(node.id, el);
        else nodeElementMap.current.delete(node.id);
      });
    }
    return map;
  }, [nodes]);

  const criticElementRefs = useMemo(() => {
    const map = new Map<string, (el: HTMLButtonElement | null) => void>();
    for (const node of nodes) {
      if (node.critic_id || node.critic_api_url) {
        map.set(node.id, (el) => {
          if (el) criticElementMap.current.set(node.id, el);
          else criticElementMap.current.delete(node.id);
        });
      }
    }
    return map;
  }, [nodes]);

  // ── Build detail panel data ──
  const selectedPanelData: ArchitectureDetailPanelData | null = useMemo(() => {
    if (!selectedCard) return null;
    const node = nodes.find((n) => n.id === selectedCard.nodeId);
    if (!node) return null;

    if (selectedCard.focus === "critic") {
      const criticAgent = agentLookup.get(node.critic_id ?? "") ?? null;
      return { node, agent: null, plugin: null, criticAgent, focus: "critic" };
    }

    const agent = agentLookup.get(node.worker_id ?? "") ?? null;
    const plugin = agent?.plugin_id
      ? pluginLookup.get(agent.plugin_id) ?? null
      : null;
    const criticAgent = node.critic_id
      ? agentLookup.get(node.critic_id) ?? null
      : null;

    return { node, agent, plugin, criticAgent, focus: "worker" };
  }, [selectedCard, nodes, agentLookup, pluginLookup]);

  // ── Handlers ──
  const handleCardClick = (nodeId: string, focus: "worker" | "critic") => {
    setSelectedCard({ nodeId, focus });
  };

  const handleDetailClose = () => setSelectedCard(null);
  const handleOpenInEditor = () => setViewMode("editor");

  // ── Group nodes by stage ──
  const nodesByStage = useMemo(() => {
    const map = new Map<string, typeof nodes>();
    for (const node of nodes) {
      const sid = node.stage_id ?? "__unassigned__";
      if (!map.has(sid)) map.set(sid, []);
      map.get(sid)!.push(node);
    }
    return map;
  }, [nodes]);

  // ── Loading ──
  if (isLoading) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader><Skeleton className="h-4 w-48" /></PageHeader>
        <PanoramaSkeleton />
      </div>
    );
  }

  // ── Error ──
  if (workflowError || !workflow) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader><Skeleton className="h-4 w-48" /></PageHeader>
        <div className="flex h-full items-center justify-center p-6">
          <Alert variant="destructive" className="max-w-md">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>{t(($) => $.detail.not_found)}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                {t(($) => $.detail.not_found)}
              </p>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => navigation.push(wsPaths.workflows())}>
                  <ArrowLeft className="mr-2 h-4 w-4" />
                  {t(($) => $.detail.back_to_workflows)}
                </Button>
                <Button variant="default" size="sm" onClick={() => workflowRefetch()}>
                  {t(($) => $.overview.error_retry)}
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        </div>
      </div>
    );
  }

  // ── Panorama view ──
  const sortedStages = [...stages].sort((a, b) => a.sort_order - b.sort_order);

  return (
    <div className="flex flex-col h-full">
      <PageHeader className="justify-between px-5 shrink-0">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-border/70 bg-muted/60 text-muted-foreground">
            <PanelsTopLeft className="h-4 w-4" strokeWidth={1.9} />
          </span>
          <div className="min-w-0">
            <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
              Workflow panorama
            </div>
            <h1 className="text-sm font-medium truncate">{workflow.title}</h1>
          </div>
        </div>
        {viewToggle && <div className="flex items-center gap-1">{viewToggle}</div>}
      </PageHeader>

      <div
        ref={containerRef}
        className="flex-1 overflow-auto bg-[radial-gradient(circle_at_top,_rgba(148,163,184,0.08),_transparent_38%)] p-3 relative"
      >
        <PanoramaSvgOverlay
          edges={edges}
          nodes={nodes}
          nodePositions={nodePositions}
          criticPositions={criticPositions}
        />

        {sortedStages.length === 0 ? (
          <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
            {t(($) => $.overview.stage_canvas.empty_title)}
          </div>
        ) : (
          sortedStages.map((stage, idx) => (
            <div key={stage.id}>
              <StageLane
                stage={stage}
                nodeIds={nodesByStage.get(stage.id) ?? []}
                agentLookup={agentLookup}
                pluginLookup={pluginLookup}
                onCardClick={handleCardClick}
                selectedCard={selectedCard}
                nodeElementRefs={nodeElementRefs}
                criticElementRefs={criticElementRefs}
              />
              {idx < sortedStages.length - 1 && (
                <div
                  data-testid="stage-transition-gradient"
                  className={`h-[6px] ${gradientClass}`}
                />
              )}
            </div>
          ))
        )}
      </div>

      {selectedPanelData && (
        <ArchitectureDetailPanel
          data={selectedPanelData}
          onClose={handleDetailClose}
          onOpenInEditor={handleOpenInEditor}
        />
      )}
    </div>
  );
}
```

// ── Stage transition gradient lookup (6-color cycle → all pairwise transitions) ──
const STAGE_TRANSITION_GRADIENTS = [
  "bg-gradient-to-b from-slate-50/40 to-stone-50/40",
  "bg-gradient-to-b from-stone-50/40 to-blue-50/40",
  "bg-gradient-to-b from-blue-50/40 to-rose-50/40",
  "bg-gradient-to-b from-rose-50/40 to-violet-50/40",
  "bg-gradient-to-b from-violet-50/40 to-amber-50/40",
  "bg-gradient-to-b from-amber-50/40 to-slate-50/40",
] as const;

// Inside the sortedStages.map callback, before the transition div:
const currColorIdx = Math.abs(stage.sort_order) % 6;
const nextColorIdx = Math.abs(sortedStages[idx + 1]!.sort_order) % 6;
// For same-color stages or arbitrary pairings, use the (currIdx) transition:
const gradientClass = STAGE_TRANSITION_GRADIENTS[currColorIdx] ?? STAGE_TRANSITION_GRADIENTS[0];

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/panorama-page.test.tsx
```

Expected: PASS — 所有 9 个测试通过

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/workflow-panorama-page.tsx packages/views/workflows/components/overview/panorama-page.test.tsx
git commit -m "feat(workflows): redesign panorama page as continuous flow canvas"
```

---

### Task 6: 清理旧文件并更新导出

**Files:**
- Delete: `packages/views/workflows/components/overview/data-flow-arrow.tsx`
- Delete: `packages/views/workflows/components/overview/data-flow-arrow.test.tsx`
- Modify: `packages/views/workflows/components/overview/index.ts`
- Delete: `packages/views/workflows/components/overview/stage-swimlane.tsx`
- Delete: `packages/views/workflows/components/overview/stage-swimlane.test.tsx`
- Delete: `packages/views/workflows/components/overview/plugin-card.tsx`
- Delete: `packages/views/workflows/components/overview/plugin-card.test.tsx`

- [ ] **Step 1: 更新 index.ts 导出**

```typescript
export { WorkflowOverviewPage } from "./workflow-overview-page";
export type { WorkflowOverviewPageProps } from "./workflow-overview-page";

export { WorkflowPanoramaPage } from "./workflow-panorama-page";
export type { WorkflowPanoramaPageProps } from "./workflow-panorama-page";

export { StageCanvas } from "./stage-canvas";
export type { StageCanvasProps } from "./stage-canvas";
export { StageCard } from "./stage-card";
export type { StageCardProps } from "./stage-card";
export { StageNodeDag } from "./stage-node-dag";
export type { StageNodeDagProps } from "./stage-node-dag";
export { NodeDetailPanel } from "./node-detail-panel";
export { StageCreateDialog } from "./stage-create-dialog";

// Panorama (flow canvas) components
export { StageLane } from "./stage-lane";
export type { StageLaneProps } from "./stage-lane";
export { CompactNodeCard } from "./compact-node-card";
export type { CompactNodeCardProps } from "./compact-node-card";
export { CriticBadge } from "./critic-badge";
export type { CriticBadgeProps } from "./critic-badge";
export { PanoramaSvgOverlay } from "./panorama-svg-overlay";
export type { PanoramaSvgOverlayProps, EdgePath } from "./panorama-svg-overlay";
export { ArchitectureDetailPanel } from "./architecture-detail-panel";
export type { ArchitectureDetailPanelData, ArchitectureDetailPanelProps } from "./architecture-detail-panel";
```

- [ ] **Step 2: 删除旧文件**

```bash
rm packages/views/workflows/components/overview/data-flow-arrow.tsx
rm packages/views/workflows/components/overview/data-flow-arrow.test.tsx
rm packages/views/workflows/components/overview/stage-swimlane.tsx
rm packages/views/workflows/components/overview/stage-swimlane.test.tsx
rm packages/views/workflows/components/overview/plugin-card.tsx
rm packages/views/workflows/components/overview/plugin-card.test.tsx
```

- [ ] **Step 3: 验证无残留引用**

```bash
cd F:/ai-coding/multica && grep -r "stage-swimlane\|plugin-card\|data-flow-arrow" packages/views/ --include="*.ts" --include="*.tsx" | grep -v "compact-node-card\|stage-lane\|panorama-svg-overlay"
```

Expected: 无输出（无残留引用）

- [ ] **Step 4: 运行全部 overview 测试套件**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/
```

Expected: PASS — 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add -A packages/views/workflows/components/overview/
git commit -m "chore(workflows): remove old panorama components, update exports"
```

---

### Task 7: 全量检查

- [ ] **Step 1: TypeScript 类型检查**

```bash
pnpm typecheck
```

Expected: PASS — 无类型错误

- [ ] **Step 2: 完整检查**

```bash
make check
```

Expected: PASS — 所有检查通过

- [ ] **Step 3: 如发现问题，回 Task 5/6 修复后重新运行**

- [ ] **Step 4: 最终提交**

```bash
git add -A
git commit -m "chore: finalize panorama flow canvas migration"
```

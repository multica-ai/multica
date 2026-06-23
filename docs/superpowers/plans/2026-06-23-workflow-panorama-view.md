# Workflow 研发全景图 (Panorama View) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新建一个泳道卡片风格的「研发全景图」页面，作为 `/workflows/[id]` 的默认视图。Stage = 水平泳道行，Plugin = 卡片，Agent = 关联信息在右侧滑出面板中展示。旧 overview 页面完整保留，作为备选视图。

**Architecture:** 新建 `WorkflowPanoramaPage` + 5 个子组件（`StageSwimlane`, `PluginCard`, `CriticBadge`, `DataFlowArrow`, `ArchitectureDetailPanel`）。`WorkflowViewStore` 增加 `"panorama"` 模式作为新默认值。`WorkflowDetailShell` 根据 viewMode 分发到 panorama / overview / editor。数据通过现有 TanStack Query hooks 获取（stages/nodes/edges/agents/plugins），在页面层做 client-side join——node.worker_id → agent lookup → agent.plugin_id → plugin lookup。

**Tech Stack:** React, TypeScript, TanStack Query, Zustand (view-store), Tailwind CSS, Vitest + jsdom + @testing-library/react

## Global Constraints

- TypeScript strict mode enabled; keep types explicit
- 遵循项目命名规则（参考 `packages/views/locales/zh-Hans/workflows.json`）
- 中文优先（所有 UI 文案使用 i18n key）
- 使用设计系统 tokens（`bg-background`, `text-muted-foreground` 等），禁止硬编码颜色
- `packages/views/` 内禁止 import `next/*` 或 `react-router-dom`
- 数据映射：每个 `workflow_node` 渲染为一个 plugin 卡片；`worker_id` → Agent → `plugin_id` → Plugin
- 点击卡片弹出右侧滑出面板（380px），显示 Plugin 详情 + 关联 Agent 全量信息
- **不修改** `WorkflowOverviewPage`、现有 overview 子组件、现有测试文件

---

### Task 1: 更新 WorkflowViewStore — 新增 "panorama" 视图模式

**Files:**
- Modify: `packages/core/workflows/stores/view-store.ts`（新增 panorama 模式作为默认值）
- Modify: `packages/core/workflows/stores/view-store.test.ts`（更新测试）

**Interfaces:**
- Consumes: 现有 `WorkflowViewMode` type
- Produces: `WorkflowViewMode = "panorama" | "overview" | "editor"`，默认 `"panorama"`

- [ ] **Step 1: 更新 view-store.ts**

```typescript
// packages/core/workflows/stores/view-store.ts
"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type WorkflowViewMode = "panorama" | "overview" | "editor";

interface WorkflowViewState {
  viewMode: WorkflowViewMode;
  setViewMode: (mode: WorkflowViewMode) => void;
}

/** Global singleton for the workflow detail page. */
export const useWorkflowViewStore = create<WorkflowViewState>()(
  persist(
    (set) => ({
      viewMode: "panorama" as WorkflowViewMode,
      setViewMode: (mode: WorkflowViewMode) => set({ viewMode: mode }),
    }),
    {
      name: "multica_workflows_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useWorkflowViewStore.persist.rehydrate());
```

- [ ] **Step 2: 更新 view-store.test.ts 的默认值断言**

```typescript
// 只需修改一处：默认值断言从 "overview" 改为 "panorama"
// 文件: packages/core/workflows/stores/view-store.test.ts

// 修改前: expect(useWorkflowViewStore.getState().viewMode).toBe("overview");
// 修改后: expect(useWorkflowViewStore.getState().viewMode).toBe("panorama");
```

- [ ] **Step 3: 运行测试确认通过**

```bash
pnpm --filter @multica/core exec vitest run workflows/stores/view-store.test.ts
```
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add packages/core/workflows/stores/view-store.ts packages/core/workflows/stores/view-store.test.ts
git commit -m "feat(workflows): add panorama as default view mode"
```

---

### Task 2: 新建 PluginCard 组件（TDD）

**Files:**
- Create: `packages/views/workflows/components/overview/plugin-card.tsx`
- Test: `packages/views/workflows/components/overview/plugin-card.test.tsx`

**Interfaces:**
- Consumes: `WorkflowNode` (from `@multica/core/types`), `Agent` (from `@multica/core/types`), `BuiltinPlugin` (from `@multica/core/api/schemas`)
- Produces: `PluginCard` component, `PluginCardProps` interface

```typescript
// plugin-card.tsx 接口
export interface PluginCardProps {
  node: WorkflowNode;
  agent: Agent | null;           // worker_id → agent lookup result
  plugin: BuiltinPlugin | null;  // agent.plugin_id → plugin lookup result
  onClick: (nodeId: string) => void;
}
```

- [ ] **Step 1: 编写 PluginCard 单元测试**

```typescript
// plugin-card.test.tsx
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { PluginCard } from "./plugin-card";
import type { WorkflowNode } from "@multica/core/types";
import type { Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_NODE: WorkflowNode = {
  id: "node-1",
  workflow_id: "wf-1",
  title: "需求分析",
  description: "分析产品需求文档",
  position_x: 0, position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-1",
  critic_type: "",
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
  name: "需求分析 Agent",
  description: "负责需求分析",
  instructions: "...",
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
  plugin_id: "plugin-uuid-1",
  is_builtin: false,
  owner_id: null,
  skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-uuid-1",
  name: "Cospowers Requirements",
  description: "需求分析插件",
  slug: "cospowers-requirements",
  version: "1.0.0",
  category: "engineering",
};

describe("PluginCard", () => {
  it("renders plugin name from plugin lookup", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("Cospowers Requirements")).toBeTruthy();
  });

  it("falls back to node title when plugin is null", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={null} onClick={onClick} />);
    expect(screen.getByText("需求分析")).toBeTruthy();
  });

  it("renders plugin description when available", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText("需求分析插件")).toBeTruthy();
  });

  it("shows agent status dot and model", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    expect(screen.getByText(/claude-sonnet-4-6/)).toBeTruthy();
    expect(screen.getByText("需求分析 Agent")).toBeTruthy();
  });

  it("fires onClick with node id when clicked", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={MOCK_AGENT} plugin={MOCK_PLUGIN} onClick={onClick} />);
    fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledWith("node-1");
  });

  it("renders without agent info when agent is null", () => {
    const onClick = vi.fn();
    render(<PluginCard node={MOCK_NODE} agent={null} plugin={null} onClick={onClick} />);
    expect(screen.getByText("需求分析")).toBeTruthy();
    expect(screen.getByRole("button")).toBeTruthy();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/plugin-card.test.tsx
```
Expected: FAIL — "Cannot find module './plugin-card'"

- [ ] **Step 3: 实现 PluginCard 组件**

```typescript
// plugin-card.tsx
"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";

export interface PluginCardProps {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string) => void;
}

export function PluginCard({ node, agent, plugin, onClick }: PluginCardProps) {
  const displayName = plugin?.name ?? node.title;
  const displayDesc = plugin?.description ?? node.description ?? "";

  return (
    <button
      data-testid={`plugin-card-${node.id}`}
      onClick={() => onClick(node.id)}
      className={cn(
        "flex shrink-0 flex-col gap-1.5 rounded-lg border bg-card p-3 text-left transition-colors min-w-[160px]",
        "hover:bg-accent/50 hover:border-primary/50",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      )}
    >
      <span className="text-sm font-medium truncate">{displayName}</span>

      {displayDesc && (
        <span className="text-xs text-muted-foreground line-clamp-2">{displayDesc}</span>
      )}

      {agent && (
        <div className="flex items-center gap-1.5 mt-1">
          <span
            className={cn(
              "inline-block w-1.5 h-1.5 rounded-full shrink-0",
              agent.status === "working" && "bg-green-500",
              agent.status === "idle" && "bg-blue-400",
              agent.status === "offline" && "bg-muted-foreground/40",
              agent.status === "error" && "bg-destructive",
            )}
          />
          <span className="text-xs text-muted-foreground truncate">
            {agent.name}
          </span>
          {agent.model && (
            <span className="text-[10px] text-muted-foreground/60 truncate ml-auto">
              {agent.model}
            </span>
          )}
        </div>
      )}
    </button>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/plugin-card.test.tsx
```
Expected: PASS (6 tests)

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/plugin-card.tsx packages/views/workflows/components/overview/plugin-card.test.tsx
git commit -m "feat(workflows): add PluginCard component for panorama view"
```

---

### Task 3: 新建 CriticBadge 组件（TDD）

**Files:**
- Create: `packages/views/workflows/components/overview/critic-badge.tsx`
- Test: `packages/views/workflows/components/overview/critic-badge.test.tsx`

**Interfaces:**
- Consumes: `WorkflowNode` (from `@multica/core/types`), `Agent | null`
- Produces: `CriticBadge` component, `CriticBadgeProps` interface

```typescript
export interface CriticBadgeProps {
  node: WorkflowNode;
  criticAgent: Agent | null;
  onClick: (nodeId: string) => void;
}
```

- [ ] **Step 1: 编写 CriticBadge 单元测试**

```typescript
// critic-badge.test.tsx
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { CriticBadge } from "./critic-badge";
import type { WorkflowNode, Agent } from "@multica/core/types";

const MOCK_CRITIC_NODE: WorkflowNode = {
  id: "critic-1",
  workflow_id: "wf-1",
  title: "评估器",
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
  name: "审核师",
  description: "负责代码审查",
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
    const btn = screen.getByRole("button");
    expect(btn.className).toContain("border-dashed");
  });

  it("renders critic agent name", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    expect(screen.getByText("审核师")).toBeTruthy();
  });

  it("falls back to node title when critic agent is null", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={null} onClick={onClick} />);
    expect(screen.getByText("评估器")).toBeTruthy();
  });

  it("fires onClick when clicked", () => {
    const onClick = vi.fn();
    render(<CriticBadge node={MOCK_CRITIC_NODE} criticAgent={MOCK_CRITIC_AGENT} onClick={onClick} />);
    fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledWith("critic-1");
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/critic-badge.test.tsx
```
Expected: FAIL — "Cannot find module './critic-badge'"

- [ ] **Step 3: 实现 CriticBadge 组件**

```typescript
// critic-badge.tsx
"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export interface CriticBadgeProps {
  node: WorkflowNode;
  criticAgent: Agent | null;
  onClick: (nodeId: string) => void;
}

export function CriticBadge({ node, criticAgent, onClick }: CriticBadgeProps) {
  const displayName = criticAgent?.name ?? node.title;

  return (
    <button
      data-testid={`critic-badge-${node.id}`}
      onClick={() => onClick(node.id)}
      className={cn(
        "flex shrink-0 flex-col gap-1 rounded-md border-2 border-dashed border-amber-300 bg-amber-50/50 dark:bg-amber-950/20 p-2 text-left transition-colors min-w-[140px]",
        "hover:bg-amber-100/50 hover:border-amber-400",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      )}
    >
      <span className="text-[10px] text-muted-foreground uppercase tracking-wider">
        Critic
      </span>
      <span className="text-xs font-medium truncate">{displayName}</span>
      {criticAgent?.model && (
        <span className="text-[10px] text-muted-foreground/60 truncate">
          {criticAgent.model}
        </span>
      )}
    </button>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/critic-badge.test.tsx
```
Expected: PASS (4 tests)

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/critic-badge.tsx packages/views/workflows/components/overview/critic-badge.test.tsx
git commit -m "feat(workflows): add CriticBadge component for panorama view"
```

---

### Task 4: 新建 ArchitectureDetailPanel 组件（TDD）

**Files:**
- Create: `packages/views/workflows/components/overview/architecture-detail-panel.tsx`
- Test: `packages/views/workflows/components/overview/architecture-detail-panel.test.tsx`

**Interfaces:**
- Consumes: `WorkflowNode`, `Agent | null`, `BuiltinPlugin | null`
- Produces: `ArchitectureDetailPanel` component, `ArchitectureDetailPanelData` interface

```typescript
export interface ArchitectureDetailPanelData {
  node: WorkflowNode;
  agent: Agent | null;           // worker's agent (via worker_id)
  plugin: BuiltinPlugin | null;  // agent's plugin (via plugin_id)
  criticAgent: Agent | null;     // critic's agent (via critic_id or worker_id for critic nodes)
}

export interface ArchitectureDetailPanelProps {
  data: ArchitectureDetailPanelData;
  onClose: () => void;
  onOpenInEditor: () => void;
}
```

- [ ] **Step 1: 编写 ArchitectureDetailPanel 单元测试**

```typescript
// architecture-detail-panel.test.tsx
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { ArchitectureDetailPanel } from "./architecture-detail-panel";
import type { ArchitectureDetailPanelData } from "./architecture-detail-panel";
import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

// ── Hoisted mocks ──
const mocks = vi.hoisted(() => ({
  setViewMode: vi.fn(),
}));

vi.mock("@multica/core/workflows/stores/view-store", () => ({
  useWorkflowViewStore: (selector: (s: unknown) => unknown) =>
    selector({ viewMode: "panorama", setViewMode: mocks.setViewMode }),
}));

vi.mock("../../../i18n", () => ({
  useT: () => ((key: unknown) => {
    if (typeof key === "function") {
      const result = key({
        detail_panel: {
          title: "Node Details",
          plugin_info: "Plugin Info",
          agent_info: "Associated Agent",
          open_in_editor: "Open in Editor",
        }
      });
      if (result?.detail_panel?.title) return result.detail_panel.title;
      if (result?.detail_panel?.plugin_info) return result.detail_panel.plugin_info;
      if (result?.detail_panel?.agent_info) return result.detail_panel.agent_info;
      if (result?.detail_panel?.open_in_editor) return result.detail_panel.open_in_editor;
    }
    return String(key);
  }),
}));

const MOCK_NODE: WorkflowNode = {
  id: "node-1", workflow_id: "wf-1", title: "需求分析",
  description: "", position_x: 0, position_y: 0,
  format_schema: null, worker_type: "agent", worker_id: "agent-1",
  critic_type: "", critic_id: null, critic_api_url: null,
  sort_order: 0, stage_id: "stage-1",
  created_at: "", updated_at: "",
};

const MOCK_AGENT: Agent = {
  id: "agent-1", workspace_id: "ws-1", runtime_id: "rt-1",
  name: "需求分析 Agent", description: "负责需求分析",
  instructions: "请分析需求文档", avatar_url: null,
  runtime_mode: "cloud", runtime_config: {},
  custom_env: { "NODE_ENV": "production" }, custom_args: ["--verbose"],
  custom_env_redacted: false,
  visibility: "workspace", status: "idle",
  max_concurrent_tasks: 1, model: "claude-sonnet-4-6",
  thinking_level: "medium", plugin_id: "plugin-uuid-1",
  is_builtin: false, owner_id: null, skills: [
    { id: "s1", name: "brainstorming", description: "Brainstorming skill" },
    { id: "s2", name: "session-context", description: "Session context" },
  ],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-uuid-1", name: "Cospowers Requirements",
  description: "需求分析插件", slug: "cospowers-requirements",
  version: "1.0.0", category: "engineering",
};

const MOCK_DATA: ArchitectureDetailPanelData = {
  node: MOCK_NODE,
  agent: MOCK_AGENT,
  plugin: MOCK_PLUGIN,
  criticAgent: null,
};

describe("ArchitectureDetailPanel", () => {
  it("renders plugin section with name and slug", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(<ArchitectureDetailPanel data={MOCK_DATA} onClose={onClose} onOpenInEditor={onOpenInEditor} />);
    expect(screen.getByText("Cospowers Requirements")).toBeTruthy();
    expect(screen.getByText("cospowers-requirements")).toBeTruthy();
  });

  it("renders agent section with full info", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(<ArchitectureDetailPanel data={MOCK_DATA} onClose={onClose} onOpenInEditor={onOpenInEditor} />);
    expect(screen.getByText("需求分析 Agent")).toBeTruthy();
    expect(screen.getByText(/claude-sonnet-4-6/)).toBeTruthy();
    expect(screen.getByText(/cloud/)).toBeTruthy();
  });

  it("shows skills count", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(<ArchitectureDetailPanel data={MOCK_DATA} onClose={onClose} onOpenInEditor={onOpenInEditor} />);
    expect(screen.getByText(/2/)).toBeTruthy();
  });

  it("calls onClose when close button clicked", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(<ArchitectureDetailPanel data={MOCK_DATA} onClose={onClose} onOpenInEditor={onOpenInEditor} />);
    fireEvent.click(screen.getByText("×"));
    expect(onClose).toHaveBeenCalled();
  });

  it("calls onOpenInEditor when editor button clicked", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(<ArchitectureDetailPanel data={MOCK_DATA} onClose={onClose} onOpenInEditor={onOpenInEditor} />);
    fireEvent.click(screen.getByText(/Open in Editor/));
    expect(onOpenInEditor).toHaveBeenCalled();
  });

  it("handles null agent and plugin gracefully", () => {
    const onClose = vi.fn();
    const onOpenInEditor = vi.fn();
    render(
      <ArchitectureDetailPanel
        data={{ ...MOCK_DATA, agent: null, plugin: null }}
        onClose={onClose}
        onOpenInEditor={onOpenInEditor}
      />
    );
    expect(screen.getByText("需求分析")).toBeTruthy(); // node title as fallback
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/architecture-detail-panel.test.tsx
```
Expected: FAIL — "Cannot find module './architecture-detail-panel'"

- [ ] **Step 3: 实现 ArchitectureDetailPanel 组件**

```typescript
// architecture-detail-panel.tsx
"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { useT } from "../../../i18n";

export interface ArchitectureDetailPanelData {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  criticAgent: Agent | null;
}

export interface ArchitectureDetailPanelProps {
  data: ArchitectureDetailPanelData;
  onClose: () => void;
  onOpenInEditor: () => void;
}

export function ArchitectureDetailPanel({
  data,
  onClose,
  onOpenInEditor,
}: ArchitectureDetailPanelProps) {
  const { t } = useT("workflows");
  const { node, agent, plugin, criticAgent } = data;

  const displayEntity = criticAgent ?? agent;
  const displayName = plugin?.name ?? displayEntity?.name ?? node.title;
  const displayDesc = plugin?.description ?? displayEntity?.description ?? node.description;

  return (
    <div
      className="fixed right-0 top-0 bottom-0 w-[380px] bg-background border-l shadow-lg z-50 overflow-y-auto"
      data-testid="architecture-detail-panel"
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b sticky top-0 bg-background">
        <h2 className="font-semibold text-sm">
          {t(($) => $.overview.detail_panel.title)}
        </h2>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground text-lg leading-none"
          data-testid="detail-panel-close"
        >
          ×
        </button>
      </div>

      <div className="p-4 space-y-5">
        {/* ── Plugin / Entity info ── */}
        <section>
          <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
            {criticAgent ? "Critic" : t(($) => $.overview.detail_panel.plugin_info)}
          </h3>
          <h4 className="font-medium text-sm">{displayName}</h4>
          {plugin?.slug && (
            <p className="text-xs text-muted-foreground mt-0.5">{plugin.slug}</p>
          )}
          {displayDesc && (
            <p className="text-xs text-muted-foreground mt-1">{displayDesc}</p>
          )}
          {plugin?.version && (
            <p className="text-xs text-muted-foreground mt-1">v{plugin.version}</p>
          )}
          {plugin?.category && (
            <p className="text-xs text-muted-foreground mt-0.5">{plugin.category}</p>
          )}
        </section>

        {/* ── Agent info ── */}
        {displayEntity && (
          <section>
            <h3 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
              {t(($) => $.overview.detail_panel.agent_info)}
            </h3>
            <AgentInfoBlock agent={displayEntity} />
          </section>
        )}
      </div>

      {/* Footer */}
      <div className="sticky bottom-0 bg-background border-t p-3">
        <button
          onClick={onOpenInEditor}
          className="w-full py-2 text-sm bg-primary text-primary-foreground rounded-md hover:opacity-90"
        >
          {t(($) => $.overview.detail_panel.open_in_editor)}
        </button>
      </div>
    </div>
  );
}

/** Renders the full agent info block. */
function AgentInfoBlock({ agent }: { agent: Agent }) {
  const fields: [string, string | number | boolean | null | undefined][] = [
    ["Name", agent.name],
    ["Description", agent.description],
    ["Runtime mode", agent.runtime_mode],
    ["Status", agent.status],
    ["Model", agent.model],
    ["Thinking level", agent.thinking_level || "—"],
    ["Visibility", agent.visibility],
    ["Max concurrent", agent.max_concurrent_tasks],
    ["Built-in", agent.is_builtin ? "Yes" : "No"],
    ["Instructions", agent.instructions],
    ["Custom env keys", Object.keys(agent.custom_env).join(", ") || "—"],
    ["Custom args", agent.custom_args.join(", ") || "—"],
    ["Skills", `${agent.skills.length}`],
  ];

  return (
    <dl className="space-y-1.5 text-xs">
      {fields.map(([label, value]) => {
        const displayValue = value === "" || value === null || value === undefined ? "—" : String(value);
        return (
          <div key={label} className="flex gap-2">
            <dt className="text-muted-foreground shrink-0 w-28">{label}</dt>
            <dd className="text-foreground truncate">{displayValue}</dd>
          </div>
        );
      })}
    </dl>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/architecture-detail-panel.test.tsx
```
Expected: PASS (6 tests)

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/architecture-detail-panel.tsx packages/views/workflows/components/overview/architecture-detail-panel.test.tsx
git commit -m "feat(workflows): add ArchitectureDetailPanel for panorama view"
```

---

### Task 5: 新建 DataFlowArrow 组件（TDD）

**Files:**
- Create: `packages/views/workflows/components/overview/data-flow-arrow.tsx`
- Test: `packages/views/workflows/components/overview/data-flow-arrow.test.tsx`

**Interfaces:**
- Consumes: `WorkflowEdge`, `WorkflowNode`, `WorkflowStage`
- Produces: `DataFlowArrow` component

```typescript
export interface DataFlowArrowProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  stages: WorkflowStage[];
}
```

- [ ] **Step 1: 编写 DataFlowArrow 单元测试**

```typescript
// data-flow-arrow.test.tsx
// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { DataFlowArrow } from "./data-flow-arrow";
import type { WorkflowEdge, WorkflowNode, WorkflowStage } from "@multica/core/types";

const MOCK_STAGES: WorkflowStage[] = [
  { id: "stage-1", workflow_id: "wf-1", name: "需求", description: "", sort_order: 0, node_count: 2, created_at: "", updated_at: "" },
  { id: "stage-2", workflow_id: "wf-1", name: "设计", description: "", sort_order: 1, node_count: 2, created_at: "", updated_at: "" },
];

const MOCK_NODES: WorkflowNode[] = [
  { id: "n1", workflow_id: "wf-1", title: "Node 1", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "a1", critic_type: "", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n2", workflow_id: "wf-1", title: "Node 2", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "a2", critic_type: "", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-2", created_at: "", updated_at: "" },
];

describe("DataFlowArrow", () => {
  it("renders nothing when no cross-stage edges exist", () => {
    const sameStageEdges: WorkflowEdge[] = [
      { id: "e1", workflow_id: "wf-1", source_node_id: "n1", target_node_id: "n1", condition: null, created_at: "" },
    ];
    const { container } = render(
      <DataFlowArrow edges={sameStageEdges} nodes={MOCK_NODES} stages={MOCK_STAGES} />
    );
    expect(container.querySelector('[data-testid="data-flow-arrow"]')).toBeNull();
  });

  it("renders arrow for cross-stage edges", () => {
    const crossStageEdges: WorkflowEdge[] = [
      { id: "e1", workflow_id: "wf-1", source_node_id: "n1", target_node_id: "n2", condition: null, created_at: "" },
    ];
    render(
      <DataFlowArrow edges={crossStageEdges} nodes={MOCK_NODES} stages={MOCK_STAGES} />
    );
    expect(screen.getByTestId("data-flow-arrow")).toBeTruthy();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/data-flow-arrow.test.tsx
```
Expected: FAIL

- [ ] **Step 3: 实现 DataFlowArrow 组件**

```typescript
// data-flow-arrow.tsx
"use client";

import { useMemo } from "react";
import type { WorkflowEdge, WorkflowNode, WorkflowStage } from "@multica/core/types";

export interface DataFlowArrowProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  stages: WorkflowStage[];
}

/**
 * Renders cross-stage data-flow arrows between swimlanes.
 * Only shows when source and target nodes belong to different stages.
 */
export function DataFlowArrow({ edges, nodes }: DataFlowArrowProps) {
  const nodeMap = useMemo(
    () => new Map(nodes.map((n) => [n.id, n])),
    [nodes],
  );

  const crossStageEdges = useMemo(
    () =>
      edges.filter((e) => {
        const src = nodeMap.get(e.source_node_id);
        const tgt = nodeMap.get(e.target_node_id);
        if (!src || !tgt) return false;
        return src.stage_id !== tgt.stage_id;
      }),
    [edges, nodeMap],
  );

  if (crossStageEdges.length === 0) return null;

  return (
    <div
      data-testid="data-flow-arrow"
      className="flex items-center justify-center py-2"
    >
      <div className="flex items-center gap-1 text-muted-foreground">
        <svg
          width="24"
          height="24"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="rotate-90"
        >
          <line x1="12" y1="5" x2="12" y2="19" />
          <polyline points="19 12 12 19 5 12" />
        </svg>
        <span className="text-xs">↓</span>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/data-flow-arrow.test.tsx
```
Expected: PASS (2 tests)

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/data-flow-arrow.tsx packages/views/workflows/components/overview/data-flow-arrow.test.tsx
git commit -m "feat(workflows): add DataFlowArrow component for panorama view"
```

---

### Task 6: 新建 StageSwimlane 组件（TDD）

**Files:**
- Create: `packages/views/workflows/components/overview/stage-swimlane.tsx`
- Test: `packages/views/workflows/components/overview/stage-swimlane.test.tsx`

**Interfaces:**
- Consumes: `WorkflowStage`, `WorkflowNode[]`, `Map<string, Agent | null>`, `Map<string, BuiltinPlugin | null>`
- Produces: `StageSwimlane` component, `StageSwimlaneProps` interface

```typescript
export interface StageSwimlaneProps {
  stage: WorkflowStage;
  nodes: WorkflowNode[];
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string) => void;
}
```

- [ ] **Step 1: 编写 StageSwimlane 单元测试**

```typescript
// stage-swimlane.test.tsx
// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { StageSwimlane } from "./stage-swimlane";
import type { WorkflowStage, WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_STAGE: WorkflowStage = {
  id: "stage-1", workflow_id: "wf-1", name: "需求接入",
  description: "", sort_order: 0, node_count: 2,
  created_at: "", updated_at: "",
};

const MOCK_NODES: WorkflowNode[] = [
  { id: "n1", workflow_id: "wf-1", title: "brainstorming", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "agent-1", critic_type: "", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n2", workflow_id: "wf-1", title: "aireq-evaluator", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "agent-critic", critic_type: "agent", critic_id: "agent-critic-2", critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
];

const MOCK_AGENT: Agent = {
  id: "agent-1", workspace_id: "ws-1", runtime_id: "rt-1",
  name: "需求分析 Agent", description: "分析需求",
  instructions: "", avatar_url: null,
  runtime_mode: "cloud", runtime_config: {},
  custom_env: {}, custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace", status: "idle",
  max_concurrent_tasks: 1, model: "claude-sonnet-4-6",
  thinking_level: "", plugin_id: "plugin-1",
  is_builtin: false, owner_id: null, skills: [],
  created_at: "", updated_at: "",
  archived_at: null, archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-1", name: "My Plugin",
  description: "A test plugin", slug: "my-plugin",
  version: "1.0.0", category: "engineering",
};

describe("StageSwimlane", () => {
  const agentLookup = new Map([["agent-1", MOCK_AGENT], ["agent-critic", null]]);
  const pluginLookup = new Map([["plugin-1", MOCK_PLUGIN]]);

  it("renders stage name as header", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane stage={MOCK_STAGE} nodes={MOCK_NODES}
        agentLookup={agentLookup} pluginLookup={pluginLookup}
        onCardClick={onCardClick} />
    );
    expect(screen.getByText("需求接入")).toBeTruthy();
  });

  it("renders PluginCard for worker nodes and CriticBadge for critic nodes", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane stage={MOCK_STAGE} nodes={MOCK_NODES}
        agentLookup={agentLookup} pluginLookup={pluginLookup}
        onCardClick={onCardClick} />
    );
    // n1 = worker node → PluginCard
    expect(screen.getByTestId("plugin-card-n1")).toBeTruthy();
    // n2 = critic_type non-empty → CriticBadge
    expect(screen.getByTestId("critic-badge-n2")).toBeTruthy();
  });

  it("fires onCardClick when a card is clicked", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane stage={MOCK_STAGE} nodes={MOCK_NODES}
        agentLookup={agentLookup} pluginLookup={pluginLookup}
        onCardClick={onCardClick} />
    );
    fireEvent.click(screen.getByTestId("plugin-card-n1"));
    expect(onCardClick).toHaveBeenCalledWith("n1");
  });

  it("shows empty state when no nodes for stage", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane stage={MOCK_STAGE} nodes={[]}
        agentLookup={agentLookup} pluginLookup={pluginLookup}
        onCardClick={onCardClick} />
    );
    expect(screen.getByTestId("stage-swimlane-empty")).toBeTruthy();
  });
});
```

- [ ] **Step 2: 运行测试确认失败**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/stage-swimlane.test.tsx
```
Expected: FAIL

- [ ] **Step 3: 实现 StageSwimlane 组件**

```typescript
// stage-swimlane.tsx
"use client";

import { useMemo } from "react";
import type { WorkflowStage, WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { PluginCard } from "./plugin-card";
import { CriticBadge } from "./critic-badge";

export interface StageSwimlaneProps {
  stage: WorkflowStage;
  nodes: WorkflowNode[];
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string) => void;
}

export function StageSwimlane({
  stage,
  nodes,
  agentLookup,
  pluginLookup,
  onCardClick,
}: StageSwimlaneProps) {
  const stageNodes = useMemo(
    () => nodes.filter((n) => n.stage_id === stage.id),
    [nodes, stage.id],
  );

  // Separate worker nodes from critic nodes (critic_type non-empty → CriticBadge)
  const { workerNodes, criticNodes } = useMemo(() => {
    const workers: WorkflowNode[] = [];
    const critics: WorkflowNode[] = [];
    for (const n of stageNodes) {
      if (n.critic_type) {
        critics.push(n);
      } else {
        workers.push(n);
      }
    }
    return { workerNodes: workers, criticNodes: critics };
  }, [stageNodes]);

  return (
    <div data-testid="stage-swimlane" className="rounded-lg border bg-card/30 overflow-hidden">
      {/* Stage header */}
      <div className="px-4 py-2 border-b bg-muted/30">
        <h3 className="text-sm font-semibold text-center">{stage.name}</h3>
        {stage.description && (
          <p className="text-xs text-muted-foreground text-center mt-0.5">
            {stage.description}
          </p>
        )}
      </div>

      {/* Cards area */}
      <div className="p-3">
        {stageNodes.length === 0 ? (
          <div
            data-testid="stage-swimlane-empty"
            className="flex items-center justify-center h-16 text-xs text-muted-foreground"
          >
            No nodes in this stage
          </div>
        ) : (
          <div className="flex flex-wrap gap-2">
            {workerNodes.map((node) => {
              const agent = agentLookup.get(node.worker_id ?? "") ?? null;
              const plugin = agent?.plugin_id
                ? pluginLookup.get(agent.plugin_id) ?? null
                : null;
              return (
                <PluginCard
                  key={node.id}
                  node={node}
                  agent={agent}
                  plugin={plugin}
                  onClick={onCardClick}
                />
              );
            })}

            {criticNodes.map((node) => {
              // For critic nodes, the worker_id is the agent that performs critique
              const criticAgent = agentLookup.get(node.worker_id ?? "") ?? null;
              return (
                <CriticBadge
                  key={node.id}
                  node={node}
                  criticAgent={criticAgent}
                  onClick={onCardClick}
                />
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/stage-swimlane.test.tsx
```
Expected: PASS (4 tests)

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/stage-swimlane.tsx packages/views/workflows/components/overview/stage-swimlane.test.tsx
git commit -m "feat(workflows): add StageSwimlane component for panorama view"
```

---

### Task 7: 新建 WorkflowPanoramaPage 页面（TDD）

**Files:**
- Create: `packages/views/workflows/components/overview/workflow-panorama-page.tsx`
- Test: `packages/views/workflows/components/overview/panorama-page.test.tsx`

**Interfaces:**
- Consumes: `WorkflowOverviewPageProps` (兼容接口：`workflowId: string; viewToggle?: ReactNode`), `workflowOverviewOptions`, `workflowStagesOptions`, `workflowNodesOptions`, `workflowEdgesOptions`, `agentListOptions`, `builtinPluginListOptions`
- Produces: `WorkflowPanoramaPage` component

- [ ] **Step 1: 编写 panorama-page.test.tsx**

```typescript
// panorama-page.test.tsx
// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, cleanup, screen } from "@testing-library/react";
import { renderWithI18n } from "../../../test/i18n";

// ── Mock data ──
const MOCK_WORKFLOW = { id: "wf-1", title: "Test Workflow" };

const MOCK_STAGES = [
  { id: "stage-1", workflow_id: "wf-1", name: "需求接入", description: "", sort_order: 0, node_count: 2, created_at: "", updated_at: "" },
  { id: "stage-2", workflow_id: "wf-1", name: "需求分析", description: "", sort_order: 1, node_count: 1, created_at: "", updated_at: "" },
];

const MOCK_NODES = [
  { id: "n1", workflow_id: "wf-1", title: "brainstorming", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "agent-1", critic_type: "", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n2", workflow_id: "wf-1", title: "session-context", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "agent-2", critic_type: "", critic_id: null, critic_api_url: null, sort_order: 1, stage_id: "stage-1", created_at: "", updated_at: "" },
  { id: "n3", workflow_id: "wf-1", title: "requirement-analysis", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "agent-3", critic_type: "", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-2", created_at: "", updated_at: "" },
];

const MOCK_EDGES = [
  { id: "e1", workflow_id: "wf-1", source_node_id: "n1", target_node_id: "n2", condition: null, created_at: "" },
  { id: "e2", workflow_id: "wf-1", source_node_id: "n2", target_node_id: "n3", condition: null, created_at: "" },
];

const MOCK_AGENTS = [
  { id: "agent-1", workspace_id: "ws-1", runtime_id: "rt-1", name: "Brainstorming Agent", description: "Brainstorms", instructions: "", avatar_url: null, runtime_mode: "cloud", runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace", status: "idle", max_concurrent_tasks: 1, model: "claude-sonnet-4-6", thinking_level: "medium", plugin_id: "plugin-1", is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
  { id: "agent-2", workspace_id: "ws-1", runtime_id: "rt-1", name: "Session Agent", description: "Session context", instructions: "", avatar_url: null, runtime_mode: "cloud", runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace", status: "working", max_concurrent_tasks: 1, model: "claude-opus-4-8", thinking_level: "", plugin_id: "plugin-2", is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
  { id: "agent-3", workspace_id: "ws-1", runtime_id: "rt-1", name: "Req Analysis Agent", description: "Requirements analysis", instructions: "", avatar_url: null, runtime_mode: "local", runtime_config: {}, custom_env: {}, custom_args: [], custom_env_redacted: false, visibility: "workspace", status: "idle", max_concurrent_tasks: 2, model: "claude-haiku-4-5-20251001", thinking_level: "", plugin_id: null, is_builtin: false, owner_id: null, skills: [], created_at: "", updated_at: "", archived_at: null, archived_by: null },
];

const MOCK_PLUGINS = {
  items: [
    { id: "plugin-1", name: "Cospowers Brainstorming", description: "Brainstorming plugin", slug: "cospowers-brainstorming", version: "1.0.0", category: "engineering" },
    { id: "plugin-2", name: "Cospowers Session", description: "Session context plugin", slug: "cospowers-session", version: "1.0.0", category: "engineering" },
  ],
  total: 2, page: 1, pageSize: 100, hasMore: false,
};

// ── Hoisted mocks ──
const mocks = vi.hoisted(() => ({
  workflowData: undefined as unknown,
  stagesData: undefined as unknown[],
  nodesData: undefined as unknown[],
  edgesData: undefined as unknown[],
  agentsData: undefined as unknown[],
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
    return { data: mocks.workflowData, isLoading: mocks.isLoading, isError: mocks.isError };
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

  it("renders stage swimlanes for each stage", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.getByText("需求接入")).toBeTruthy();
    expect(screen.getByText("需求分析")).toBeTruthy();
  });

  it("renders plugin cards with resolved names", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    expect(screen.getByText("Cospowers Brainstorming")).toBeTruthy();
    expect(screen.getByText("Cospowers Session")).toBeTruthy();
    // n3 agent has no plugin_id → falls back to node title
    expect(screen.getByText("requirement-analysis")).toBeTruthy();
  });

  it("opens detail panel on card click", () => {
    renderWithI18n(<WorkflowPanoramaPage workflowId="wf-1" />);
    fireEvent.click(screen.getByTestId("plugin-card-n1"));
    expect(screen.getByTestId("architecture-detail-panel")).toBeTruthy();
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
pnpm --filter @multica/views exec vitest run workflows/overview/panorama-page.test.tsx
```
Expected: FAIL — "Cannot find module './workflow-panorama-page'"

- [ ] **Step 3: 实现 WorkflowPanoramaPage**

```typescript
// workflow-panorama-page.tsx
"use client";

import { useState, useMemo, type ReactNode } from "react";
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
import { StageSwimlane } from "./stage-swimlane";
import { DataFlowArrow } from "./data-flow-arrow";
import {
  ArchitectureDetailPanel,
  type ArchitectureDetailPanelData,
} from "./architecture-detail-panel";
import type { Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

export interface WorkflowPanoramaPageProps {
  workflowId: string;
  viewToggle?: ReactNode;
}

/** Loading skeleton for panorama view. */
function PanoramaSkeleton() {
  return (
    <div className="flex flex-col gap-4 p-6" data-testid="panorama-skeleton">
      <Skeleton className="h-8 w-64" />
      {Array.from({ length: 3 }).map((_, i) => (
        <Skeleton key={i} className="h-32 w-full rounded-lg" />
      ))}
    </div>
  );
}

export function WorkflowPanoramaPage({ workflowId, viewToggle }: WorkflowPanoramaPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();
  const setViewMode = useWorkflowViewStore((s) => s.setViewMode);

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

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
    for (const a of agents) {
      map.set(a.id, a);
    }
    return map;
  }, [agents]);

  const pluginLookup = useMemo(() => {
    const map = new Map<string, BuiltinPlugin | null>();
    const items = pluginsData?.items ?? [];
    for (const p of items) {
      map.set(p.id, p);
    }
    return map;
  }, [pluginsData]);

  // Build detail panel data for selected node
  const selectedPanelData: ArchitectureDetailPanelData | null = useMemo(() => {
    if (!selectedNodeId) return null;
    const node = nodes.find((n) => n.id === selectedNodeId);
    if (!node) return null;

    const isCriticNode = !!node.critic_type;

    if (isCriticNode) {
      // Critic node: worker_id is the critic's agent
      const criticAgent = agentLookup.get(node.worker_id ?? "") ?? null;
      return { node, agent: null, plugin: null, criticAgent };
    }

    const agent = agentLookup.get(node.worker_id ?? "") ?? null;
    const plugin = agent?.plugin_id
      ? pluginLookup.get(agent.plugin_id) ?? null
      : null;
    const criticAgent = node.critic_id
      ? agentLookup.get(node.critic_id) ?? null
      : null;

    return { node, agent, plugin, criticAgent };
  }, [selectedNodeId, nodes, agentLookup, pluginLookup]);

  // ── Handlers ──
  const handleCardClick = (nodeId: string) => {
    setSelectedNodeId(nodeId);
  };

  const handleDetailClose = () => {
    setSelectedNodeId(null);
  };

  const handleOpenInEditor = () => {
    setViewMode("editor");
  };

  // ── Loading ──
  if (isLoading) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader>
          <Skeleton className="h-4 w-48" />
        </PageHeader>
        <PanoramaSkeleton />
      </div>
    );
  }

  // ── Error ──
  if (workflowError || !workflow) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader>
          <Skeleton className="h-4 w-48" />
        </PageHeader>
        <div className="flex h-full items-center justify-center p-6">
          <Alert variant="destructive" className="max-w-md">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>{t(($) => $.detail.not_found)}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                {t(($) => $.detail.not_found)}
              </p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => navigation.push(wsPaths.workflows())}
                >
                  <ArrowLeft className="mr-2 h-4 w-4" />
                  {t(($) => $.detail.back_to_workflows)}
                </Button>
                <Button
                  variant="default"
                  size="sm"
                  onClick={() => workflowRefetch()}
                >
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
        <div className="flex items-center gap-2 min-w-0">
          <h1 className="text-sm font-medium truncate">{workflow.title}</h1>
        </div>
        {viewToggle && <div className="flex items-center gap-1">{viewToggle}</div>}
      </PageHeader>

      <div className="flex-1 overflow-auto p-6 flex flex-col gap-4">
        {sortedStages.map((stage, idx) => (
          <div key={stage.id}>
            <StageSwimlane
              stage={stage}
              nodes={nodes}
              agentLookup={agentLookup}
              pluginLookup={pluginLookup}
              onCardClick={handleCardClick}
            />
            {idx < sortedStages.length - 1 && (
              <DataFlowArrow edges={edges} nodes={nodes} stages={stages} />
            )}
          </div>
        ))}

        {sortedStages.length === 0 && (
          <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
            {t(($) => $.overview.stage_canvas.empty_title)}
          </div>
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

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/panorama-page.test.tsx
```
Expected: PASS (6 tests)

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/overview/workflow-panorama-page.tsx packages/views/workflows/components/overview/panorama-page.test.tsx
git commit -m "feat(workflows): add WorkflowPanoramaPage for swimlane panorama view"
```

---

### Task 8: 更新 WorkflowDetailShell 和导出

**Files:**
- Modify: `packages/views/workflows/components/workflow-detail-shell.tsx`（新增 panorama 视图分发 + 视图切换下拉菜单）
- Modify: `packages/views/workflows/components/overview/index.ts`（新增导出）

**Interfaces:**
- Consumes: `WorkflowPanoramaPage`, `useWorkflowViewStore`
- Produces: 更新后的 view-toggle 下拉菜单（panorama / overview / editor）

- [ ] **Step 1: 更新 WorkflowDetailShell**

```typescript
// workflow-detail-shell.tsx
"use client";

import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { WorkflowDetailPage } from "./workflow-detail-page";
import { WorkflowOverviewPage } from "./overview";
import { WorkflowPanoramaPage } from "./overview/workflow-panorama-page";

import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Layers, Pen, GitFork } from "lucide-react";

export interface WorkflowDetailShellProps {
  workflowId: string;
}

/** Renders the panorama (default), overview, or editor view with a shared view-toggle dropdown. */
export function WorkflowDetailShell({ workflowId }: WorkflowDetailShellProps) {
  const { t } = useT("workflows");
  const viewMode = useWorkflowViewStore((s) => s.viewMode);
  const setViewMode = useWorkflowViewStore((s) => s.setViewMode);

  const viewToggle = (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="outline" size="icon-sm" className="text-muted-foreground" title={t(($) => $.view.section)}>
            {viewMode === "panorama" ? <GitFork className="size-4" /> :
             viewMode === "overview" ? <Layers className="size-4" /> :
             <Pen className="size-4" />}
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.view.section)}</DropdownMenuLabel>
          <DropdownMenuItem onClick={() => setViewMode("panorama")}>
            <GitFork className="size-4 mr-2" />
            {"全景图"}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setViewMode("overview")}>
            <Layers className="size-4 mr-2" />
            {t(($) => $.view.overview)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setViewMode("editor")}>
            <Pen className="size-4 mr-2" />
            {t(($) => $.view.editor)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  if (viewMode === "editor") {
    return <WorkflowDetailPage workflowId={workflowId} viewToggle={viewToggle} />;
  }

  if (viewMode === "overview") {
    return <WorkflowOverviewPage workflowId={workflowId} viewToggle={viewToggle} />;
  }

  return <WorkflowPanoramaPage workflowId={workflowId} viewToggle={viewToggle} />;
}
```

- [ ] **Step 2: 更新 overview/index.ts 导出**

在现有 index.ts 末尾追加新组件导出：

```typescript
// 追加到文件末尾（保留所有现有导出）
export { WorkflowPanoramaPage } from "./workflow-panorama-page";
export type { WorkflowPanoramaPageProps } from "./workflow-panorama-page";
export { PluginCard } from "./plugin-card";
export type { PluginCardProps } from "./plugin-card";
export { CriticBadge } from "./critic-badge";
export type { CriticBadgeProps } from "./critic-badge";
export { StageSwimlane } from "./stage-swimlane";
export type { StageSwimlaneProps } from "./stage-swimlane";
export { DataFlowArrow } from "./data-flow-arrow";
export type { DataFlowArrowProps } from "./data-flow-arrow";
export { ArchitectureDetailPanel } from "./architecture-detail-panel";
export type { ArchitectureDetailPanelData, ArchitectureDetailPanelProps } from "./architecture-detail-panel";
```

- [ ] **Step 3: 运行现有测试确保无回归**

```bash
pnpm --filter @multica/views exec vitest run workflows/overview/overview-page.test.tsx
```
Expected: PASS — 旧 overview 测试全部通过（未被修改）

- [ ] **Step 4: 运行类型检查**

```bash
pnpm typecheck
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add packages/views/workflows/components/workflow-detail-shell.tsx \
        packages/views/workflows/components/overview/index.ts
git commit -m "feat(workflows): wire panorama page as default view in detail shell"
```

---

### Task 9: i18n 翻译 & 最终验证

- [ ] **Step 1: 添加中文翻译**

在 `packages/views/locales/zh-Hans/workflows.json` 的 `view` 块新增：

```json
"panorama": "全景图"
```

在 `overview.detail_panel` 块补充字段：

```json
"plugin_info": "Plugin 信息",
"agent_info": "关联 Agent"
```

- [ ] **Step 2: 添加英文翻译**

在 `packages/views/locales/en/workflows.json` 同步添加。

- [ ] **Step 3: 全量类型检查**

```bash
pnpm typecheck
```
Expected: PASS

- [ ] **Step 4: 全量 TS 单元测试**

```bash
pnpm test
```
Expected: ALL TESTS PASS

- [ ] **Step 5: Go 测试**

```bash
make test
```
Expected: PASS（后端无变更）

- [ ] **Step 6: 最终提交**

```bash
git add -A
git commit -m "chore(workflows): finalize panorama view with i18n"
```

---

## 文件变更总结

| 文件 | 操作 | 说明 |
|---|---|---|
| `packages/core/workflows/stores/view-store.ts` | **修改** | 新增 `"panorama"` mode，设为首选默认值 |
| `packages/core/workflows/stores/view-store.test.ts` | **修改** | 更新默认值断言 |
| `packages/views/workflows/components/overview/plugin-card.tsx` | **新建** | Plugin 卡片组件 |
| `packages/views/workflows/components/overview/plugin-card.test.tsx` | **新建** | 测试 |
| `packages/views/workflows/components/overview/critic-badge.tsx` | **新建** | 评估器虚线卡片 |
| `packages/views/workflows/components/overview/critic-badge.test.tsx` | **新建** | 测试 |
| `packages/views/workflows/components/overview/architecture-detail-panel.tsx` | **新建** | 右侧滑出详情面板 |
| `packages/views/workflows/components/overview/architecture-detail-panel.test.tsx` | **新建** | 测试 |
| `packages/views/workflows/components/overview/data-flow-arrow.tsx` | **新建** | 阶段间箭头 |
| `packages/views/workflows/components/overview/data-flow-arrow.test.tsx` | **新建** | 测试 |
| `packages/views/workflows/components/overview/stage-swimlane.tsx` | **新建** | Stage 泳道行容器 |
| `packages/views/workflows/components/overview/stage-swimlane.test.tsx` | **新建** | 测试 |
| `packages/views/workflows/components/overview/workflow-panorama-page.tsx` | **新建** | 全景图页面主容器 |
| `packages/views/workflows/components/overview/panorama-page.test.tsx` | **新建** | 测试 |
| `packages/views/workflows/components/overview/index.ts` | **修改** | 追加新组件导出 |
| `packages/views/workflows/components/workflow-detail-shell.tsx` | **修改** | 三路视图分发（panorama/overview/editor） |
| `packages/views/locales/zh-Hans/workflows.json` | **修改** | 新增 i18n key |
| `packages/views/locales/en/workflows.json` | **修改** | 新增 i18n key |

**未修改文件（保留不变）：**
- `workflow-overview-page.tsx` — 旧 overview 页面完整保留
- `overview-page.test.tsx` — 旧测试完整保留
- `stage-canvas.tsx`, `stage-card.tsx`, `stage-node-dag.tsx`, `node-detail-panel.tsx`, `stage-create-dialog.tsx` — 旧组件全部保留

## 验证清单

1. `pnpm typecheck` — 零类型错误
2. `pnpm --filter @multica/views exec vitest run workflows/overview/` — 新 + 旧测试全部通过
3. `pnpm test` — 全量 TS 测试通过
4. 视觉验证（可选）：`pnpm dev:web` → workflow 详情页默认显示全景图，可切换到 overview/editor

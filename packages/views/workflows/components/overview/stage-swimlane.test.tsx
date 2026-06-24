// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen, waitFor } from "@testing-library/react";
import { StageSwimlane } from "./stage-swimlane";
import type { WorkflowStage, WorkflowNode, Agent, WorkflowEdge } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

const MOCK_STAGE: WorkflowStage = {
  id: "stage-1",
  workflow_id: "wf-1",
  name: "Stage One",
  description: "First workflow stage",
  sort_order: 0,
  node_count: 2,
  created_at: "",
  updated_at: "",
};

const MOCK_NODES: WorkflowNode[] = [
  {
    id: "n1",
    workflow_id: "wf-1",
    title: "brainstorming",
    description: "",
    position_x: 0,
    position_y: 0,
    format_schema: null,
    worker_type: "agent",
    worker_id: "agent-1",
    critic_type: "human",
    critic_id: null,
    critic_api_url: null,
    sort_order: 0,
    stage_id: "stage-1",
    created_at: "",
    updated_at: "",
  },
  {
    id: "n-worker-2",
    workflow_id: "wf-1",
    title: "handoff",
    description: "",
    position_x: 0,
    position_y: 0,
    format_schema: null,
    worker_type: "agent",
    worker_id: "agent-1",
    critic_type: "human",
    critic_id: null,
    critic_api_url: null,
    sort_order: 1,
    stage_id: "stage-1",
    created_at: "",
    updated_at: "",
  },
  {
    id: "n2",
    workflow_id: "wf-1",
    title: "quality-gate",
    description: "",
    position_x: 0,
    position_y: 0,
    format_schema: null,
    worker_type: "agent",
    worker_id: null,
    critic_type: "agent",
    critic_id: "agent-critic",
    critic_api_url: null,
    sort_order: 1,
    stage_id: "stage-1",
    created_at: "",
    updated_at: "",
  },
];

const MOCK_EDGES: WorkflowEdge[] = [
  {
    id: "edge-1",
    workflow_id: "wf-1",
    source_node_id: "n1",
    target_node_id: "n-worker-2",
    condition: null,
    created_at: "",
  },
];

const MOCK_AGENT: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "Planner",
  description: "Plans the work",
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
  thinking_level: "",
  plugin_id: "plugin-1",
  is_builtin: false,
  owner_id: null,
  skills: [],
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
};

const MOCK_PLUGIN: BuiltinPlugin = {
  id: "plugin-1",
  name: "Planning Plugin",
  description: "Creates a plan",
  slug: "planning-plugin",
  version: "1.0.0",
  category: "engineering",
};

describe("StageSwimlane", () => {
  const agentLookup = new Map<string, Agent | null>([
    ["agent-1", MOCK_AGENT],
    ["agent-critic", null],
  ]);
  const pluginLookup = new Map<string, BuiltinPlugin | null>([["plugin-1", MOCK_PLUGIN]]);

  it("renders stage name as header", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={MOCK_NODES}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByText("Stage One")).toBeTruthy();
  });

  it("renders critic badges as attachments below their plugin cards", () => {
    const onCardClick = vi.fn();
    const nodesWithAttachedCritic: WorkflowNode[] = [
      {
        ...MOCK_NODES[0]!,
        critic_type: "agent",
        critic_id: "agent-critic",
      },
      MOCK_NODES[1]!,
    ];
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={nodesWithAttachedCritic}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByTestId("plugin-card-n1")).toBeTruthy();
    expect(screen.getByTestId("critic-badge-n1")).toBeTruthy();
    expect(screen.getByTestId("critic-attachment-n1")).toBeTruthy();
    expect(screen.getByTestId("critic-attachment-connector-n1")).toBeTruthy();
  });

  it("keeps worker nodes with critic bindings rendered as plugin cards", () => {
    const onCardClick = vi.fn();
    const nodesWithAttachedCritic: WorkflowNode[] = [
      {
        ...MOCK_NODES[0]!,
        critic_type: "agent",
        critic_id: "agent-critic",
      },
      MOCK_NODES[1]!,
    ];
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={nodesWithAttachedCritic}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByTestId("plugin-card-n1")).toBeTruthy();
    expect(screen.getByTestId("critic-badge-n1")).toBeTruthy();
  });

  it("fires onCardClick when a card is clicked", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={MOCK_NODES}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    fireEvent.click(screen.getByTestId("plugin-card-n1"));
    expect(onCardClick).toHaveBeenCalledWith("n1", "worker");
  });

  it("shows empty state when no nodes for stage", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={[]}
        edges={[]}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByTestId("stage-swimlane-empty")).toBeTruthy();
  });

  it("renders stage styling and an internal flow layer", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={MOCK_NODES}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByTestId("stage-swimlane-stage-1").className).toContain("border-l-[6px]");
    expect(screen.getByTestId("stage-flow-stage-1")).toBeTruthy();
  });

  it("renders inline connectors between worker cards", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={[MOCK_NODES[0]!, MOCK_NODES[1]!]}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getAllByTestId("stage-flow-connector")).toHaveLength(1);
  });

  it("renders polished card-to-card connector icons", () => {
    const onCardClick = vi.fn();
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={[MOCK_NODES[0]!, MOCK_NODES[1]!]}
        edges={MOCK_EDGES}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByTestId("stage-flow-connector-icon")).toBeTruthy();
  });

  it("renders arc edge badges with fully visible icons", () => {
    const onCardClick = vi.fn();
    const middleWorkerNode: WorkflowNode = {
      ...MOCK_NODES[0]!,
      id: "n-middle",
      title: "handoff-middle",
      worker_id: "agent-1",
      critic_id: null,
      critic_type: "human",
      sort_order: 1,
    };
    const trailingWorkerNode: WorkflowNode = {
      ...MOCK_NODES[0]!,
      id: "n3",
      title: "handoff-third",
      worker_id: "agent-1",
      critic_id: null,
      critic_type: "human",
      sort_order: 2,
    };
    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={[
          MOCK_NODES[0]!,
          middleWorkerNode,
          trailingWorkerNode,
        ]}
        edges={[
          {
            id: "edge-1",
            workflow_id: "wf-1",
            source_node_id: "n1",
            target_node_id: "n3",
            condition: null,
            created_at: "",
          },
        ]}
        agentLookup={agentLookup}
        pluginLookup={pluginLookup}
        onCardClick={onCardClick}
      />,
    );
    expect(screen.getByTestId("stage-arc-edge-badge")).toBeTruthy();
    expect(screen.getByTestId("stage-arc-edge-icon")).toBeTruthy();
    expect(screen.getByTestId("stage-arc-edge-icon-shell")).toBeTruthy();
    expect(screen.getByTestId("stage-arc-edge-icon-shell").className).toContain("overflow-hidden");
    expect(screen.getByTestId("stage-arc-edge-icon").className).not.toContain("overflow-visible");
  });

  it("hides inline connectors when the next card wraps onto a new row", async () => {
    const onCardClick = vi.fn();
    const originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect;
    const originalResizeObserver = globalThis.ResizeObserver;

    const wrapNodes: WorkflowNode[] = [
      {
        ...MOCK_NODES[0]!,
        id: "n-wrap-1",
        title: "first-card",
        worker_id: "agent-1",
        critic_id: null,
        critic_type: "human",
        sort_order: 0,
      },
      {
        ...MOCK_NODES[0]!,
        id: "n-wrap-2",
        title: "second-card",
        worker_id: "agent-1",
        critic_id: null,
        critic_type: "human",
        sort_order: 1,
      },
      {
        ...MOCK_NODES[0]!,
        id: "n-wrap-3",
        title: "third-card",
        worker_id: "agent-1",
        critic_id: null,
        critic_type: "human",
        sort_order: 2,
      },
    ];

    const wrapPluginLookup = new Map<string, BuiltinPlugin | null>([["plugin-1", null]]);

    class ResizeObserverMock {
      observe() {}
      disconnect() {}
      unobserve() {}
    }
    globalThis.ResizeObserver = ResizeObserverMock as typeof ResizeObserver;

    HTMLElement.prototype.getBoundingClientRect = function mockRect() {
      const text = this.textContent ?? "";
      if (text.includes("first-card")) {
        return { x: 0, y: 0, top: 0, left: 0, right: 180, bottom: 68, width: 180, height: 68, toJSON() { return this; } };
      }
      if (text.includes("second-card")) {
        return { x: 220, y: 0, top: 0, left: 220, right: 400, bottom: 68, width: 180, height: 68, toJSON() { return this; } };
      }
      if (text.includes("third-card")) {
        return { x: 0, y: 96, top: 96, left: 0, right: 180, bottom: 164, width: 180, height: 68, toJSON() { return this; } };
      }
      return originalGetBoundingClientRect.call(this);
    };

    render(
      <StageSwimlane
        stage={MOCK_STAGE}
        nodes={wrapNodes}
        edges={[]}
        agentLookup={agentLookup}
        pluginLookup={wrapPluginLookup}
        onCardClick={onCardClick}
      />,
    );

    await waitFor(() => {
      expect(screen.getAllByTestId("stage-flow-connector")).toHaveLength(1);
    });

    HTMLElement.prototype.getBoundingClientRect = originalGetBoundingClientRect;
    globalThis.ResizeObserver = originalResizeObserver;
  });
});

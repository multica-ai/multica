// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@testing-library/react";
import { StageLane } from "./stage-lane";
import type { WorkflowStage, WorkflowNode, Agent } from "@multica/core/types";
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

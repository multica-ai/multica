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
  { id: "n1", workflow_id: "wf-1", title: "brainstorming", description: "", position_x: 0, position_y: 0, format_schema: null, worker_type: "agent", worker_id: "agent-1", critic_type: "human", critic_id: null, critic_api_url: null, sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "" },
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
    // n2 = critic_id non-null → CriticBadge
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

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

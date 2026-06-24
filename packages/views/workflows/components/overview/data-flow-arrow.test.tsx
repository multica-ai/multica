// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { DataFlowArrow } from "./data-flow-arrow";
import type { WorkflowEdge, WorkflowNode } from "@multica/core/types";

const MOCK_NODES: WorkflowNode[] = [
  {
    id: "n1",
    workflow_id: "wf-1",
    title: "Node 1",
    description: "",
    position_x: 0,
    position_y: 0,
    format_schema: null,
    worker_type: "agent",
    worker_id: "a1",
    critic_type: "human",
    critic_id: null,
    critic_api_url: null,
    sort_order: 0,
    stage_id: "stage-1",
    created_at: "",
    updated_at: "",
  },
  {
    id: "n2",
    workflow_id: "wf-1",
    title: "Node 2",
    description: "",
    position_x: 0,
    position_y: 0,
    format_schema: null,
    worker_type: "agent",
    worker_id: "a2",
    critic_type: "human",
    critic_id: null,
    critic_api_url: null,
    sort_order: 0,
    stage_id: "stage-2",
    created_at: "",
    updated_at: "",
  },
];

describe("DataFlowArrow", () => {
  it("renders a fallback connector when no matching cross-stage edges exist", () => {
    const sameStageEdges: WorkflowEdge[] = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n1",
        condition: null,
        created_at: "",
      },
    ];
    const { container } = render(
      <DataFlowArrow
        edges={sameStageEdges}
        nodes={MOCK_NODES}
        sourceStageId="stage-1"
        targetStageId="stage-2"
      />,
    );
    expect(container.querySelector('[data-testid="data-flow-arrow"]')).toBeTruthy();
    expect(container.querySelector('[data-testid="data-flow-arrow-line"]')).toBeTruthy();
  });

  it("renders arrow for cross-stage edges", () => {
    const crossStageEdges: WorkflowEdge[] = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n2",
        condition: null,
        created_at: "",
      },
    ];
    render(
      <DataFlowArrow
        edges={crossStageEdges}
        nodes={MOCK_NODES}
        sourceStageId="stage-1"
        targetStageId="stage-2"
      />,
    );
    expect(screen.getByTestId("data-flow-arrow")).toBeTruthy();
  });

  it("renders a visible connector line between stages", () => {
    const crossStageEdges: WorkflowEdge[] = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n2",
        condition: null,
        created_at: "",
      },
    ];
    render(
      <DataFlowArrow
        edges={crossStageEdges}
        nodes={MOCK_NODES}
        sourceStageId="stage-1"
        targetStageId="stage-2"
      />,
    );
    expect(
      screen
        .getByTestId("data-flow-arrow")
        .querySelector('[data-testid="data-flow-arrow-line"]'),
    ).toBeTruthy();
  });

  it("renders a styled transition pill", () => {
    const crossStageEdges: WorkflowEdge[] = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n2",
        condition: null,
        created_at: "",
      },
    ];
    render(
      <DataFlowArrow
        edges={crossStageEdges}
        nodes={MOCK_NODES}
        sourceStageId="stage-1"
        targetStageId="stage-2"
      />,
    );
    expect(screen.getByTestId("data-flow-arrow-pill")).toBeTruthy();
  });

  it("renders a label summarizing the flow", () => {
    const crossStageEdges: WorkflowEdge[] = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n2",
        condition: null,
        created_at: "",
      },
    ];
    render(
      <DataFlowArrow
        edges={crossStageEdges}
        nodes={MOCK_NODES}
        sourceStageId="stage-1"
        targetStageId="stage-2"
      />,
    );
    expect(screen.getByText("Node 1 -> Node 2")).toBeTruthy();
  });

  it("omits the summary label when there are no matching cross-stage edges", () => {
    const sameStageEdges: WorkflowEdge[] = [
      {
        id: "e1",
        workflow_id: "wf-1",
        source_node_id: "n1",
        target_node_id: "n1",
        condition: null,
        created_at: "",
      },
    ];
    render(
      <DataFlowArrow
        edges={sameStageEdges}
        nodes={MOCK_NODES}
        sourceStageId="stage-1"
        targetStageId="stage-2"
      />,
    );
    expect(screen.queryByText("Node 1 -> Node 2")).toBeNull();
  });
});

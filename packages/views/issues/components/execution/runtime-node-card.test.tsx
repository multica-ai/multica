import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { RuntimeNodeCard } from "./runtime-node-card";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";

const baseNode: WorkflowNode = {
  id: "node-1",
  workflow_id: "wf-1",
  title: "需求收集",
  description: "",
  position_x: 0,
  position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "agent-1",
  critic_type: "agent",
  critic_id: "agent-2",
  critic_api_url: null,
  sort_order: 0,
  stage_id: null,
  created_at: "2026-01-01",
  updated_at: "2026-01-01",
};

const completedRun: WorkflowNodeRun = {
  id: "run-1",
  workflow_run_id: "wr-1",
  workflow_node_id: "node-1",
  node_title: "需求收集",
  status: "completed",
  retry_count: 0,
  worker_type: "agent",
  worker_id: "agent-1",
  worker_output: null,
  worker_agent_task_id: null,
  critic_type: "agent",
  critic_id: "agent-2",
  critic_output: null,
  critic_comment: "",
  critic_agent_task_id: null,
  agent_task_id: null,
  session_id: null,
  runtime_id: null,
  device_id: null,
  started_at: null,
  completed_at: null,
  created_at: "2026-01-01",
  updated_at: "2026-01-01",
};

describe("RuntimeNodeCard", () => {
  it("renders with completed status", () => {
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName="审核员"
        onClick={vi.fn()}
      />,
    );
    const card = screen.getByTestId("runtime-node-card-node-1");
    expect(card).toBeInTheDocument();
  });

  it("renders without left border when nodeRun is null (not started)", () => {
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={null}
        workerName={null}
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    const card = screen.getByTestId("runtime-node-card-node-1");
    expect(card).toBeInTheDocument();
  });

  it("does not render critic row when critic_type is empty and critic_id is null", () => {
    const noCriticNode: WorkflowNode = {
      ...baseNode,
      critic_type: "" as any,
      critic_id: null,
    };
    render(
      <RuntimeNodeCard
        node={noCriticNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    expect(screen.queryByText(/审核员/)).not.toBeInTheDocument();
  });

  it("calls onClick with node id on click", async () => {
    const onClick = vi.fn();
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName="审核员"
        onClick={onClick}
      />,
    );
    await userEvent.click(screen.getByTestId("runtime-node-card-node-1"));
    expect(onClick).toHaveBeenCalledWith("node-1");
  });
});

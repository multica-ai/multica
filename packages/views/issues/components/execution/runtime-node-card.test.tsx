import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { RuntimeNodeCard } from "./runtime-node-card";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";

// Mock @multica/views/i18n for useT hook — handles function selector form
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: unknown) => {
      if (typeof selector === "function") {
        return selector({
          execution: {
            card: {
              worker_label: "Worker",
              critic_label: "Critic",
              artifacts_label: "Artifacts",
            },
            detail_panel: {
              worker_output: "Worker Output",
              critic_output: "Critic Output",
            },
          },
        });
      }
      return String(selector);
    },
  }),
}));

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

  it("renders pending status when nodeRun is null (not started)", () => {
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
    // Critic row not rendered at all
    expect(screen.queryByText("Critic:")).not.toBeInTheDocument();
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

  it("shows artifact row with names when outputs exist", () => {
    const runWithOutputs: WorkflowNodeRun = {
      ...completedRun,
      worker_output: { summary: "已完成需求文档" },
      critic_output: { comment: "审核通过" },
    };
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={runWithOutputs}
        workerName="小助手"
        criticName="审核员"
        onClick={vi.fn()}
      />,
    );
    expect(screen.getByText(/Artifacts:/)).toBeInTheDocument();
    expect(screen.getByText(/Worker Output/)).toBeInTheDocument();
    expect(screen.getByText(/Critic Output/)).toBeInTheDocument();
  });

  it("does not show artifact row when no outputs exist", () => {
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName="审核员"
        onClick={vi.fn()}
      />,
    );
    expect(screen.queryByText(/Artifacts:/)).not.toBeInTheDocument();
  });

  it("renders Bot icon for agent worker_type", () => {
    const { container } = render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    // lucide-bot class on the svg
    expect(container.querySelector(".lucide-bot")).toBeInTheDocument();
  });

  it("renders Building2 icon for squad worker_type", () => {
    const squadNode: WorkflowNode = {
      ...baseNode,
      worker_type: "squad",
    };
    const { container } = render(
      <RuntimeNodeCard
        node={squadNode}
        nodeRun={completedRun}
        workerName="全栈小队"
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    expect(container.querySelector(".lucide-building-2")).toBeInTheDocument();
  });

  it("renders User icon for human worker_type", () => {
    const humanNode: WorkflowNode = {
      ...baseNode,
      worker_type: "human",
    };
    const { container } = render(
      <RuntimeNodeCard
        node={humanNode}
        nodeRun={completedRun}
        workerName="张伟"
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    expect(container.querySelector(".lucide-user")).toBeInTheDocument();
  });

  it("renders status icon on worker row when nodeRun exists", () => {
    const { container } = render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName="审核员"
        onClick={vi.fn()}
      />,
    );
    // Worker row has a status icon — completed status maps to data-testid="status-icon"
    const statusIcons = container.querySelectorAll('[data-testid="status-icon"]');
    // At least the worker row status icon is present (title row also has one)
    expect(statusIcons.length).toBeGreaterThanOrEqual(1);
  });
});

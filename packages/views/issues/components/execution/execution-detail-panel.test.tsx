import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { ExecutionDetailPanel } from "./execution-detail-panel";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";

// Mock @multica/views/i18n for useT hook
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        "execution.detail_panel.status_path": "Status Path",
        "execution.detail_panel.worker": "Worker",
        "execution.detail_panel.critic": "Critic",
        "execution.detail_panel.not_configured": "Not configured",
        "execution.detail_panel.metadata": "Metadata",
        "execution.detail_panel.started_at": "Started At",
        "execution.detail_panel.completed_at": "Completed At",
        "execution.detail_panel.duration": "Duration",
        "execution.detail_panel.retry_count": "Retry Count",
      };
      return map[key] || key;
    },
  }),
}));

const node: WorkflowNode = {
  id: "n1",
  workflow_id: "w1",
  title: "编码",
  description: "",
  position_x: 0,
  position_y: 0,
  format_schema: null,
  worker_type: "agent",
  worker_id: "a1",
  critic_type: "agent",
  critic_id: "a2",
  critic_api_url: null,
  sort_order: 0,
  stage_id: null,
  created_at: "2026-06-25T10:00:00Z",
  updated_at: "2026-06-25T10:00:00Z",
};

const run: WorkflowNodeRun = {
  id: "r1",
  workflow_run_id: "wr1",
  workflow_node_id: "n1",
  node_title: "编码",
  status: "working",
  retry_count: 0,
  worker_type: "agent",
  worker_id: "a1",
  worker_output: { pr: "#42" },
  worker_agent_task_id: null,
  critic_type: "agent",
  critic_id: "a2",
  critic_output: null,
  critic_comment: "",
  critic_agent_task_id: null,
  agent_task_id: null,
  session_id: null,
  runtime_id: null,
  device_id: null,
  started_at: "2026-06-25T10:00:00Z",
  completed_at: null,
  created_at: "2026-06-25T10:00:00Z",
  updated_at: "2026-06-25T10:05:00Z",
};

describe("ExecutionDetailPanel", () => {
  it("renders node title in header", () => {
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={run}
        workerName="后端助手"
        criticName="审核员"
        onClose={vi.fn()}
        wsId="ws-1"
      />,
    );
    expect(screen.getByText("编码")).toBeInTheDocument();
  });

  it("calls onClose when clicking mask", async () => {
    const onClose = vi.fn();
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={run}
        workerName="后端助手"
        criticName="审核员"
        onClose={onClose}
        wsId="ws-1"
      />,
    );
    await userEvent.click(screen.getByTestId("detail-panel-mask"));
    expect(onClose).toHaveBeenCalled();
  });

  it("calls onClose on Escape key", async () => {
    const onClose = vi.fn();
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={run}
        workerName="后端助手"
        criticName="审核员"
        onClose={onClose}
        wsId="ws-1"
      />,
    );
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("shows 'Not configured' when no critic", () => {
    const noCriticNode: WorkflowNode = {
      ...node,
      critic_type: "" as WorkflowNode["critic_type"],
      critic_id: null,
    };
    render(
      <ExecutionDetailPanel
        node={noCriticNode}
        nodeRun={run}
        workerName="后端助手"
        criticName={null}
        onClose={vi.fn()}
        wsId="ws-1"
      />,
    );
    expect(screen.getByText(/Not configured/i)).toBeInTheDocument();
  });
});

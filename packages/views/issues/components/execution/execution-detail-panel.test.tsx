import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { ExecutionDetailPanel } from "./execution-detail-panel";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";

// Mock @multica/views/i18n for useT hook — handles function selector form
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: unknown) => {
      if (typeof selector === "function") {
        return selector({
          detail: {
            desc_label: "Description",
          },
          execution: {
            detail_panel: {
              status_path: "Status Path",
              worker: "Worker",
              critic: "Critic",
              not_configured: "Not configured",
              worker_output: "Worker Output",
              critic_output: "Critic Output",
              attachments: "Artifacts",
              no_output: "No output yet",
              metadata: "Metadata",
              started_at: "Started At",
              completed_at: "Completed At",
              duration: "Duration",
              retry_count: "Retry Count",
              error: "Error",
              view_full_issue: "View full issue",
              unblock: "Unblock",
              retry: "Retry",
              review_comment: "Review Comment",
            },
          },
          });
      }
      return String(selector);
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

  it("renders artifact section with empty state when no outputs", () => {
    const noOutputRun = { ...run, worker_output: null };
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={noOutputRun}
        workerName="后端助手"
        criticName="审核员"
        onClose={vi.fn()}
        wsId="ws-1"
      />,
    );
    expect(screen.getByText("Artifacts")).toBeInTheDocument();
    expect(screen.getByText("No output yet")).toBeInTheDocument();
  });

  it("renders metadata with retry_count always visible", () => {
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
    expect(screen.getByText("Metadata")).toBeInTheDocument();
    expect(screen.getByText("Retry Count")).toBeInTheDocument();
    expect(screen.getByText("0")).toBeInTheDocument();
  });

  it("renders duration in human-readable format when completed", () => {
    const completedRun = {
      ...run,
      status: "completed" as const,
      started_at: "2026-06-25T10:00:00Z",
      completed_at: "2026-06-25T10:05:30Z",
    };
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={completedRun}
        workerName="后端助手"
        criticName="审核员"
        onClose={vi.fn()}
        wsId="ws-1"
      />,
    );
    expect(screen.getByText("5m 30s")).toBeInTheDocument();
  });

  it("renders 'View full issue' link when issueId provided", () => {
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={run}
        workerName="后端助手"
        criticName="审核员"
        onClose={vi.fn()}
        wsId="demo111"
        issueId="33cf28ab-f5ce-4ff7-b199-fb4a6c32064c"
      />,
    );
    const link = screen.getByText("View full issue");
    expect(link).toBeInTheDocument();
    expect(link.closest("a")).toHaveAttribute(
      "href",
      "/tasks/demo111/issues/33cf28ab-f5ce-4ff7-b199-fb4a6c32064c",
    );
  });

  it("does not render 'View full issue' when issueId not provided", () => {
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
    expect(screen.queryByText("View full issue")).not.toBeInTheDocument();
  });

  it("renders unblock button when status is blocked and onUnblock provided", () => {
    const blockedRun = { ...run, status: "blocked" as const };
    const onUnblock = vi.fn();
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={blockedRun}
        workerName="后端助手"
        criticName="审核员"
        onClose={vi.fn()}
        wsId="ws-1"
        onUnblock={onUnblock}
      />,
    );
    expect(screen.getByText("Unblock")).toBeInTheDocument();
  });

  it("renders retry button when status is failed and onRetry provided", () => {
    const failedRun = { ...run, status: "failed" as const };
    const onRetry = vi.fn();
    render(
      <ExecutionDetailPanel
        node={node}
        nodeRun={failedRun}
        workerName="后端助手"
        criticName="审核员"
        onClose={vi.fn()}
        wsId="ws-1"
        onRetry={onRetry}
      />,
    );
    expect(screen.getByText("Retry")).toBeInTheDocument();
  });
});

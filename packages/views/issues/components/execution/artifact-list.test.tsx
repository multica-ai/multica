import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ArtifactList } from "./artifact-list";
import type { WorkflowNodeRun } from "@multica/core/types";

// Mock @multica/views/i18n for useT hook
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        "execution.detail_panel.worker_output": "Worker Output",
        "execution.detail_panel.critic_output": "Critic Output",
      };
      return map[key] || key;
    },
  }),
}));

const baseRun: WorkflowNodeRun = {
  id: "run-1",
  workflow_run_id: "wr-1",
  workflow_node_id: "wn-1",
  node_title: "test",
  status: "completed",
  retry_count: 0,
  worker_type: "agent",
  worker_id: "agent-1",
  worker_output: { summary: "PR #42 created" },
  worker_agent_task_id: null,
  critic_type: "agent",
  critic_id: "agent-2",
  critic_output: { approved: true, score: 92 },
  critic_comment: "LGTM",
  critic_agent_task_id: null,
  agent_task_id: null,
  session_id: null,
  runtime_id: null,
  device_id: null,
  started_at: "2026-06-25T10:00:00Z",
  completed_at: "2026-06-25T10:05:00Z",
  created_at: "2026-06-25T10:00:00Z",
  updated_at: "2026-06-25T10:05:00Z",
};

describe("ArtifactList", () => {
  it("renders nothing when no outputs or attachments", () => {
    const empty = { ...baseRun, worker_output: null, critic_output: null };
    const { container } = render(<ArtifactList nodeRun={empty} />);
    expect(container.firstChild).toBeNull();
  });

  it("renders worker output section when present", () => {
    render(<ArtifactList nodeRun={baseRun} />);
    expect(screen.getByText(/Worker Output/i)).toBeInTheDocument();
  });

  it("renders critic output section when present", () => {
    render(<ArtifactList nodeRun={baseRun} />);
    expect(screen.getByText(/Critic Output/i)).toBeInTheDocument();
  });
});

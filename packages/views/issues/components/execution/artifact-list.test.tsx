import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ArtifactList } from "./artifact-list";
import type { WorkflowNodeRun } from "@multica/core/types";

// Mock @multica/views/i18n for useT hook — handles function selector form
vi.mock("@multica/views/i18n", () => ({
  useT: () => ({
    t: (selector: unknown) => {
      if (typeof selector === "function") {
        return selector({
          execution: {
            detail_panel: {
              worker_output: "Worker Output",
              critic_output: "Critic Output",
              attachments: "Artifacts",
              no_output: "No output yet",
            },
          },
        });
      }
      return String(selector);
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
  it("renders section header and empty state when no outputs", () => {
    const empty = { ...baseRun, worker_output: null, critic_output: null };
    render(<ArtifactList nodeRun={empty} />);
    expect(screen.getByText("Artifacts")).toBeInTheDocument();
    expect(screen.getByText("No output yet")).toBeInTheDocument();
  });

  it("renders worker output section when present", () => {
    const runWithWorker = { ...baseRun, critic_output: null };
    render(<ArtifactList nodeRun={runWithWorker} />);
    expect(screen.getByText("Worker Output")).toBeInTheDocument();
    expect(screen.queryByText("No output yet")).not.toBeInTheDocument();
  });

  it("renders critic output section when present", () => {
    render(<ArtifactList nodeRun={baseRun} />);
    expect(screen.getByText("Critic Output")).toBeInTheDocument();
  });

  it("renders both worker and critic outputs when both present", () => {
    render(<ArtifactList nodeRun={baseRun} />);
    expect(screen.getByText("Artifacts")).toBeInTheDocument();
    expect(screen.getByText("Worker Output")).toBeInTheDocument();
    expect(screen.getByText("Critic Output")).toBeInTheDocument();
  });
});

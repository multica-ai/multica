// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { AgentTask } from "@multica/core/types/agent";

// Per-user preference gate. Default on for these tests (we exercise the visual
// path + escape hatch); individual cases flip it.
let visualPref = true;
vi.mock("@multica/core/auth", () => {
  const state = () => ({ user: { visual_execution_history: visualPref } });
  const useAuthStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) =>
      sel ? sel(state()) : state(),
    { getState: state },
  );
  return { useAuthStore };
});

vi.mock("@multica/core/api", () => ({
  api: { listTaskMessages: vi.fn().mockResolvedValue([]) },
}));

// Capture which dialog renders and with what task status, so we can prove the
// running→completed handoff happens in place (same mount).
vi.mock("../../chat/components/visual-run-history", () => ({
  VisualRunHistory: ({ task }: { task: AgentTask }) => (
    <div data-testid="visual-dialog" data-status={task.status} />
  ),
}));

vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({ task }: { task: AgentTask }) => (
    <div data-testid="raw-dialog" data-status={task.status} />
  ),
}));

import { RunHistoryDialogHost } from "./run-history-dialog-host";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    trigger_summary: "Do the thing",
    ...overrides,
  } as AgentTask;
}

afterEach(() => {
  visualPref = true;
  cleanup();
});

describe("RunHistoryDialogHost", () => {
  it("renders nothing when no task is open", () => {
    const { container } = render(
      <RunHistoryDialogHost task={null} onClose={() => {}} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the same dialog mounted across the running→completed handoff", () => {
    const { rerender } = render(
      <RunHistoryDialogHost task={makeTask()} onClose={() => {}} />,
    );

    expect(screen.getByTestId("visual-dialog")).toHaveAttribute(
      "data-status",
      "running",
    );

    // Same task id, new status object — mirrors the section's active→past
    // re-bucket. The dialog must stay open and reflect the terminal status.
    rerender(
      <RunHistoryDialogHost
        task={makeTask({
          status: "completed",
          completed_at: "2026-06-08T08:05:00Z",
        })}
        onClose={() => {}}
      />,
    );

    const dialog = screen.getByTestId("visual-dialog");
    expect(dialog).toBeInTheDocument();
    expect(dialog).toHaveAttribute("data-status", "completed");
  });

  it("renders the raw event log when the preference is off", () => {
    visualPref = false;
    render(<RunHistoryDialogHost task={makeTask()} onClose={() => {}} />);

    expect(screen.getByTestId("raw-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("visual-dialog")).not.toBeInTheDocument();
  });
});

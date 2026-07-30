// FIR-4073 — the grey "waiting on a paused machine" row. It replaces the issue
// comment the auto-pause used to write, so the wording carries the whole fact:
// what state we are in, whose run it was, and when it ends.

import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DeadFailedRun } from "../dead-failed-runs";

const mockListTasksByIssue = vi.hoisted(() => vi.fn());
const mockListTaskMessages = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listTasksByIssue: mockListTasksByIssue,
    listTaskMessages: mockListTaskMessages,
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getAgentName: () => "Sara" }),
}));

vi.mock("@multica/views/common/task-transcript", () => ({
  AgentTranscriptDialog: () => <div data-testid="run-log-dialog">run log</div>,
  buildTimeline: (msgs: unknown[]) => msgs,
}));

import { PausedRunActivityRow, pausedRunLabel } from "./paused-run-activity";

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const run: DeadFailedRun = {
  task_id: "task-1",
  agent_id: "agent-1",
  issue_id: "issue-1",
  failure_reason: "runtime_paused",
  attempt: 1,
  max_attempts: 1,
  resume_possible: false,
  runtime_name: "sara-mac",
  runtime_paused: true,
  unpause_at: "2026-07-29T16:00:00Z",
};

describe("pausedRunLabel", () => {
  const now = new Date("2026-07-29T14:00:00Z");

  it("names the time the machine comes back", () => {
    expect(pausedRunLabel("2026-07-29T16:00:00Z", now)).toMatch(/^Paused — resumes /);
  });

  it("says a human is needed when there is no unpause time", () => {
    expect(pausedRunLabel(undefined, now)).toBe("Paused — needs a human to resume");
  });

  it("does not show a past time as if it were still in the future", () => {
    expect(pausedRunLabel("2026-07-29T13:00:00Z", now)).toBe("Paused — resuming any moment");
  });

  it("degrades instead of crashing on an unparseable timestamp", () => {
    expect(pausedRunLabel("not-a-date", now)).toBe("Paused");
  });
});

describe("PausedRunActivityRow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTaskMessages.mockResolvedValue([]);
    mockListTasksByIssue.mockResolvedValue([
      { id: "task-1", issue_id: "issue-1", status: "failed" },
    ]);
  });

  it("reads as waiting, not as a failure", () => {
    render(<PausedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    expect(screen.getByTestId("paused-run-row")).toBeInTheDocument();
    expect(screen.getByText(/^Paused/)).toBeInTheDocument();
    expect(screen.getByText(/sara-mac/)).toBeInTheDocument();
  });

  it("says which agent is waiting", () => {
    render(<PausedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    expect(screen.getByText(/Sara/)).toBeInTheDocument();
    // One attempt is not a retry series, so counting them would be noise.
    expect(screen.queryByText(/attempt/i)).toBeNull();
  });

  it("offers the run log but no retry buttons", async () => {
    render(<PausedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    await screen.findByRole("button", { name: /open run log/i });
    // Retrying against a paused machine would fail the same way, so the row
    // deliberately offers nothing but the log.
    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: /start over/i })).toBeNull();
  });
});

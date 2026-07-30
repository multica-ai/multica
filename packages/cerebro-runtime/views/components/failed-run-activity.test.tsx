import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DeadFailedRun } from "../dead-failed-runs";

const mockRerunIssue = vi.hoisted(() => vi.fn());
const mockListTasksByIssue = vi.hoisted(() => vi.fn());
const mockListTaskMessages = vi.hoisted(() => vi.fn());
const mockTranscriptDialog = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    rerunIssue: mockRerunIssue,
    listTasksByIssue: mockListTasksByIssue,
    listTaskMessages: mockListTaskMessages,
  },
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getAgentName: () => "Sara" }),
}));

// The real dialog drags the whole transcript tree into a unit test. Here we
// only need to know it was opened, and with which run.
vi.mock("@multica/views/common/task-transcript", () => ({
  AgentTranscriptDialog: (props: { agentName: string }) => {
    mockTranscriptDialog(props);
    return <div data-testid="run-log-dialog">run log</div>;
  },
  buildTimeline: (msgs: unknown[]) => msgs,
}));

import { FailedRunActivityRow } from "./failed-run-activity";

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
  failure_reason: "runtime_offline",
  attempt: 2,
  max_attempts: 3,
  resume_possible: true,
  runtime_name: "sara-mac",
};

describe("FailedRunActivityRow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockRerunIssue.mockResolvedValue(undefined);
    mockListTaskMessages.mockResolvedValue([]);
    mockListTasksByIssue.mockResolvedValue([
      { id: "task-1", issue_id: "issue-1", status: "failed" },
    ]);
  });

  it("names what broke and which runtime it ran on", () => {
    render(<FailedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    expect(screen.getByTestId("failed-run-row")).toBeInTheDocument();
    expect(screen.getByText(/sara-mac/)).toBeInTheDocument();
    // A real, human failure label — not the raw reason code.
    expect(screen.queryByText("runtime_offline")).toBeNull();
  });

  it("says which agent failed and which attempt it was", () => {
    render(<FailedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    expect(screen.getByText(/Sara/)).toBeInTheDocument();
    expect(screen.getByText(/attempt 2 of 3/)).toBeInTheDocument();
  });

  it("opens the run log from the whole text block, not a stray icon", async () => {
    render(<FailedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    const opener = await screen.findByRole("button", { name: /open run log/i });
    await userEvent.click(opener);

    await waitFor(() => expect(screen.getByTestId("run-log-dialog")).toBeInTheDocument());
    expect(mockListTaskMessages).toHaveBeenCalledWith("task-1");
    expect(mockTranscriptDialog).toHaveBeenCalledWith(
      expect.objectContaining({ agentName: "Sara" }),
    );
  });

  it("offers the run log, Resume and Start over on one row", async () => {
    render(<FailedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    expect(await screen.findByRole("button", { name: /open run log/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /resume/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /start over/i })).toBeInTheDocument();
  });

  it("starts the run over from the row", async () => {
    render(<FailedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    await userEvent.click(screen.getByRole("button", { name: /start over/i }));

    await waitFor(() =>
      expect(mockRerunIssue).toHaveBeenCalledWith("MUL-1", "task-1", false),
    );
  });

  it("keeps the row usable when the run's task is not in the list yet", () => {
    mockListTasksByIssue.mockResolvedValue([]);

    render(<FailedRunActivityRow issueId="MUL-1" run={run} />, { wrapper });

    expect(screen.getByRole("button", { name: /resume/i })).toBeInTheDocument();
    // No task to read a log for yet, so the text block is plain text.
    expect(screen.queryByRole("button", { name: /open run log/i })).toBeNull();
  });

  it("disables Resume when the conversation cannot be picked back up", () => {
    render(
      <FailedRunActivityRow
        issueId="MUL-1"
        run={{ ...run, resume_possible: false, blocked_reason: "No session" }}
      />,
      { wrapper },
    );

    expect(screen.getByRole("button", { name: /resume/i })).toBeDisabled();
  });
});

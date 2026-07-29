import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockRerunIssue = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockUseIssueFailedRuns = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { rerunIssue: mockRerunIssue },
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

vi.mock("../dead-failed-runs", () => ({
  ISSUE_FAILED_RUNS_KEY: (issueId: string) => ["cerebro-issue-failed-runs", issueId],
  useIssueFailedRuns: mockUseIssueFailedRuns,
}));

import { RunRetryActions, TranscriptRunRetryActions } from "./run-retry-actions";

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("RunRetryActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockRerunIssue.mockResolvedValue(undefined);
    mockUseIssueFailedRuns.mockReturnValue([]);
  });

  it("resumes the same conversation", async () => {
    render(
      <RunRetryActions issueId="MUL-1" taskId="task-1" resumePossible={true} />,
      { wrapper },
    );

    await userEvent.click(screen.getByRole("button", { name: /resume/i }));

    await waitFor(() =>
      expect(mockRerunIssue).toHaveBeenCalledWith("MUL-1", "task-1", true),
    );
  });

  it("starts over with a blank conversation", async () => {
    render(
      <RunRetryActions issueId="MUL-1" taskId="task-1" resumePossible={true} />,
      { wrapper },
    );

    await userEvent.click(screen.getByRole("button", { name: /start over/i }));

    await waitFor(() =>
      expect(mockRerunIssue).toHaveBeenCalledWith("MUL-1", "task-1", false),
    );
  });

  it("disables Resume and explains why when the run cannot be continued", () => {
    render(
      <RunRetryActions
        issueId="MUL-1"
        taskId="task-1"
        resumePossible={false}
        blockedReason="The runtime is offline"
      />,
      { wrapper },
    );

    const resume = screen.getByRole("button", { name: /resume/i });
    expect(resume).toBeDisabled();
    expect(resume.getAttribute("title")).toBe("The runtime is offline");
    // Start over never depends on the old session, so it stays available.
    expect(screen.getByRole("button", { name: /start over/i })).not.toBeDisabled();
  });

  it("reports a failed rerun instead of pretending it started", async () => {
    mockRerunIssue.mockRejectedValue(new Error("Runtime is offline"));

    render(
      <RunRetryActions issueId="MUL-1" taskId="task-1" resumePossible={true} />,
      { wrapper },
    );

    await userEvent.click(screen.getByRole("button", { name: /start over/i }));

    await waitFor(() =>
      expect(mockToastError).toHaveBeenCalledWith("Runtime is offline"),
    );
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });
});

describe("TranscriptRunRetryActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockRerunIssue.mockResolvedValue(undefined);
    mockUseIssueFailedRuns.mockReturnValue([]);
  });

  it("renders nothing for a run that did not fail", () => {
    const { container } = render(
      <TranscriptRunRetryActions
        task={{ id: "task-1", issue_id: "issue-1", status: "completed" }}
      />,
      { wrapper },
    );

    expect(container.firstChild).toBeNull();
  });

  it("blocks Resume when the failed run cannot be resumed", () => {
    mockUseIssueFailedRuns.mockReturnValue([
      {
        task_id: "task-1",
        issue_id: "issue-1",
        attempt: 1,
        max_attempts: 1,
        resume_possible: false,
        blocked_reason: "No session to continue",
      },
    ]);

    render(
      <TranscriptRunRetryActions
        task={{ id: "task-1", issue_id: "issue-1", status: "failed" }}
      />,
      { wrapper },
    );

    expect(screen.getByRole("button", { name: /resume/i })).toBeDisabled();
  });

  it("still offers Resume for a failure that fell out of the alert window", () => {
    mockUseIssueFailedRuns.mockReturnValue([]);

    render(
      <TranscriptRunRetryActions
        task={{ id: "task-1", issue_id: "issue-1", status: "failed" }}
      />,
      { wrapper },
    );

    expect(screen.getByRole("button", { name: /resume/i })).not.toBeDisabled();
  });
});

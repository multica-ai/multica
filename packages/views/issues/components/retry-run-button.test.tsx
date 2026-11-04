import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import type { AgentTask } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";
import { renderWithI18n } from "../../test/i18n";
import { RetryRunButton, canRetryRun } from "./retry-run-button";

vi.mock("@multica/core/api", () => ({ api: { rerunIssue: vi.fn() }, dispatchReasonCode: vi.fn() }));

const id = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

function task(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id, agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "failed",
    priority: 0, created_at: "2026-11-03T00:00:00Z", started_at: null, dispatched_at: null,
    completed_at: null, result: null, error: null, ...overrides,
  };
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("RetryRunButton", () => {
  it("offers a new-session retry next to the plain one", () => {
    renderWithI18n(<RetryRunButton task={task()} issueId="issue-1" />);
    expect(screen.getByRole("button", { name: "Retry run" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry in a new session" })).toBeTruthy();
  });

  it("retries in the same session by default, naming this row's run", async () => {
    vi.mocked(api.rerunIssue).mockResolvedValue(task({ id: "rerun" }));
    renderWithI18n(<RetryRunButton task={task()} issueId="issue-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Retry run" }));
    await waitFor(() => expect(api.rerunIssue).toHaveBeenCalledWith("issue-1", id));
  });

  it("asks the server for a fresh session when the new-session button is clicked", async () => {
    vi.mocked(api.rerunIssue).mockResolvedValue(task({ id: "rerun" }));
    renderWithI18n(<RetryRunButton task={task()} issueId="issue-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Retry in a new session" }));
    await waitFor(() => expect(api.rerunIssue).toHaveBeenCalledWith("issue-1", id, { fresh: true }));
  });

  it("warns in the new-session copy that the working directory resets too", () => {
    // `fresh` drops the working directory along with the session, so uncommitted
    // changes in the old one do not carry over. The tooltip is the only place a
    // user learns that — guard the wording against the endpoint's behaviour.
    expect(enIssues.execution_log.retry_fresh_task_tooltip).toContain(
      "working directory",
    );
  });

  it("disables both variants while a rerun is in flight", async () => {
    let release: () => void = () => {};
    vi.mocked(api.rerunIssue).mockReturnValue(
      new Promise((resolve) => {
        release = () => resolve(task({ id: "rerun" }));
      }) as Promise<AgentTask>,
    );
    renderWithI18n(<RetryRunButton task={task()} issueId="issue-1" />);
    fireEvent.click(screen.getByRole("button", { name: "Retry run" }));
    await waitFor(() => expect(api.rerunIssue).toHaveBeenCalled());
    expect((screen.getByRole("button", { name: "Retry in a new session" }) as HTMLButtonElement).disabled).toBe(true);
    release();
    await waitFor(() =>
      expect((screen.getByRole("button", { name: "Retry run" }) as HTMLButtonElement).disabled).toBe(false),
    );
  });

  it("keeps retry limited to runs that ended without success", () => {
    expect(canRetryRun(task({ status: "failed" }))).toBe(true);
    expect(canRetryRun(task({ status: "cancelled" }))).toBe(true);
    expect(canRetryRun(task({ status: "completed" }))).toBe(false);
    expect(canRetryRun(task({ status: "running" }))).toBe(false);
  });
});

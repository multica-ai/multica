// @vitest-environment jsdom

import { act, cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  cancelTask: vi.fn(),
  taskMessagesOptions: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    cancelTask: mockState.cancelTask,
    rerunIssue: vi.fn(),
  },
}));

vi.mock("@multica/core/chat/queries", () => ({
  taskMessagesOptions: mockState.taskMessagesOptions,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: ({ title }: { title?: string }) => (
    <button type="button">{title ?? "Transcript"}</button>
  ),
}));

vi.mock("./terminate-task-confirm-dialog", () => ({
  TerminateTaskConfirmDialog: ({
    open,
    onConfirm,
  }: {
    open: boolean;
    onConfirm: () => void;
  }) =>
    open ? (
      <button type="button" onClick={onConfirm}>
        Confirm cancel
      </button>
    ) : null,
}));

import { ActiveTaskRow } from "./execution-log-section";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    trigger_summary: "Started from comment",
    ...overrides,
  };
}

function renderRow(task = makeTask(), issueId = "issue-1") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
  const result = renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <ActiveTaskRow task={task} issueId={issueId} />
    </QueryClientProvider>,
  );
  return { ...result, invalidateSpy };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-06-08T08:05:04Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ActiveTaskRow", () => {
  it("renders running status as elapsed time only", () => {
    renderRow();

    expect(screen.getByText("5m 04s")).toBeInTheDocument();
    expect(screen.queryByText(/events?/i)).not.toBeInTheDocument();
    expect(screen.getByText("Started from comment")).toBeInTheDocument();
    expect(screen.getByText("View transcript")).toBeInTheDocument();
    expect(mockState.taskMessagesOptions).not.toHaveBeenCalled();
  });

  it("does not make transcript actions depend on hover-only rendering", () => {
    renderRow();

    const transcriptButton = screen.getByRole("button", { name: "View transcript" });
    const status = screen.getByText("5m 04s");

    expect(status.parentElement?.className).toContain("flex h-7");
    expect(status.parentElement?.className).toContain(
      "[@media(hover:hover)]:group-hover/execution-log-row:hidden",
    );
    expect(transcriptButton.parentElement?.className).toContain("flex h-7");
    expect(transcriptButton.parentElement?.className).toContain("[@media(hover:hover)]:hidden");
    expect(transcriptButton.parentElement?.className).toContain(
      "[@media(hover:hover)]:group-hover/execution-log-row:flex",
    );
  });

  it("invalidates the issue task list after cancel completes", async () => {
    mockState.cancelTask.mockResolvedValueOnce({
      task: makeTask({ status: "completed", completed_at: "2026-06-08T08:05:00Z" }),
      cancel_state: "already_terminal",
      message: "Task already finished",
    });
    const { invalidateSpy } = renderRow();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Cancel task" }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Confirm cancel" }));
    });

    expect(mockState.cancelTask).toHaveBeenCalledWith("issue-1", "task-1");
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: issueKeys.tasks("issue-1"),
    });
  });
});

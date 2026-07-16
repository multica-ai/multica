// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  taskMessagesOptions: vi.fn(),
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
  TerminateTaskConfirmDialog: () => null,
}));

import { ActiveTaskRow, TaskCommentCoverage } from "./execution-log-section";

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
    renderWithI18n(
      <ActiveTaskRow
        task={makeTask({
          trigger_comment_id: "comment-3",
          coalesced_comment_ids: ["comment-1", "comment-2"],
        })}
        issueId="issue-1"
      />,
    );

    expect(screen.getByText("5m 04s")).toBeInTheDocument();
    expect(screen.queryByText(/events?/i)).not.toBeInTheDocument();
    expect(screen.getByText("Started from comment")).toBeInTheDocument();
    expect(screen.getByText("Includes 3 comments")).toBeInTheDocument();
    expect(screen.getByText("View transcript")).toBeInTheDocument();
    expect(mockState.taskMessagesOptions).not.toHaveBeenCalled();
  });

  it("does not make transcript actions depend on hover-only rendering", () => {
    renderWithI18n(<ActiveTaskRow task={makeTask()} issueId="issue-1" />);

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
});

describe("TaskCommentCoverage", () => {
  it.each<AgentTask["status"]>([
    "queued",
    "dispatched",
    "waiting_local_directory",
    "running",
    "completed",
    "failed",
  ])("shows merged comment coverage for %s tasks", (status) => {
    renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({
          status,
          trigger_comment_id: "comment-3",
          coalesced_comment_ids: ["comment-1", "comment-2"],
          delivered_comment_ids:
            status === "queued"
              ? undefined
              : ["comment-1", "comment-2", "comment-3"],
        })}
      />,
    );

    expect(screen.getByText("Includes 3 comments")).toBeInTheDocument();
  });

  it("uses the unique planned union for queued tasks", () => {
    renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({
          status: "queued",
          trigger_comment_id: "comment-2",
          coalesced_comment_ids: ["comment-1", "comment-2", "comment-1"],
          delivered_comment_ids: ["comment-1"],
        })}
      />,
    );

    expect(screen.getByText("Includes 2 comments")).toBeInTheDocument();
    expect(screen.queryByText("Includes 4 comments")).not.toBeInTheDocument();
  });

  it("prefers the actual delivery receipt after a task is claimed", () => {
    renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({
          trigger_comment_id: "comment-3",
          coalesced_comment_ids: ["comment-1", "comment-2"],
          delivered_comment_ids: ["comment-1", "comment-2", "comment-2"],
        })}
      />,
    );

    expect(screen.getByText("Includes 2 comments")).toBeInTheDocument();
    expect(screen.queryByText("Includes 3 comments")).not.toBeInTheDocument();
  });

  it("falls back to planned coverage for legacy claimed-task rows", () => {
    renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({
          trigger_comment_id: "comment-3",
          coalesced_comment_ids: ["comment-1", "comment-2"],
        })}
      />,
    );

    expect(screen.getByText("Includes 3 comments")).toBeInTheDocument();
  });

  it("treats an explicitly empty delivery receipt as authoritative", () => {
    renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({
          trigger_comment_id: "comment-3",
          coalesced_comment_ids: ["comment-1", "comment-2"],
          delivered_comment_ids: [],
        })}
      />,
    );

    expect(screen.queryByText(/Includes \d+ comments?/)).not.toBeInTheDocument();
  });

  it("stays hidden for one comment but shows a cancelled task receipt", () => {
    const { rerender } = renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({ trigger_comment_id: "comment-1" })}
      />,
    );
    expect(screen.queryByText(/Includes \d+ comments?/)).not.toBeInTheDocument();

    rerender(
      <TaskCommentCoverage
        task={makeTask({
          status: "cancelled",
          trigger_comment_id: "comment-2",
          coalesced_comment_ids: ["comment-1"],
          delivered_comment_ids: ["comment-1", "comment-2"],
        })}
      />,
    );
    expect(screen.getByText("Includes 2 comments")).toBeInTheDocument();
  });

  it("renders the Chinese comment count", () => {
    renderWithI18n(
      <TaskCommentCoverage
        task={makeTask({
          trigger_comment_id: "comment-3",
          coalesced_comment_ids: ["comment-1", "comment-2"],
        })}
      />,
      { locale: "zh-Hans" },
    );

    expect(screen.getByText("包含 3 条评论")).toBeInTheDocument();
  });
});

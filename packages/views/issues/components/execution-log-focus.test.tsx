// @vitest-environment jsdom

import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { issueKeys } from "@multica/core/issues/queries";
import { renderWithI18n } from "../../test/i18n";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: () => <button type="button">Transcript</button>,
}));

vi.mock("./terminate-task-confirm-dialog", () => ({
  TerminateTaskConfirmDialog: () => null,
}));

import {
  ExecutionLogSection,
  focusIssueExecutionTask,
} from "./execution-log-section";

const completedTask: AgentTask = {
  id: "task-source",
  agent_id: "agent-1",
  runtime_id: "runtime-1",
  issue_id: "issue-1",
  status: "completed",
  priority: 0,
  dispatched_at: "2026-08-16T00:00:00Z",
  started_at: "2026-08-16T00:00:01Z",
  completed_at: "2026-08-16T00:01:00Z",
  result: null,
  error: null,
  created_at: "2026-08-16T00:00:00Z",
  trigger_summary: "Source execution",
};

describe("comment source execution focus", () => {
  const scrollIntoView = vi.fn();

  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });
    scrollIntoView.mockClear();
  });

  afterEach(() => cleanup());

  it("expands terminal runs and scrolls to the source task", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(issueKeys.tasks("issue-1"), [completedTask]);
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ExecutionLogSection issueId="issue-1" />
      </QueryClientProvider>,
    );

    expect(document.getElementById("execution-log-task-task-source")).toBeNull();
    act(() => focusIssueExecutionTask("issue-1", "task-source"));

    await waitFor(() => {
      expect(document.getElementById("execution-log-task-task-source")).not.toBeNull();
      expect(scrollIntoView).toHaveBeenCalledWith({
        behavior: "smooth",
        block: "nearest",
      });
    });
  });

  it("delegates every branch marker click for an offscreen comment", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(issueKeys.tasks("issue-1"), [
      {
        ...completedTask,
        status: "running",
        completed_at: null,
        branch_point_comment_id: "comment-offscreen",
      },
    ]);
    const onCommentFocus = vi.fn();
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ExecutionLogSection
          issueId="issue-1"
          onCommentFocus={onCommentFocus}
        />
      </QueryClientProvider>,
    );

    const marker = screen.getByRole("button", {
      name: "Run started from a comment",
    });
    fireEvent.click(marker);
    fireEvent.click(marker);

    expect(onCommentFocus).toHaveBeenCalledTimes(2);
    expect(onCommentFocus).toHaveBeenNthCalledWith(1, "comment-offscreen");
    expect(onCommentFocus).toHaveBeenNthCalledWith(2, "comment-offscreen");
  });

  it("does not offer a dead source link after the comment is deleted", () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(issueKeys.tasks("issue-1"), [
      {
        ...completedTask,
        status: "running",
        completed_at: null,
        branch_point_comment_id: "comment-deleted",
      },
    ]);
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ExecutionLogSection
          issueId="issue-1"
          availableCommentIds={new Set()}
          onCommentFocus={vi.fn()}
        />
      </QueryClientProvider>,
    );

    expect(
      screen.queryByRole("button", {
        name: "Started from a comment branch",
      }),
    ).toBeNull();
  });
});

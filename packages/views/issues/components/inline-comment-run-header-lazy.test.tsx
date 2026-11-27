// @vitest-environment jsdom

import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTaskMessages } from "@multica/core/chat/queries";
import { useCancelIssueRun, useRetryIssueRun } from "@multica/core/issues/mutations";
import type { AgentTask } from "@multica/core/types";
import { useTraceIssueLabels } from "../../common/task-transcript/use-trace-issue-labels";
import { renderWithI18n } from "../../test/i18n";
import { InlineCommentRun } from "./inline-comment-run";
import { useRunAnimationVisibility, useRunDisclosureMotion } from "./use-run-comment-motion";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: vi.fn(actual.useQuery) };
});
vi.mock("@multica/core/chat/queries", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/chat/queries")>();
  return { ...actual, useTaskMessages: vi.fn() };
});
vi.mock("@multica/core/issues/mutations", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/issues/mutations")>();
  return { ...actual, useCancelIssueRun: vi.fn(), useRetryIssueRun: vi.fn() };
});
vi.mock("@multica/core/api", () => ({ api: {}, dispatchReasonCode: vi.fn() }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("@multica/core/workspace/hooks", () => ({ useActorName: () => ({ getActorName: () => "Reviewer" }) }));
vi.mock("../../common/task-transcript/use-trace-issue-labels", () => ({ useTraceIssueLabels: vi.fn() }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../editor", () => ({ ReadonlyContent: () => <div /> }));
vi.mock("../../common/task-transcript/agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({ contentState }: { contentState?: ReactNode }) => <div role="dialog">{contentState ?? "Full transcript"}</div>,
  StepBody: () => null,
}));
vi.mock("./use-run-comment-motion", () => ({
  useRunAnimationVisibility: vi.fn(),
  useRunDisclosureMotion: vi.fn(),
}));

const task: AgentTask = {
  id: "task-1", agent_id: "agent", runtime_id: "runtime", issue_id: "issue", status: "completed", priority: 0,
  created_at: "2026-09-07T00:00:00Z", started_at: "2026-09-07T00:00:00Z", dispatched_at: null,
  completed_at: "2026-09-07T00:01:00Z", result: null, error: null,
};

beforeEach(() => {
  vi.mocked(useTaskMessages).mockReturnValue({ data: [], isPending: false, isError: false, refetch: vi.fn() } as unknown as ReturnType<typeof useTaskMessages>);
  vi.mocked(useTraceIssueLabels).mockReturnValue((text: string) => text);
  const mutation = { isPending: false, isSuccess: false, mutate: vi.fn() };
  vi.mocked(useCancelIssueRun).mockReturnValue(mutation as unknown as ReturnType<typeof useCancelIssueRun>);
  vi.mocked(useRetryIssueRun).mockReturnValue(mutation as unknown as ReturnType<typeof useRetryIssueRun>);
  vi.mocked(useRunAnimationVisibility).mockReturnValue({ ref: vi.fn(), visible: false });
  vi.mocked(useRunDisclosureMotion).mockReturnValue({ chevronRef: { current: null }, contentRef: { current: null }, onTrigger: vi.fn() } as ReturnType<typeof useRunDisclosureMotion>);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("InlineCommentRun header lazy loading", () => {
  it("mounts no run queries or heavy hooks until the full log is opened", () => {
    const client = new QueryClient();
    renderWithI18n(
      <QueryClientProvider client={client}>
        <InlineCommentRun run={{ task, commentId: "comment", hasReply: true }} presentation="header" />
      </QueryClientProvider>,
    );

    expect(screen.getByRole("button", { name: "Open full log" })).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(useQuery).not.toHaveBeenCalled();
    expect(useTaskMessages).not.toHaveBeenCalled();
    expect(useTraceIssueLabels).not.toHaveBeenCalled();
    expect(useCancelIssueRun).not.toHaveBeenCalled();
    expect(useRetryIssueRun).not.toHaveBeenCalled();
    expect(useRunAnimationVisibility).not.toHaveBeenCalled();
    expect(useRunDisclosureMotion).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Open full log" }));

    expect(screen.getByRole("dialog")).toHaveTextContent("Full transcript");
    expect(useTaskMessages).toHaveBeenCalledWith("task-1", false, true);
    expect(useQuery).not.toHaveBeenCalled();
    expect(useTraceIssueLabels).not.toHaveBeenCalled();
    expect(useCancelIssueRun).not.toHaveBeenCalled();
    expect(useRetryIssueRun).not.toHaveBeenCalled();
    expect(useRunAnimationVisibility).not.toHaveBeenCalled();
    expect(useRunDisclosureMotion).not.toHaveBeenCalled();
  });
});

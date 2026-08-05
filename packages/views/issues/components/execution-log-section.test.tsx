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

import { ActiveTaskRow, TaskCommentCoverage, IssueUsageTotal } from "./execution-log-section";
import type { TaskUsage } from "@multica/core/types";
import { act } from "@testing-library/react";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";

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

// claude-opus-5 at 5 / 25 / 0.50 / 6.25 per million.
function usageSlice(overrides: Partial<TaskUsage> = {}): TaskUsage {
  return {
    provider: "anthropic",
    model: "claude-opus-5",
    input_tokens: 96_000,
    output_tokens: 34_000,
    cache_read_tokens: 712_000,
    cache_write_tokens: 50_000,
    ...overrides,
  };
}

describe("per-run token usage", () => {
  // An active row shows only its timer. The daemon reports usage once, after
  // the run returns, and that write publishes no realtime event — so no
  // running task carries usage in production. Asserting a token figure here
  // would only prove that a hand-written fixture renders.
  it("shows a running row's timer, and no token figure even if usage exists", () => {
    renderWithI18n(
      <ActiveTaskRow
        task={makeTask({ usage: [usageSlice()] })}
        issueId="issue-1"
      />,
    );

    expect(screen.getByText("5m 04s")).toBeInTheDocument();
    expect(screen.queryByText("892K")).not.toBeInTheDocument();
    // And no em dash either — mid-run, "no figure yet" is not a claim worth
    // making next to a ticking timer.
    expect(screen.queryByText("—")).not.toBeInTheDocument();
  });
});

describe("IssueUsageTotal pricing", () => {
  afterEach(() => {
    useCustomPricingStore.setState({ pricings: {} });
  });

  it("recomputes when a custom model rate is saved", () => {
    // `estimateCost` reads the custom-rate store imperatively, so nothing
    // re-renders this on a rate change unless the component subscribes. Before
    // that subscription existed the figure stayed stale until the task list
    // happened to refetch.
    const unpriced: TaskUsage = {
      provider: "acme",
      model: "totally-made-up-model",
      input_tokens: 1_000_000,
      output_tokens: 0,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
    };
    const task = makeTask({ status: "completed", usage: [unpriced] });

    renderWithI18n(
      <IssueUsageTotal tasks={[task]} alone onOpen={() => {}} />,
    );

    // No rate on file for this model yet.
    expect(screen.getByText("$0.00")).toBeInTheDocument();

    act(() => {
      useCustomPricingStore.getState().setCustomPricing("acme/totally-made-up-model", {
        input: 7,
        output: 0,
        cacheRead: 0,
        cacheWrite: 0,
      });
    });

    // 1M input tokens at $7/M, without any refetch.
    expect(screen.getByText("$7.00")).toBeInTheDocument();
  });
});

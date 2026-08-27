// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
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

import {
  ActiveTaskRow,
  ExecutionLogSection,
  TaskCommentCoverage,
  IssueUsageTotal,
} from "./execution-log-section";
import type { TaskUsage } from "@multica/core/types";
import { act, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { issueKeys } from "@multica/core/issues/queries";
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

describe("execution log failure reasons", () => {
  function failedLogClient() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(issueKeys.tasks("issue-1"), [
      makeTask({
        status: "failed",
        completed_at: "2026-06-08T08:04:00Z",
        error: "provider returned 402",
        failure_reason: "agent_error.provider_quota_limit",
      }),
    ]);
    return queryClient;
  }

  it("renders a failed run's reason in the active locale", () => {
    renderWithI18n(
      <QueryClientProvider client={failedLogClient()}>
        <ExecutionLogSection issueId="issue-1" />
      </QueryClientProvider>,
      { locale: "zh-Hans" },
    );

    fireEvent.click(screen.getByRole("button", { name: "显示历史运行（1）" }));
    expect(screen.getByText(/提供商配额已用尽/)).toBeInTheDocument();
    expect(
      screen.queryByText(/Provider quota exhausted/),
    ).not.toBeInTheDocument();
  });

  // #7411: the raw `task.error` is English prose the server writes for logs
  // and classification. It used to be concatenated into the status tooltip,
  // which put untranslated text — and absolute worktree paths — in front of
  // every non-English workspace. The localized reason is the whole hover text
  // now; the raw diagnostic lives in the transcript's Run details.
  it("keeps the raw server error out of the status tooltip", () => {
    renderWithI18n(
      <QueryClientProvider client={failedLogClient()}>
        <ExecutionLogSection issueId="issue-1" />
      </QueryClientProvider>,
      { locale: "zh-Hans" },
    );

    fireEvent.click(screen.getByRole("button", { name: "显示历史运行（1）" }));
    expect(screen.queryByTitle(/provider returned 402/)).not.toBeInTheDocument();
    expect(screen.getByTitle("提供商配额已用尽")).toBeInTheDocument();
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

// The sidebar this section lives in is a resizable panel — 260px minimum,
// 320px default, 420px maximum — so the header's three items (section label,
// active-run count, issue total) have to hold a width the component does not
// choose. They stopped holding it once the total moved into the header: at the
// 260px minimum the row has 227px and the full header wants ~238px, and the
// label was the only item that could give. It gave by breaking "Execution log"
// across two lines (MUL-5804). These tests pin the contract that replaced that:
// one line always, and a width tier that drops the token figure whole.
describe("execution log header geometry", () => {
  function renderSection(tasks: AgentTask[]) {
    // Seed the cache instead of mocking the API: the query is fresh for 30s,
    // so `listTasksByIssue` is never reached and the section renders its real
    // header markup.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(issueKeys.tasks("issue-1"), tasks);
    return renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ExecutionLogSection issueId="issue-1" identifier="MUL-1" />
      </QueryClientProvider>,
    );
  }

  function headerOf(): HTMLElement {
    const label = screen.getByText("Execution log");
    const header = label.closest("div");
    if (!header) throw new Error("header row not found");
    return header;
  }

  const completed = makeTask({
    status: "completed",
    completed_at: "2026-06-08T08:04:00Z",
    usage: [usageSlice()],
  });

  it("keeps the section label on one line", () => {
    renderSection([completed]);

    const label = screen.getByText("Execution log");
    // The label is the only header item allowed to shrink, so it is the one
    // that must carry nowrap + ellipsis. A heading that reflows mid-phrase
    // reads as broken; an ellipsis reads as a narrow column.
    expect(label.className).toContain("truncate");
    expect(label.closest("button")?.className).toContain("whitespace-nowrap");
  });

  it("tiers on the sidebar's width, not the viewport's", () => {
    const { container } = renderSection([completed]);

    // Two sidebars of different widths can be open in one window (desktop
    // split panes, the mobile sheet), so a `lg:` variant would tier this
    // header on a width that has nothing to do with the panel it sits in.
    const root = container.firstElementChild as HTMLElement;
    expect(root.className).toContain("@container/execution-log");
    for (const el of headerOf().querySelectorAll("*")) {
      // classList, not className: the chevron is an SVG, whose className is an
      // SVGAnimatedString.
      for (const cls of el.classList) {
        expect(cls).not.toMatch(/^(sm|md|lg|xl|2xl):/);
      }
    }
  });

  it("drops the token figure whole rather than clipping a number", () => {
    const { unmount } = renderSection([completed]);
    let header = within(headerOf());

    // Below the tier the tokens and their separator leave together and the
    // cost stays — never a clipped "$2.0…", which would read as a different
    // figure than the issue actually spent.
    const cost = header.getByText("$2.00");
    expect(header.getByText("892K").className).toContain(
      "@max-[14rem]/execution-log:hidden",
    );
    expect(header.getByText("·").className).toContain(
      "@max-[14rem]/execution-log:hidden",
    );
    expect(cost.className).not.toContain("hidden");
    // And the pill itself never truncates — that is what would clip a digit.
    const pill = cost.closest("button");
    expect(pill?.className).toContain("shrink-0");
    expect(pill?.className).not.toContain("truncate");

    // An active run puts the count chip in the same row, which is the shape
    // that actually runs out of width — so it tiers earlier. At rest the total
    // fits the 260px minimum whole and should not be tiered away with it.
    unmount();
    renderSection([completed, makeTask({ status: "running" })]);
    header = within(headerOf());
    expect(header.getByText("892K").className).toContain(
      "@max-[16rem]/execution-log:hidden",
    );
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

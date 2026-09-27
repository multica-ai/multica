// @vitest-environment jsdom

import { act, cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, TaskUsage } from "@multica/core/types";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { renderWithI18n } from "../../test/i18n";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: ({ title }: { title?: string }) => (
    <button type="button">{title ?? "Transcript"}</button>
  ),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lambda" }),
}));

// The real label loads the issue's wakeup rules; the row only needs to use it.
vi.mock("./wakeup-source-chip", () => ({
  WakeupRunLabel: ({ render }: { render: (label: string) => React.ReactNode }) =>
    render("Wakeup · When CI finishes"),
}));

import { IssueRunsDialog } from "./issue-runs-dialog";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-09-27T09:00:00",
    completed_at: "2026-09-27T09:30:00",
    result: null,
    error: null,
    created_at: "2026-09-27T08:59:00",
    trigger_summary: "Initial run",
    ...overrides,
  };
}

// claude-opus-5 at 5 / 25 / 0.50 / 6.25 per million: 1M output = $25.
function usage(overrides: Partial<TaskUsage> = {}): TaskUsage {
  return {
    provider: "anthropic",
    model: "claude-opus-5",
    input_tokens: 0,
    output_tokens: 1_000_000,
    cache_read_tokens: 0,
    cache_write_tokens: 0,
    ...overrides,
  };
}

function open(tasks: AgentTask[], locale?: "zh-Hans") {
  return renderWithI18n(
    <IssueRunsDialog
      open
      onOpenChange={() => {}}
      issueId="issue-1"
      identifier="MUL-7680"
      issueTitle="Issue wakeups v2"
      tasks={tasks}
    />,
    { locale },
  );
}

beforeEach(() => {
  cleanup();
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-09-27T18:00:00"));
});

afterEach(() => {
  vi.useRealTimers();
  useCustomPricingStore.setState({ pricings: {} });
  cleanup();
});

describe("IssueRunsDialog", () => {
  it("names the issue and sums what the runs spent and took", () => {
    open([
      makeTask({ id: "a", usage: [usage()] }),
      makeTask({
        id: "b",
        status: "failed",
        started_at: "2026-09-26T10:00:00",
        completed_at: "2026-09-26T10:15:00",
        usage: [usage({ output_tokens: 200_000 })],
      }),
    ]);

    expect(screen.getByRole("heading", { name: "Runs" })).toBeInTheDocument();
    expect(screen.getByText("MUL-7680 · Issue wakeups v2")).toBeInTheDocument();
    // $25 + $5, 30m + 15m of agent time, first start to last end.
    const stat = (label: string) => screen.getByText(label).parentElement!;
    expect(stat("Spent")).toHaveTextContent("$30.00");
    expect(stat("Agent time")).toHaveTextContent("45m");
    expect(stat("Elapsed")).toHaveTextContent("23h 30m");
    expect(screen.getByText(/1 failed/)).toBeInTheDocument();
    expect(
      screen.getByRole("img", { name: "Cumulative cost over time: $30.00 across 2 runs" }),
    ).toBeInTheDocument();
  });

  it("groups runs by day, newest first, with each day's total", () => {
    open([
      makeTask({ id: "old", trigger_summary: "Older run", started_at: "2026-09-26T10:00:00", completed_at: "2026-09-26T10:15:00", usage: [usage({ output_tokens: 200_000 })] }),
      makeTask({ id: "new", trigger_summary: "Newer run", usage: [usage()] }),
    ]);

    const today = screen.getByRole("region", { name: "Today" });
    const yesterday = screen.getByRole("region", { name: "Yesterday" });
    expect(within(today).getByText("Newer run")).toBeInTheDocument();
    expect(within(today).getByText("1 run · 30m · $25.00")).toBeInTheDocument();
    expect(within(yesterday).getByText("Older run")).toBeInTheDocument();
    expect(
      today.compareDocumentPosition(yesterday) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("quotes what a person said and cleans the snapshot's markdown", () => {
    open([
      makeTask({
        trigger_comment_id: "comment-1",
        trigger_summary: "@Lambda fix conflicts &amp; ci",
        usage: [usage()],
      }),
    ]);

    expect(screen.getByText("“@Lambda fix conflicts & ci”")).toBeInTheDocument();
  });

  it("shows no figure — not $0 — for a run without usage", () => {
    open([
      makeTask({ id: "priced", usage: [usage()] }),
      makeTask({ id: "unpriced", status: "cancelled", started_at: "2026-09-27T10:00:00", completed_at: "2026-09-27T10:00:05" }),
    ]);

    expect(screen.getByTitle("No usage recorded")).toHaveTextContent("—");
    expect(screen.queryByText("$0.00")).not.toBeInTheDocument();
    expect(screen.getByText("Cancelled")).toBeInTheDocument();
  });

  it("gives every terminal run a status a screen reader can read", () => {
    // Completed rows show their duration; the status word must still be there.
    open([
      makeTask({ id: "ok", usage: [usage()] }),
      makeTask({ id: "bad", status: "failed", started_at: "2026-09-27T11:00:00", completed_at: "2026-09-27T11:05:00", usage: [usage()] }),
    ]);

    expect(screen.getByText("Completed")).toHaveClass("sr-only");
    expect(screen.getByText("Failed")).toBeInTheDocument();
  });

  it("flags models with no price on file", () => {
    open([makeTask({ usage: [usage({ provider: "acme", model: "made-up-model" })] })]);

    expect(screen.getByText(/No price on file for acme\/made-up-model/)).toBeInTheDocument();
  });

  it("re-prices when a custom model rate is saved", () => {
    // `estimateCost` reads the custom-rate store imperatively, so nothing
    // re-renders the dialog on a rate change unless it subscribes.
    open([
      makeTask({
        usage: [usage({ provider: "acme", model: "made-up-model", output_tokens: 0, input_tokens: 1_000_000 })],
      }),
    ]);
    expect(screen.queryByText("$7.00")).not.toBeInTheDocument();

    act(() => {
      useCustomPricingStore.getState().setCustomPricing("acme/made-up-model", {
        input: 7,
        output: 0,
        cacheRead: 0,
        cacheWrite: 0,
      });
    });

    expect(screen.getAllByText("$7.00").length).toBeGreaterThan(0);
  });

  it("answers a pointer anywhere on the chart with the nearest run", () => {
    open([
      makeTask({ id: "early", trigger_summary: "Early run", started_at: "2026-09-27T09:00:00", completed_at: "2026-09-27T09:30:00", usage: [usage()] }),
      makeTask({ id: "late", trigger_summary: "Late run", started_at: "2026-09-27T15:00:00", completed_at: "2026-09-27T15:10:00", usage: [usage({ output_tokens: 200_000 })] }),
    ]);
    // The plot column sits beside the lane labels; give it a width, which
    // jsdom does not lay out.
    const plot = screen.getByRole("img").children[1] as HTMLElement;
    plot.getBoundingClientRect = () => ({ left: 0, width: 1000, top: 0, height: 200, right: 1000, bottom: 200, x: 0, y: 0, toJSON: () => ({}) });

    // Far right, past the last bar: snaps to the last run.
    fireEvent.pointerMove(plot, { clientX: 995 });
    expect(within(plot).getByText("Late run")).toBeInTheDocument();
    expect(within(plot).getByText(/Total so far \$30\.00/)).toBeInTheDocument();

    // Far left: the first run, with the curve's reading there.
    fireEvent.pointerMove(plot, { clientX: 5 });
    expect(within(plot).getByText("Early run")).toBeInTheDocument();
    expect(within(plot).getByText(/Total so far \$25\.00/)).toBeInTheDocument();

    // Leaving hands the plot back to the peak label.
    fireEvent.pointerLeave(plot);
    expect(within(plot).queryByText(/Total so far/)).not.toBeInTheDocument();
  });

  it("picks the run on the lane under the pointer when runs overlap", () => {
    // Review repro: a short Emacs run inside a long Lambda run.
    open([
      makeTask({ id: "long", agent_id: "agent-lambda", trigger_summary: "Long run", started_at: "2026-09-27T09:00:00", completed_at: "2026-09-27T12:00:00", usage: [usage()] }),
      makeTask({ id: "short", agent_id: "agent-emacs", trigger_summary: "Short run", started_at: "2026-09-27T10:00:00", completed_at: "2026-09-27T11:00:00", usage: [usage({ output_tokens: 200_000 })] }),
    ]);
    const plot = screen.getByRole("img").children[1] as HTMLElement;
    plot.getBoundingClientRect = () => ({ left: 0, width: 1000, top: 0, height: 200, right: 1000, bottom: 200, x: 0, y: 0, toJSON: () => ({}) });
    const lane = (agentId: string) => plot.querySelector(`[data-lane="${agentId}"]`) as HTMLElement;
    // The x of 10:30 on the chart's padded domain.
    const [d0, d1] = [new Date("2026-09-27T09:00:00").getTime(), new Date("2026-09-27T12:00:00").getTime()];
    const pad = (d1 - d0) * 0.02;
    const x = ((new Date("2026-09-27T10:30:00").getTime() - (d0 - pad)) / (d1 - d0 + 2 * pad)) * 1000;

    fireEvent.pointerMove(lane("agent-lambda"), { clientX: x });
    expect(within(plot).getByText("Long run")).toBeInTheDocument();

    fireEvent.pointerMove(lane("agent-emacs"), { clientX: x });
    expect(within(plot).getByText("Short run")).toBeInTheDocument();
  });

  it("opens a run's cost split from a real button, not hover alone", () => {
    open([makeTask({ usage: [usage()] })]);

    // Focusable (keyboard) and clickable (touch); the review found a span.
    const button = screen.getByRole("button", { name: "Cost breakdown: $25.00" });
    expect(button).not.toHaveAttribute("tabindex", "-1");
    expect(screen.queryByText("claude-opus-5")).not.toBeInTheDocument();

    fireEvent.click(button);
    expect(screen.getByText("claude-opus-5")).toBeInTheDocument();
    expect(screen.getByText("1M")).toBeInTheDocument();
  });

  it("names a wakeup run by its rule, not as a quote", () => {
    // A wakeup run carries the thread's comment id and the rule's instruction
    // as its summary; neither is something a person said.
    open([
      makeTask({
        wakeup_id: "wakeup-1",
        trigger_comment_id: "comment-1",
        trigger_summary: "Wakeup: check CI and fix it",
        usage: [usage()],
      }),
    ]);

    expect(screen.getByTitle("Wakeup · When CI finishes")).toHaveTextContent("Wakeup · When CI finishes");
    expect(screen.queryByText(/“/)).not.toBeInTheDocument();
  });

  it("keeps the whole trigger reachable when one line truncates it", () => {
    const long = "把之前拆出去的三个子任务都合并回这个 PR，conditions / history / runaway protection 一起做完再提交";
    open([makeTask({ trigger_comment_id: "comment-1", trigger_summary: long, usage: [usage()] })]);

    expect(screen.getByText(`“${long}”`)).toHaveAttribute("title", `“${long}”`);
  });

  it("renders in the member's language", () => {
    open([makeTask({ usage: [usage()] })], "zh-Hans");

    expect(screen.getByRole("heading", { name: "运行记录" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "今天" })).toBeInTheDocument();
  });
});

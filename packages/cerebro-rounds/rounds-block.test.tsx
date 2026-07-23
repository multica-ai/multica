// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IssueRoundsSectionView, RoundsBlock } from "./rounds-block";
import type { RoundStatus } from "./schemas";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const status = (handled = false, active = true): RoundStatus => ({
  round: { id: "round-1", workspace_id: "ws", owner_id: "owner", name: "Daily", created_at: "", updated_at: "" },
  members: ["ready", "running", "wakeup", "handled", "orphan"].map((issue_id) => ({
    round_id: "round-1", issue_id, added_by_type: "member", added_by_id: "owner", created_at: "",
  })),
  active_cycle: active ? {
    id: "cycle-1", round_id: "round-1", started_at: "2026-07-14T12:00:00Z",
    items: [
      { issue_id: "ready", handled_at: null },
      { issue_id: "running", handled_at: null },
      { issue_id: "wakeup", handled_at: null },
      { issue_id: "handled", handled_at: handled ? "2026-07-14T12:01:00Z" : null },
    ],
  } : null,
});

const props = {
  issueTitles: { ready: "Ready message", running: "Running message", wakeup: "Wakeup message", handled: "Handled message", orphan: "Orphan issue" },
  messageIssueIds: ["handled", "ready", "running", "wakeup"],
  issueRunStates: new Map([["running", "running"]]),
  wakeupIssueIds: new Set(["wakeup"]),
  onStart: vi.fn(),
  onPause: vi.fn(),
  onSelectIssue: vi.fn(),
  renderIssue: (issueId: string) => issueId === "orphan" ? null : <div data-testid={`row-${issueId}`}>{issueId}</div>,
};

describe("RoundsBlock", () => {
  it("defaults to Ready and hides active runs and scheduled unread messages", () => {
    render(<RoundsBlock statuses={[status(true)]} {...props} />);
    expect(screen.getByText("1 ready")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    expect(screen.getByRole("button", { name: "Ready" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("row-ready")).toBeInTheDocument();
    expect(screen.queryByTestId("row-running")).not.toBeInTheDocument();
    expect(screen.queryByTestId("row-wakeup")).not.toBeInTheDocument();
    expect(screen.queryByTestId("row-handled")).not.toBeInTheDocument();
  });

  it("moves a reply from Ready to Handled this round without removing it from All messages", () => {
    const { rerender } = render(<RoundsBlock statuses={[status(false)]} {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    expect(screen.getByTestId("row-handled")).toBeInTheDocument();

    rerender(<RoundsBlock statuses={[status(true)]} {...props} />);
    expect(screen.queryByTestId("row-handled")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Handled this round" }));
    expect(screen.getByTestId("row-handled")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "All messages" }));
    expect(screen.getByTestId("row-handled")).toBeInTheDocument();
    expect(screen.getByTestId("row-running")).toBeInTheDocument();
    expect(screen.getByTestId("row-wakeup")).toBeInTheDocument();
    expect(screen.getAllByTestId(/^row-/).map((row) => row.textContent)).toEqual(["handled", "ready", "running", "wakeup"]);
    expect(screen.queryByRole("button", { name: "Orphan issue" })).not.toBeInTheDocument();
  });

  it("searches the selected flat view and Play starts a fresh snapshot", () => {
    const onStart = vi.fn();
    const { rerender } = render(<RoundsBlock statuses={[status(true)]} {...props} onStart={onStart} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    fireEvent.click(screen.getByRole("button", { name: "All messages" }));
    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), { target: { value: "handled" } });
    expect(screen.getByTestId("row-handled")).toBeInTheDocument();
    expect(screen.queryByTestId("row-ready")).not.toBeInTheDocument();
    rerender(<RoundsBlock statuses={[status(false, false)]} {...props} onStart={onStart} />);
    fireEvent.click(screen.getByRole("button", { name: "Play Daily" }));
    expect(onStart).toHaveBeenCalledWith("round-1");
  });

  it("automatically expands every round with matching messages and searches across all views (FIR-3440)", () => {
    const weekly = status(true);
    weekly.round = { ...weekly.round, id: "round-2", name: "Weekly" };
    weekly.members = [{ ...weekly.members[0]!, round_id: "round-2", issue_id: "weekly" }];
    weekly.active_cycle = {
      ...weekly.active_cycle!,
      id: "cycle-2",
      round_id: "round-2",
      items: [{ issue_id: "weekly", handled_at: null }],
    };

    render(
      <RoundsBlock
        statuses={[status(true), weekly]}
        {...props}
        issueTitles={{
          ...props.issueTitles,
          ready: "Returns ready",
          handled: "Returns handled",
          running: "Pricing running",
          wakeup: "Pricing wakeup",
          weekly: "Returns weekly",
        }}
        messageIssueIds={["handled", "ready", "running", "wakeup", "weekly"]}
        renderIssue={(issueId) => <div data-testid={`row-${issueId}`}>{issueId}</div>}
      />,
    );

    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), {
      target: { value: "returns" },
    });

    expect(screen.getByRole("button", { name: "Collapse Daily" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Collapse Weekly" })).toBeInTheDocument();
    expect(screen.getByTestId("row-ready")).toBeInTheDocument();
    expect(screen.getByTestId("row-handled")).toBeInTheDocument();
    expect(screen.getByTestId("row-weekly")).toBeInTheDocument();
    expect(screen.queryByTestId("row-running")).not.toBeInTheDocument();
    expect(screen.queryByTestId("row-wakeup")).not.toBeInTheDocument();
  });

  it("does not expand a round when only an issue without an Inbox message matches", () => {
    render(<RoundsBlock statuses={[status(true)]} {...props} />);

    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), {
      target: { value: "orphan" },
    });

    expect(screen.getByRole("button", { name: "Expand Daily" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Orphan issue" })).not.toBeInTheDocument();
  });

  it("offers Pause only for an active snapshot and folds it away", () => {
    const onPause = vi.fn();
    const { rerender } = render(<RoundsBlock statuses={[status()]} {...props} onPause={onPause} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    fireEvent.click(screen.getByRole("button", { name: "Pause Daily" }));
    expect(onPause).toHaveBeenCalledWith("round-1");
    expect(screen.getByRole("button", { name: "Expand Daily" })).toBeInTheDocument();

    rerender(<RoundsBlock statuses={[status(false, false)]} {...props} onPause={onPause} />);
    expect(screen.queryByRole("button", { name: "Pause Daily" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Play Daily" })).toBeInTheDocument();
  });

  it("keeps green Pause available on a completed Round so it can be closed immediately", () => {
    const onPause = vi.fn();
    const completed = status(true);
    completed.active_cycle!.items = completed.active_cycle!.items.map((item) => ({
      ...item,
      handled_at: "2026-07-14T12:01:00Z",
    }));

    render(<RoundsBlock statuses={[completed]} {...props} onPause={onPause} />);

    expect(screen.getByText("Complete")).toBeInTheDocument();
    const pause = screen.getByRole("button", { name: "Pause Daily" });
    expect(pause).toHaveClass("bg-success");

    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    expect(screen.getByRole("alert")).toHaveTextContent("All ready messages handled.");

    fireEvent.click(pause);
    expect(onPause).toHaveBeenCalledWith("round-1");
  });

  it("automatically closes a completed Round after one minute", () => {
    vi.useFakeTimers();
    const onPause = vi.fn();
    const completed = status(true);
    completed.active_cycle!.items = completed.active_cycle!.items.map((item) => ({
      ...item,
      handled_at: "2026-07-14T12:01:00Z",
    }));

    render(<RoundsBlock statuses={[completed]} {...props} onPause={onPause} />);

    act(() => vi.advanceTimersByTime(59_999));
    expect(onPause).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(1));
    expect(onPause).toHaveBeenCalledWith("round-1");
  });
});

describe("IssueRoundsSectionView", () => {
  it("renders a collapsible Rounds section", () => {
    render(
      <IssueRoundsSectionView>
        <button type="button">Add to round</button>
      </IssueRoundsSectionView>,
    );

    expect(screen.getByText("Rounds")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add to round" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Collapse Rounds" }));
    expect(screen.queryByRole("button", { name: "Add to round" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand Rounds" }));
    expect(screen.getByRole("button", { name: "Add to round" })).toBeInTheDocument();
  });
});

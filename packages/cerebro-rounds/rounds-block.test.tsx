// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RoundsBlock } from "./rounds-block";
import type { RoundStatus } from "./schemas";

afterEach(cleanup);

const status = (handled = false): RoundStatus => ({
  round: { id: "round-1", workspace_id: "ws", owner_id: "owner", name: "Daily", created_at: "", updated_at: "" },
  members: ["ready", "running", "wakeup", "handled", "orphan"].map((issue_id) => ({
    round_id: "round-1", issue_id, added_by_type: "member", added_by_id: "owner", created_at: "",
  })),
  active_cycle: {
    id: "cycle-1", round_id: "round-1", started_at: "2026-07-14T12:00:00Z",
    items: [
      { issue_id: "ready", handled_at: null },
      { issue_id: "running", handled_at: null },
      { issue_id: "wakeup", handled_at: null },
      { issue_id: "handled", handled_at: handled ? "2026-07-14T12:01:00Z" : null },
    ],
  },
});

const props = {
  issueTitles: { ready: "Ready message", running: "Running message", wakeup: "Wakeup message", handled: "Handled message", orphan: "Orphan issue" },
  messageIssueIds: ["handled", "ready", "running", "wakeup"],
  issueRunStates: new Map([["running", "running"]]),
  wakeupIssueIds: new Set(["wakeup"]),
  onStart: vi.fn(),
  onSelectIssue: vi.fn(),
  renderIssue: (issueId: string) => issueId === "orphan" ? null : <div data-testid={`row-${issueId}`}>{issueId}</div>,
};

describe("RoundsBlock", () => {
  it("defaults to Ready and hides active runs without hiding scheduled unread messages", () => {
    render(<RoundsBlock statuses={[status(true)]} {...props} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    expect(screen.getByRole("button", { name: "Ready" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("row-ready")).toBeInTheDocument();
    expect(screen.queryByTestId("row-running")).not.toBeInTheDocument();
    expect(screen.getByTestId("row-wakeup")).toBeInTheDocument();
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
    render(<RoundsBlock statuses={[status(true)]} {...props} onStart={onStart} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily" }));
    fireEvent.click(screen.getByRole("button", { name: "All messages" }));
    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), { target: { value: "handled" } });
    expect(screen.getByTestId("row-handled")).toBeInTheDocument();
    expect(screen.queryByTestId("row-ready")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Play Daily" }));
    expect(onStart).toHaveBeenCalledWith("round-1");
  });
});

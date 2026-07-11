// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RoundManager, RoundsBlock } from "./rounds-block";
import type { RoundStatus } from "./schemas";

const status = (overrides: Partial<RoundStatus> = {}): RoundStatus => ({
  round: {
    id: "round-1", workspace_id: "ws", owner_id: "owner", name: "Daily ideas",
    mode: "batch",
    schedule_cron: "0 9 * * *", timezone: "Europe/Copenhagen", next_run_at: "2026-07-11T07:00:00Z",
    created_at: "", updated_at: "",
  },
  active_run: null,
  members: [{ round_id: "round-1", issue_id: "issue-1", added_by_type: "member", added_by_id: "owner", held_trigger_count: 2, created_at: "" }],
  ...overrides,
});

describe("RoundsBlock", () => {
  it("shows issue titles, schedule and starts the next round", () => {
    const onStart = vi.fn();
    render(<RoundsBlock statuses={[status()]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} onStart={onStart} onSelectIssue={vi.fn()} />);
    expect(screen.getByText("Daily ideas")).toBeInTheDocument();
    expect(screen.getByText(/Next/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily ideas" }));
    expect(screen.getByText("FIR-42 · Investigate returns")).toBeInTheDocument();
    expect(screen.getByText("2 held responses")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Run Daily ideas" }));
    expect(onStart).toHaveBeenCalledWith("round-1");
  });

  it("renders all rounds inside one inbox block with settings in its header", () => {
    const second = status({ round: { ...status().round, id: "round-2", name: "Live follow-ups", mode: "live" } });
    const settings = <button type="button">Round settings</button>;
    const { container } = render(<RoundsBlock statuses={[status(), second]} issueTitles={{}} onStart={vi.fn()} onSelectIssue={vi.fn()} settings={settings} />);
    const blocks = within(container).getAllByRole("region", { name: "Rounds" });
    expect(blocks).toHaveLength(1);
    expect(within(blocks[0]!).getByRole("button", { name: "Round settings" })).toBeInTheDocument();
    expect(within(blocks[0]!).getByText("Daily ideas")).toBeInTheDocument();
    expect(within(blocks[0]!).getByText("Live follow-ups")).toBeInTheDocument();
    expect(within(blocks[0]!).getByRole("button", { name: "Run Daily ideas" })).toBeInTheDocument();
    expect(within(blocks[0]!).getByRole("button", { name: "Run Live follow-ups" })).toBeInTheDocument();
  });

  it("uses the inbox row renderer for messages in an expanded round", () => {
    const renderIssue = vi.fn((issueId: string) => <div data-testid="inbox-row">Inbox row {issueId}</div>);
    const { container } = render(<RoundsBlock statuses={[status()]} issueTitles={{}} onStart={vi.fn()} onSelectIssue={vi.fn()} renderIssue={renderIssue} />);
    fireEvent.click(within(container).getByRole("button", { name: "Expand Daily ideas" }));
    expect(within(container).getByTestId("inbox-row")).toHaveTextContent("Inbox row issue-1");
    expect(renderIssue).toHaveBeenCalledWith("issue-1");
  });

  it("shows live run progress and the next-round action", () => {
    const onStart = vi.fn();
    const { container } = render(<RoundsBlock statuses={[status({ active_run: { id: "run-1", round_id: "round-1", status: "ready", total_count: 4, responded_count: 3, stalled_count: 1, nudged_count: 0, started_at: "", ready_at: "", completed_at: null, created_at: "" } })]} issueTitles={{}} onStart={onStart} onSelectIssue={vi.fn()} />);
    expect(within(container).getByText("3/4 ready")).toBeInTheDocument();
    fireEvent.click(within(container).getByRole("button", { name: "Run Daily ideas" }));
    expect(onStart).toHaveBeenCalledWith("round-1");
  });

  it("shows progress and nudges, then opens a ready run for review", () => {
    const { container, rerender } = render(<RoundsBlock statuses={[status({ active_run: {
      id: "run-1", round_id: "round-1", status: "running", total_count: 4,
      responded_count: 2, stalled_count: 0, nudged_count: 1, started_at: "",
      ready_at: null, completed_at: null, created_at: "",
    } })]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);

    expect(within(container).getByRole("progressbar", { name: "Daily ideas progress" })).toHaveAttribute("aria-valuenow", "2");
    expect(within(container).getByText("1 nudged")).toBeInTheDocument();

    rerender(<RoundsBlock statuses={[status({ active_run: {
      id: "run-1", round_id: "round-1", status: "ready", total_count: 4,
      responded_count: 4, stalled_count: 0, nudged_count: 1, started_at: "",
      ready_at: "", completed_at: null, created_at: "",
    } })]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);

    expect(within(container).getByRole("button", { name: "FIR-42 · Investigate returns" })).toBeVisible();
  });
});

describe("RoundManager", () => {
  it("creates, edits, removes members and deletes rounds", () => {
    const actions = { onCreate: vi.fn(), onUpdate: vi.fn(), onDelete: vi.fn(), onRemoveMember: vi.fn() };
    render(<RoundManager statuses={[status()]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} {...actions} />);
    fireEvent.click(screen.getByRole("button", { name: "Manage rounds" }));
    fireEvent.click(screen.getByRole("button", { name: "Create round" }));
    fireEvent.change(screen.getByLabelText("Round name"), { target: { value: "Weekly review" } });
    expect(screen.queryByText(/cron/i)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: "Live" }));
    fireEvent.click(screen.getByRole("button", { name: "Save round" }));
    expect(actions.onCreate).toHaveBeenCalledWith(expect.objectContaining({ name: "Weekly review", mode: "live" }));
    fireEvent.click(screen.getByRole("button", { name: "Remove FIR-42 · Investigate returns" }));
    expect(actions.onRemoveMember).toHaveBeenCalledWith("round-1", "issue-1");
    fireEvent.click(screen.getByRole("button", { name: "Delete Daily ideas" }));
    expect(actions.onDelete).toHaveBeenCalledWith("round-1");
  });
});

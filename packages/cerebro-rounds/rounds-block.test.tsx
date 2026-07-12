// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RoundManager, RoundPickerDialog, RoundsBlock } from "./rounds-block";
import type { RoundStatus } from "./schemas";

const mocks = vi.hoisted(() => ({ mobile: false, add: vi.fn() }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => mocks.mobile }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("./queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./queries")>()),
  useRoundStatuses: () => ({ data: [status()] }),
  useAddIssueToRound: () => ({ mutate: mocks.add }),
}));

afterEach(() => {
  cleanup();
  mocks.mobile = false;
  mocks.add.mockReset();
});

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
  it("matches native inbox folding and hides every count while folded", () => {
    const { container } = render(<RoundsBlock statuses={[status()]} issueTitles={{}} onStart={vi.fn()} onSelectIssue={vi.fn()} defaultCollapsed />);
    const block = within(container).getByRole("region", { name: "Rounds" });
    expect(within(block).getByRole("button", { name: "Expand Rounds" })).toBeInTheDocument();
    expect(within(block).queryByText("1")).not.toBeInTheDocument();
    expect(within(block).queryByText("Daily ideas")).not.toBeInTheDocument();
  });

  it("searches the inbox rows inside the expanded block", () => {
    const second = status({ round: { ...status().round, id: "round-2", name: "Weekly" }, members: [{ ...status().members[0]!, round_id: "round-2", issue_id: "issue-2" }] });
    render(<RoundsBlock statuses={[status(), second]} issueTitles={{ "issue-1": "Alpha returns", "issue-2": "Beta pricing" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), { target: { value: "beta" } });
    expect(screen.queryByText("Daily ideas")).not.toBeInTheDocument();
    expect(screen.getByText("Weekly")).toBeInTheDocument();
  });

  it("shows a green disabled play button when every conversation is done", () => {
    const completed = status({ active_run: { id: "run-1", round_id: "round-1", status: "ready", total_count: 1, responded_count: 1, stalled_count: 0, nudged_count: 0, started_at: "", ready_at: "", completed_at: null, created_at: "" } });
    render(<RoundsBlock statuses={[completed]} issueTitles={{}} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    expect(screen.getByRole("button", { name: "Run Daily ideas" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Run Daily ideas" })).toHaveClass("bg-success");
  });

  it("omits stale members when the shared inbox row no longer exists", () => {
    render(<RoundsBlock statuses={[status()]} issueTitles={{ "issue-1": "Old fallback row" }} onStart={vi.fn()} onSelectIssue={vi.fn()} renderIssue={() => null} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily ideas" }));
    expect(screen.queryByText("Old fallback row")).not.toBeInTheDocument();
  });

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
  it("opens settings in a bottom drawer on mobile", () => {
    mocks.mobile = true;
    render(<RoundManager statuses={[status()]} issueTitles={{}} onCreate={vi.fn()} onUpdate={vi.fn()} onDelete={vi.fn()} onRemoveMember={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Manage rounds" }));
    expect(document.querySelector('[data-slot="drawer-content"]')).toBeInTheDocument();
    mocks.mobile = false;
  });

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

describe("RoundPickerDialog", () => {
  it("opens Add to Round in a bottom drawer on mobile", () => {
    mocks.mobile = true;
    render(<RoundPickerDialog issueId="new-issue" open onOpenChange={vi.fn()} />);
    expect(screen.getByText("Add to Round")).toBeInTheDocument();
    expect(document.querySelector('[data-slot="drawer-content"]')).toBeInTheDocument();
    mocks.mobile = false;
  });
});

describe("RoundPickerDialog inside a clickable inbox row", () => {
  it("does not bubble clicks to the surrounding row (FIR-3107)", () => {
    const rowClick = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <div role="button" tabIndex={0} onClick={rowClick}>
        <RoundPickerDialog issueId="new-issue" open onOpenChange={onOpenChange} />
      </div>,
    );
    fireEvent.click(screen.getByRole("button", { name: /Daily ideas/ }));
    expect(mocks.add).toHaveBeenCalledWith({ roundId: "round-1", issueId: "new-issue" });
    expect(onOpenChange).toHaveBeenCalledWith(false);
    expect(rowClick).not.toHaveBeenCalled();
  });
});

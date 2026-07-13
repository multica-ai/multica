// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RoundManager, RoundPickerDialog, RoundsBlock } from "./rounds-block";
import type { RoundStatus } from "./schemas";

const mocks = vi.hoisted(() => ({
  mobile: false,
  add: vi.fn(),
  remove: vi.fn(),
  removeAsync: vi.fn(async () => undefined),
  statuses: null as unknown[] | null,
}));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => mocks.mobile }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("./queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./queries")>()),
  useRoundStatuses: () => ({ data: mocks.statuses ?? [status()] }),
  useAddIssueToRound: () => ({ mutate: mocks.add }),
  useRemoveIssueFromRound: () => ({ mutate: mocks.remove, mutateAsync: mocks.removeAsync }),
}));

afterEach(() => {
  cleanup();
  mocks.mobile = false;
  mocks.add.mockReset();
  mocks.remove.mockReset();
  mocks.removeAsync.mockClear();
  mocks.statuses = null;
});

// The default fixture is a resting round (no open answer cycle) holding two
// replies for its next start. Tests about the open-cycle state override
// round.cycle_opened_at.
const status = (overrides: Partial<RoundStatus> = {}): RoundStatus => ({
  round: {
    id: "round-1", workspace_id: "ws", owner_id: "owner", name: "Daily ideas",
    mode: "batch",
    schedule_cron: "0 9 * * *", timezone: "Europe/Copenhagen", next_run_at: "2026-07-11T07:00:00Z",
    cycle_opened_at: null,
    created_at: "", updated_at: "",
  },
  active_run: null,
  members: [{ round_id: "round-1", issue_id: "issue-1", added_by_type: "member", added_by_id: "owner", held_trigger_count: 2, waiting_count: 1, queued_count: 0, state: "waiting", created_at: "" }],
  ...overrides,
});
const openCycle = (overrides: Partial<RoundStatus> = {}): RoundStatus => {
  const base = status(overrides);
  return { ...base, round: { ...base.round, cycle_opened_at: "2026-07-11T07:00:00Z" } };
};

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

  it("expands a round automatically when the search matches one of its issues (FIR-3108)", () => {
    const second = status({ round: { ...status().round, id: "round-2", name: "Weekly" }, members: [{ ...status().members[0]!, round_id: "round-2", issue_id: "issue-2" }] });
    render(<RoundsBlock statuses={[status(), second]} issueTitles={{ "issue-1": "Alpha returns", "issue-2": "Beta pricing" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    expect(screen.queryByText("Beta pricing")).not.toBeInTheDocument();
    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), { target: { value: "beta" } });
    expect(screen.getByRole("button", { name: "Collapse Weekly" })).toBeInTheDocument();
    expect(screen.getByText("Beta pricing")).toBeInTheDocument();
  });

  it("shows only matching issues inside a round expanded by search", () => {
    const multiMember = status({
      members: [
        status().members[0]!,
        { ...status().members[0]!, issue_id: "issue-2" },
      ],
    });
    render(<RoundsBlock statuses={[multiMember]} issueTitles={{ "issue-1": "Alpha returns", "issue-2": "Beta pricing" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), { target: { value: "beta" } });
    expect(screen.getByText("Beta pricing")).toBeInTheDocument();
    expect(screen.queryByText("Alpha returns")).not.toBeInTheDocument();
  });

  it("expands every matching round when a search term matches issues in more than one round", () => {
    const second = status({ round: { ...status().round, id: "round-2", name: "Weekly" }, members: [{ ...status().members[0]!, round_id: "round-2", issue_id: "issue-2" }] });
    render(<RoundsBlock statuses={[status(), second]} issueTitles={{ "issue-1": "Returns queue", "issue-2": "Returns follow-up" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    fireEvent.change(screen.getByRole("searchbox", { name: "Search Rounds" }), { target: { value: "returns" } });
    expect(screen.getByRole("button", { name: "Collapse Daily ideas" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Collapse Weekly" })).toBeInTheDocument();
    expect(screen.getByText("Returns queue")).toBeInTheDocument();
    expect(screen.getByText("Returns follow-up")).toBeInTheDocument();
  });

  it("keeps Run enabled on a resting round while replies are held (FIR-3114)", () => {
    const onStart = vi.fn();
    render(<RoundsBlock statuses={[status()]} issueTitles={{}} onStart={onStart} onSelectIssue={vi.fn()} />);
    const run = screen.getByRole("button", { name: "Run Daily ideas" });
    expect(run).toBeEnabled();
    fireEvent.click(run);
    expect(onStart).toHaveBeenCalledWith("round-1");
  });

  it("hides Run during an open cycle with nothing held, and Pause collapses the round (FIR-3114)", () => {
    const onDismiss = vi.fn();
    const surfaced = openCycle({
      members: [{ ...status().members[0]!, held_trigger_count: 0 }],
    });
    render(<RoundsBlock statuses={[surfaced]} issueTitles={{}} onStart={vi.fn()} onDismiss={onDismiss} onSelectIssue={vi.fn()} />);
    expect(screen.queryByRole("button", { name: "Run Daily ideas" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Collapse Daily ideas" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Pause Daily ideas" }));
    expect(onDismiss).toHaveBeenCalledWith("round-1");
    expect(screen.getByRole("button", { name: "Expand Daily ideas" })).toBeInTheDocument();
  });

  it("folds answered members behind a collapse in batch and live alike (FIR-3114 review, point 1)", () => {
    const answered = status({ members: [{ ...status().members[0]!, state: "answered", waiting_count: 0 }] });
    render(<RoundsBlock statuses={[answered]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily ideas" }));
    expect(screen.queryByText("FIR-42 · Investigate returns")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand answered in Daily ideas" }));
    expect(screen.getByText("Answered (1)")).toBeInTheDocument();
    expect(screen.getByText("FIR-42 · Investigate returns")).toBeInTheDocument();
  });

  it("hides working and queued members entirely until the next round (FIR-3114 review, point 3)", () => {
    const mixed = openCycle({ members: [
      { ...status().members[0]!, state: "working", waiting_count: 0, held_trigger_count: 0 },
      { ...status().members[0]!, issue_id: "issue-2", state: "waiting", waiting_count: 4, held_trigger_count: 0 },
      { ...status().members[0]!, issue_id: "issue-3", state: "queued", waiting_count: 0, queued_count: 2, held_trigger_count: 0 },
    ] });
    render(<RoundsBlock statuses={[mixed]} issueTitles={{ "issue-1": "Agent busy", "issue-2": "Needs me", "issue-3": "Replied after my answer" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    expect(screen.getByText("4 waiting")).toBeInTheDocument();
    expect(screen.getByText("Needs me")).toBeInTheDocument();
    expect(screen.queryByText("Agent busy")).not.toBeInTheDocument();
    expect(screen.queryByText("Replied after my answer")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Expand answered in Daily ideas" })).not.toBeInTheDocument();
  });

  it("pauses an open cycle while a run is dispatching (FIR-3114 review, point 4)", () => {
    const onDismiss = vi.fn();
    const runningRun = openCycle({ active_run: { id: "run-1", round_id: "round-1", status: "running", total_count: 3, responded_count: 1, stalled_count: 0, nudged_count: 0, started_at: "", ready_at: null, completed_at: null, created_at: "" } });
    render(<RoundsBlock statuses={[runningRun]} issueTitles={{}} onStart={vi.fn()} onDismiss={onDismiss} onSelectIssue={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Pause Daily ideas" }));
    expect(onDismiss).toHaveBeenCalledWith("round-1");
  });

  it("falls back to the plain title row when the shared inbox row no longer exists (FIR-3114 review round 3, point 1)", () => {
    render(<RoundsBlock statuses={[status()]} issueTitles={{ "issue-1": "Counted fallback row" }} onStart={vi.fn()} onSelectIssue={vi.fn()} renderIssue={() => null} />);
    fireEvent.click(screen.getByRole("button", { name: "Expand Daily ideas" }));
    expect(screen.getByText("Counted fallback row")).toBeInTheDocument();
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

  it("counts the messages waiting for the owner, not run bookkeeping (FIR-3114 review, point 3)", () => {
    const { container } = render(<RoundsBlock statuses={[openCycle({
      members: [{ ...status().members[0]!, waiting_count: 9 }],
    })]} issueTitles={{}} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    expect(within(container).getByText("9 waiting")).toBeInTheDocument();
  });

  it("shows progress while dispatching, then surfaces the next cycle's messages", () => {
    const { container, rerender } = render(<RoundsBlock statuses={[status({ active_run: {
      id: "run-1", round_id: "round-1", status: "running", total_count: 4,
      responded_count: 2, stalled_count: 0, nudged_count: 1, started_at: "",
      ready_at: null, completed_at: null, created_at: "",
    } })]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);

    expect(within(container).getByRole("progressbar", { name: "Daily ideas progress" })).toHaveAttribute("aria-valuenow", "2");
    expect(within(container).getByText("1 nudged")).toBeInTheDocument();
    expect(within(container).getByText("2/4 running")).toBeInTheDocument();

    rerender(<RoundsBlock statuses={[openCycle({
      members: [{ ...status().members[0]!, held_trigger_count: 0 }],
    })]} issueTitles={{ "issue-1": "FIR-42 · Investigate returns" }} onStart={vi.fn()} onSelectIssue={vi.fn()} />);

    expect(within(container).getByRole("button", { name: "FIR-42 · Investigate returns" })).toBeVisible();
  });

  it("marks an answered round done and drops it into the Ready to start group (FIR-3114 review round 3, points 2+4)", () => {
    const answering = openCycle({ members: [{ ...status().members[0]!, waiting_count: 1, held_trigger_count: 0 }] });
    const done = status({
      round: { ...status().round, id: "round-2", name: "Distractions" },
      members: [{ ...status().members[0]!, round_id: "round-2", issue_id: "issue-2", state: "queued", waiting_count: 0, queued_count: 3, held_trigger_count: 0 }],
    });
    const empty = status({
      round: { ...status().round, id: "round-3", name: "Hooks" },
      members: [{ ...status().members[0]!, round_id: "round-3", issue_id: "issue-3", state: "planned", waiting_count: 0, queued_count: 0, held_trigger_count: 0 }],
    });
    render(<RoundsBlock statuses={[answering, done, empty]} issueTitles={{}} onStart={vi.fn()} onSelectIssue={vi.fn()} />);
    const group = screen.getByLabelText("Ready to start");
    expect(group).toBeInTheDocument();
    expect(screen.getByText("1 waiting")).toBeInTheDocument();
    expect(screen.getByText("Distractions")).toBeInTheDocument();
    expect(screen.getByText("3 queued")).toBeInTheDocument();
    // A round with 0 messages goes inactive and disappears from the block.
    expect(screen.queryByText("Hooks")).not.toBeInTheDocument();
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

describe("RoundPickerDialog for a round member (FIR-3114 review, point 5)", () => {
  it("removes the issue from its round", () => {
    const onOpenChange = vi.fn();
    render(<RoundPickerDialog issueId="issue-1" open onOpenChange={onOpenChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Remove from Daily ideas" }));
    expect(mocks.remove).toHaveBeenCalledWith({ roundId: "round-1", issueId: "issue-1" });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("moves the issue to another round (remove, then add)", async () => {
    mocks.statuses = [status(), status({ round: { ...status().round, id: "round-2", name: "Weekly" }, members: [] })];
    render(<RoundPickerDialog issueId="issue-1" open onOpenChange={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /Weekly/ }));
    await waitFor(() => expect(mocks.add).toHaveBeenCalledWith({ roundId: "round-2", issueId: "issue-1" }));
    expect(mocks.removeAsync).toHaveBeenCalledWith({ roundId: "round-1", issueId: "issue-1" });
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

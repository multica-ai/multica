// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RoundHeldReplyBar } from "./round-held-reply-bar";
import type { RoundStatus } from "./schemas";

const mocks = vi.hoisted(() => ({ flag: true, statuses: [] as RoundStatus[] }));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => mocks.flag }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("./queries", () => ({ useRoundStatuses: () => ({ data: mocks.statuses }) }));

afterEach(() => {
  cleanup();
  mocks.flag = true;
  mocks.statuses = [];
});

const status = (overrides: Partial<RoundStatus> = {}): RoundStatus => ({
  round: {
    id: "round-1", workspace_id: "ws", owner_id: "owner", name: "Daily ideas",
    mode: "batch",
    schedule_cron: "0 9 * * *", timezone: "Europe/Copenhagen", next_run_at: "2026-07-13T07:00:00Z",
    created_at: "", updated_at: "",
  },
  active_run: null,
  members: [{ round_id: "round-1", issue_id: "issue-1", added_by_type: "member", added_by_id: "owner", held_trigger_count: 1, created_at: "" }],
  ...overrides,
});

// FIR-3114 — a member reply on a batch-round issue is held until the round
// runs; the issue page shows that as a wakeup-style banner.
describe("RoundHeldReplyBar", () => {
  it("shows the held reply with the round name and the next run", () => {
    mocks.statuses = [status()];
    render(<RoundHeldReplyBar issueId="issue-1" />);
    expect(screen.getByRole("status", { name: "Reply held for Daily ideas" })).toBeInTheDocument();
    expect(screen.getByText(/Reply held/)).toBeInTheDocument();
    expect(screen.getByText(/Runs /)).toBeInTheDocument();
  });

  it("pluralizes held replies and falls back to the manual-run hint without a schedule", () => {
    mocks.statuses = [status({
      round: { ...status().round, next_run_at: null },
      members: [{ ...status().members[0]!, held_trigger_count: 3 }],
    })];
    render(<RoundHeldReplyBar issueId="issue-1" />);
    expect(screen.getByText(/3 replies held/)).toBeInTheDocument();
    expect(screen.getByText("Runs when you press Run")).toBeInTheDocument();
  });

  it("renders nothing without held replies, for live rounds, for other issues, or when the flag is off", () => {
    mocks.statuses = [status({ members: [{ ...status().members[0]!, held_trigger_count: 0 }] })];
    const { container: noHeld } = render(<RoundHeldReplyBar issueId="issue-1" />);
    expect(noHeld).toBeEmptyDOMElement();

    mocks.statuses = [status({ round: { ...status().round, mode: "live" } })];
    const { container: live } = render(<RoundHeldReplyBar issueId="issue-1" />);
    expect(live).toBeEmptyDOMElement();

    mocks.statuses = [status()];
    const { container: other } = render(<RoundHeldReplyBar issueId="issue-2" />);
    expect(other).toBeEmptyDOMElement();

    mocks.flag = false;
    const { container: flagOff } = render(<RoundHeldReplyBar issueId="issue-1" />);
    expect(flagOff).toBeEmptyDOMElement();
  });
});

import { describe, expect, it } from "vitest";
import { parseRoundStatuses, roundExcludedIssueIds, type RoundStatus } from "./schemas";

describe("round API compatibility", () => {
  it("parses membership and the active answer snapshot", () => {
    const [status] = parseRoundStatuses({ rounds: [{
      round: { id: "r1", name: "Daily", ignored_legacy_field: "batch" },
      members: [{ round_id: "r1", issue_id: "i1" }, { round_id: "r1", issue_id: "i2" }],
      active_cycle: {
        id: "c1", round_id: "r1", started_at: "2026-07-14T12:00:00Z",
        items: [{ issue_id: "i1", handled_at: null }, { issue_id: "i2", handled_at: "2026-07-14T12:01:00Z" }],
      },
    }] });

    expect(status?.round).toMatchObject({ id: "r1", name: "Daily" });
    expect(status?.round).not.toHaveProperty("ignored_legacy_field");
    expect(status?.active_cycle?.items).toEqual([
      { issue_id: "i1", handled_at: null },
      { issue_id: "i2", handled_at: "2026-07-14T12:01:00Z" },
    ]);
  });

  it("falls back safely for malformed responses", () => {
    expect(parseRoundStatuses({ rounds: null })).toEqual([]);
    expect(parseRoundStatuses({ rounds: [{ round: null }] })).toEqual([]);
  });
});

describe("roundExcludedIssueIds", () => {
  const status = (id: string, memberIds: string[], cycleIds: string[] | null): RoundStatus => ({
    round: { id, workspace_id: "ws", owner_id: "owner", name: id, created_at: "", updated_at: "" },
    members: memberIds.map((issue_id) => ({
      round_id: id, issue_id, added_by_type: "member", added_by_id: "owner", created_at: "",
    })),
    active_cycle: cycleIds
      ? { id: `cycle-${id}`, round_id: id, started_at: "2026-07-14T12:00:00Z", items: cycleIds.map((issue_id) => ({ issue_id, handled_at: null })) }
      : null,
  });

  // FIR-3293 — the double display Jesper reported: a Round member that Play did
  // not pick up (already read, or already running) stayed in All messages while
  // also sitting under its Round.
  it("hides members that the latest Play run left out of active_cycle", () => {
    const statuses = [status("r1", ["read-msg", "unread-msg"], ["unread-msg"])];
    expect(roundExcludedIssueIds(statuses)).toEqual(new Set(["read-msg", "unread-msg"]));
  });

  it("hides members before Play has ever run (no active_cycle)", () => {
    expect(roundExcludedIssueIds([status("r1", ["a", "b"], null)])).toEqual(new Set(["a", "b"]));
  });

  it("merges members across rounds and dedupes an issue in two rounds", () => {
    const statuses = [status("r1", ["shared", "a"], ["a"]), status("r2", ["shared", "b"], null)];
    expect(roundExcludedIssueIds(statuses)).toEqual(new Set(["shared", "a", "b"]));
  });

  it("excludes nothing when there are no rounds", () => {
    expect(roundExcludedIssueIds([])).toEqual(new Set());
  });
});

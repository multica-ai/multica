import { describe, expect, it } from "vitest";
import { parseRoundStatuses, roundIssueIdsToExclude, roundMembershipLabel, roundRunState } from "./schemas";

describe("round API compatibility", () => {
  it("falls back safely for malformed list responses", () => {
    expect(parseRoundStatuses({ rounds: null })).toEqual([]);
  });

  it("preserves multiple rounds and defaults missing counters", () => {
    const rounds = parseRoundStatuses({ rounds: [
      { round: { id: "r1", name: "Daily" }, active_run: null, members: [] },
      { round: { id: "r2", name: "Ideas" }, active_run: { id: "run", round_id: "r2", status: "unexpected" }, members: [{ round_id: "r2", issue_id: "i2", held_trigger_count: 1 }] },
    ] });
    expect(rounds).toHaveLength(2);
    expect(rounds[0]?.active_run).toBeNull();
    expect(rounds[1]?.active_run?.status).toBe("unknown");
    expect(rounds[0]?.round.mode).toBe("batch");
  });

  it("excludes only queued or running round issues from All messages", () => {
    const rounds = parseRoundStatuses({ rounds: [{ round: { id: "queued-round", name: "Queued" }, active_run: null,
      members: [{ round_id: "queued-round", issue_id: "waiting", held_trigger_count: 0 }] },
    { round: { id: "r", name: "Daily" }, active_run: {
      id: "run", round_id: "r", status: "running", total_count: 2,
    }, members: [{ round_id: "r", issue_id: "queued", held_trigger_count: 0 }, { round_id: "r", issue_id: "running", held_trigger_count: 1 }] }] });
    expect(roundIssueIdsToExclude(rounds)).toEqual(new Set(["waiting", "queued", "running"]));
    expect(roundIssueIdsToExclude(rounds, false)).toEqual(new Set());
    expect(roundRunState(rounds, "queued")).toBe("round_running");
  });

  it("describes the round state shown beside the issue composer", () => {
    const rounds = parseRoundStatuses({ rounds: [{ round: { id: "r", name: "Daily" }, active_run: null, members: [{ round_id: "r", issue_id: "i", held_trigger_count: 2 }] }] });
    expect(roundMembershipLabel(rounds, "i")).toBe("Daily · queued · 2 held responses");
    expect(roundMembershipLabel(rounds, "other")).toBeNull();
  });
});

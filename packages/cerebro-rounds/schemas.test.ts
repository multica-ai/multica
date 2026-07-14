import { describe, expect, it } from "vitest";
import { parseRoundStatuses } from "./schemas";

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

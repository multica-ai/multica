import { describe, expect, it } from "vitest";
import { parseApprovalPolicies, parsePolicyAutoApprovals } from "./approval-policy";

const policy = {
  id: "p1",
  name: "Routine notes",
  kind: "decision",
  match: { squad_ids: ["s1"], max_cost_micros: 500 },
  max_per_week: 3,
  valid_until: null,
  active: true,
  created_by: "u1",
  created_at: "2026-11-01T10:00:00Z",
  week_approvals: 1,
  flagged: false,
};

describe("parseApprovalPolicies", () => {
  it("reads the list with counts and flags", () => {
    const [p] = parseApprovalPolicies({ policies: [{ ...policy, flagged: true, week_approvals: 2 }] }, "x");
    expect(p).toMatchObject({ id: "p1", flagged: true, week_approvals: 2, max_per_week: 3 });
    expect(p?.match.squad_ids).toEqual(["s1"]);
    expect(p?.match.max_cost_micros).toBe(500);
  });

  it("drops a malformed entry and keeps the rest", () => {
    const list = parseApprovalPolicies({ policies: [policy, { id: 7, name: "bad id type" }] }, "x");
    expect(list.map((p) => p.id)).toEqual(["p1"]);
  });

  it("treats a missing match and a missing list as empty", () => {
    const [p] = parseApprovalPolicies({ policies: [{ ...policy, match: null }] }, "x");
    expect(p?.match.squad_ids).toEqual([]);
    expect(parseApprovalPolicies({}, "x")).toEqual([]);
  });
});

describe("parsePolicyAutoApprovals", () => {
  it("passes reversal flags through per entry and for the history", () => {
    const history = parsePolicyAutoApprovals(
      {
        flagged: true,
        auto_approvals: [
          {
            proposal_id: "pr1",
            squad_id: "s1",
            kind: "decision",
            state: "approved",
            title: "Draft",
            identifier: "MUL-9",
            decided_at: "2026-11-02T10:00:00Z",
            reversed_at: "2026-11-03T09:00:00Z",
            reversal_note: "Not needed.",
            flagged: true,
          },
        ],
      },
      "x",
    );
    expect(history.flagged).toBe(true);
    expect(history.auto_approvals[0]).toMatchObject({ flagged: true, reversal_note: "Not needed.", reversed_at: "2026-11-03T09:00:00Z" });
  });

  it("reads an empty or malformed history as empty", () => {
    expect(parsePolicyAutoApprovals(null, "x")).toEqual({ auto_approvals: [], flagged: false });
  });
});

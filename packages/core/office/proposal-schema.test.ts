// @vitest-environment node
import { describe, expect, it } from "vitest";
import { FALLBACK_PROPOSAL, ProposalListSchema, ProposalSchema, type Proposal } from "./proposal-schema";
import { parseWithFallback } from "../api/schema";
import { proposalKeys } from "./proposals";

const full = {
  id: "p1",
  workspace_id: "w1",
  squad_id: "s1",
  issue_id: "i1",
  issue_identifier: "MUL-7",
  issue_status: "in_review",
  title: "Pilot usage pricing",
  handoff_squad_id: "s2",
  urgent: true,
  urgent_reason: "Contract lapses",
  state: "pending",
  created_by_type: "agent",
  created_by_id: "a1",
  decided_by: null,
  decided_at: null,
  decision_note: "",
  created_at: "2026-11-01T00:00:00Z",
};

describe("ProposalSchema", () => {
  it("parses a full server response", () => {
    const p = parseWithFallback(full, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });
    expect(p).toMatchObject({ id: "p1", state: "pending", urgent: true, handoff: null });
  });

  it("keeps the handoff ids on a decision response", () => {
    const p = parseWithFallback(
      { ...full, state: "approved", handoff: { project_id: "pr1", kickoff_issue_id: "k1" } },
      ProposalSchema,
      FALLBACK_PROPOSAL,
      { endpoint: "t" },
    );
    expect(p.handoff).toEqual({ project_id: "pr1", kickoff_issue_id: "k1" });
  });

  it("degrades an unknown state to 'unknown' without failing the row", () => {
    const p = parseWithFallback({ ...full, state: "withdrawn" }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });
    expect(p.state).toBe("unknown");
    expect(p.id).toBe("p1");
  });

  it("fills defaults for fields an older server omits", () => {
    const p = parseWithFallback({ id: "p1", state: "rejected" }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });
    expect(p).toMatchObject({
      id: "p1",
      state: "rejected",
      title: "",
      urgent: false,
      handoff_squad_id: null,
      decided_at: null,
      decision_note: "",
      handoff: null,
    });
  });

  it("returns the fallback for a malformed response", () => {
    for (const bad of [null, "oops", 42, [], { id: 5 }, { ...full, urgent: "yes" }]) {
      expect(parseWithFallback(bad, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" })).toBe(FALLBACK_PROPOSAL);
    }
  });

  it("tolerates extra fields", () => {
    const p = parseWithFallback({ ...full, brand_new: 1 }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });
    expect(p.id).toBe("p1");
  });
});

describe("ProposalListSchema", () => {
  it("parses a list and degrades only the unknown rows", () => {
    const list = parseWithFallback<Proposal[]>(
      [full, { ...full, id: "p2", state: "weird" }],
      ProposalListSchema,
      [],
      { endpoint: "t" },
    );
    expect(list.map((p) => p.state)).toEqual(["pending", "unknown"]);
  });

  it("returns the empty fallback for a non-array", () => {
    expect(parseWithFallback({ proposals: [] }, ProposalListSchema, [], { endpoint: "t" })).toEqual([]);
    expect(parseWithFallback(null, ProposalListSchema, [], { endpoint: "t" })).toEqual([]);
  });

  it("returns the fallback when a row is malformed", () => {
    const fallback: never[] = [];
    expect(parseWithFallback([full, 7], ProposalListSchema, fallback, { endpoint: "t" })).toBe(fallback);
  });
});

describe("proposalKeys", () => {
  it("scopes every key by workspace and nests filtered lists under the list prefix", () => {
    expect(proposalKeys.all("w1")).toEqual(["proposals", "w1"]);
    expect(proposalKeys.listFiltered("w1", { state: "pending" }).slice(0, 3)).toEqual(proposalKeys.list("w1"));
    expect(proposalKeys.listFiltered("w1")).not.toEqual(proposalKeys.listFiltered("w2"));
    expect(proposalKeys.detail("w1", "p1")).toEqual(["proposals", "w1", "detail", "p1"]);
  });
});

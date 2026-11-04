// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  FALLBACK_PROPOSAL,
  ProposalListSchema,
  ProposalRevisionListSchema,
  ProposalSchema,
  opportunityPayloadOf,
  orgChangePayloadOf,
  stageGatePayloadOf,
  type Proposal,
  type ProposalRevision,
} from "./proposal-schema";
import { parseWithFallback } from "../api/schema";
import { proposalKeys, proposalRefusalCode } from "./proposals";
import { ApiError } from "../api/client";

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
  revision: 2,
  expires_at: "2026-11-15T00:00:00Z",
  snoozed_until: null,
  related: [
    {
      kind: "proposal",
      id: "p0",
      title: "Pilot pricing",
      score: 0.8,
      state: "approved",
      issue_identifier: "MUL-3",
      created_at: "2026-10-01T00:00:00Z",
    },
  ],
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

  it("reads the revision, window, snooze and related entries", () => {
    const p = parseWithFallback(full, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });
    expect(p.revision).toBe(2);
    expect(p.expires_at).toBe("2026-11-15T00:00:00Z");
    expect(p.snoozed_until).toBeNull();
    expect(p.related).toEqual([
      {
        kind: "proposal",
        id: "p0",
        title: "Pilot pricing",
        score: 0.8,
        state: "approved",
        issue_identifier: "MUL-3",
        created_at: "2026-10-01T00:00:00Z",
      },
    ]);
  });

  it("keeps the new lifecycle states", () => {
    for (const state of ["revising", "expired", "approved", "rejected", "pending"] as const) {
      expect(parseWithFallback({ ...full, state }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" }).state).toBe(state);
    }
  });

  it("defaults the new fields for an older server", () => {
    const p = parseWithFallback({ id: "p1", state: "pending" }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });
    expect(p).toMatchObject({ revision: 1, expires_at: null, snoozed_until: null, related: [] });
  });

  it("degrades an unknown related kind and state without failing the proposal", () => {
    const p = parseWithFallback(
      {
        ...full,
        related: [
          { kind: "memo", id: "x", title: "Old memo", score: 0.9, state: "withdrawn", issue_identifier: null, created_at: null },
          { kind: "decision", id: "d1", title: "Kb decision", score: 0.8, state: null, issue_identifier: null, created_at: null },
        ],
      },
      ProposalSchema,
      FALLBACK_PROPOSAL,
      { endpoint: "t" },
    );
    expect(p.related.map((r) => [r.kind, r.state])).toEqual([
      ["unknown", "unknown"],
      ["decision", null],
    ]);
  });

  it("returns the fallback when the related list is malformed", () => {
    expect(parseWithFallback({ ...full, related: "none" }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" })).toBe(
      FALLBACK_PROPOSAL,
    );
    expect(parseWithFallback({ ...full, related: [7] }, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" })).toBe(
      FALLBACK_PROPOSAL,
    );
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
  it("keys the revision history by workspace and proposal", () => {
    expect(proposalKeys.revisions("w1", "p1")).toEqual(["proposals", "w1", "revisions", "p1"]);
    expect(proposalKeys.revisions("w1", "p1")).not.toEqual(proposalKeys.revisions("w2", "p1"));
  });

  it("scopes every key by workspace and nests filtered lists under the list prefix", () => {
    expect(proposalKeys.all("w1")).toEqual(["proposals", "w1"]);
    expect(proposalKeys.listFiltered("w1", { state: "pending" }).slice(0, 3)).toEqual(proposalKeys.list("w1"));
    expect(proposalKeys.listFiltered("w1")).not.toEqual(proposalKeys.listFiltered("w2"));
    expect(proposalKeys.detail("w1", "p1")).toEqual(["proposals", "w1", "detail", "p1"]);
  });
});


describe("ProposalRevisionListSchema", () => {
  const revision = {
    revision: 2,
    body: "## Summary\nRevised.",
    created_by_type: "agent",
    created_by_id: "a1",
    created_at: "2026-11-02T00:00:00Z",
  };

  it("parses the history and keeps its order", () => {
    const list = parseWithFallback<ProposalRevision[]>(
      [revision, { ...revision, revision: 1 }],
      ProposalRevisionListSchema,
      [],
      { endpoint: "t" },
    );
    expect(list.map((r) => r.revision)).toEqual([2, 1]);
    expect(list[0]?.body).toBe("## Summary\nRevised.");
  });

  it("defaults the fields an older server omits", () => {
    expect(parseWithFallback([{}], ProposalRevisionListSchema, [], { endpoint: "t" })).toEqual([
      { revision: 1, body: "", created_by_type: "", created_by_id: "", created_at: "" },
    ]);
  });

  it("returns the empty fallback for a malformed body", () => {
    const fallback: ProposalRevision[] = [];
    for (const bad of [null, "oops", { revisions: [] }, [7], [{ revision: "two" }]]) {
      expect(parseWithFallback(bad, ProposalRevisionListSchema, fallback, { endpoint: "t" })).toBe(fallback);
    }
  });
});

describe("proposalRefusalCode", () => {
  it("reads the code from a coded refusal", () => {
    const err = new ApiError("the squad is revising", 409, "Conflict", { code: "proposal_revising" });
    expect(proposalRefusalCode(err)).toBe("proposal_revising");
  });

  it("returns null for an uncoded or non-API failure", () => {
    expect(proposalRefusalCode(new ApiError("boom", 500, "Server Error", "text"))).toBeNull();
    expect(proposalRefusalCode(new ApiError("boom", 500, "Server Error", { code: 7 }))).toBeNull();
    expect(proposalRefusalCode(new Error("network"))).toBeNull();
  });
});

describe("ProposalSchema kinds and payloads", () => {
  const parse = (body: unknown) => parseWithFallback(body, ProposalSchema, FALLBACK_PROPOSAL, { endpoint: "t" });

  it("reads the kind and payload a server sends", () => {
    const p = parse({ ...full, kind: "opportunity", payload: { title: "Invoices", thesis: "Slow close" } });
    expect(p.kind).toBe("opportunity");
    expect(p.payload).toEqual({ title: "Invoices", thesis: "Slow close" });
  });

  it("treats a server without kinds as a decision with an empty payload", () => {
    const p = parse({ id: "p1", state: "pending" });
    expect(p).toMatchObject({ kind: "decision", payload: {} });
  });

  it("degrades an unknown kind to 'unknown' so views can keep it read-only", () => {
    const p = parse({ ...full, kind: "org_change_v2", payload: { anything: 1 } });
    expect(p.kind).toBe("unknown");
    expect(p.payload).toEqual({ anything: 1 });
  });

  it("degrades a non-object payload to empty without failing the proposal", () => {
    for (const payload of ["oops", 42, [1, 2], null]) {
      const p = parse({ ...full, kind: "opportunity", payload });
      expect(p.id).toBe("p1");
      expect(p.payload).toEqual({});
    }
  });

  it("still returns the fallback when the kind is not a string", () => {
    expect(parse({ ...full, kind: 7 })).toBe(FALLBACK_PROPOSAL);
  });

  it("reads the opportunity fields and fills defaults for missing ones", () => {
    const withPayload = parse({
      ...full,
      kind: "opportunity",
      payload: {
        title: "Invoices",
        thesis: "Small firms close late",
        market: "SMB finance",
        evidence_doc_ids: ["d1", "d2"],
        target_stage: "validate",
      },
    });
    expect(opportunityPayloadOf(withPayload)).toEqual({
      title: "Invoices",
      thesis: "Small firms close late",
      market: "SMB finance",
      evidence_doc_ids: ["d1", "d2"],
      target_stage: "validate",
    });
    expect(opportunityPayloadOf(parse({ ...full, kind: "opportunity", payload: {} }))).toEqual({
      title: "",
      thesis: "",
      market: "",
      evidence_doc_ids: [],
      target_stage: "validate",
    });
  });

  it("degrades malformed opportunity fields to their defaults", () => {
    const p = parse({
      ...full,
      kind: "opportunity",
      payload: { thesis: 12, evidence_doc_ids: "d1", target_stage: null },
    });
    expect(opportunityPayloadOf(p)).toEqual({
      title: "",
      thesis: "",
      market: "",
      evidence_doc_ids: [],
      target_stage: "validate",
    });
  });

  it("reads the stage gate fields and drops non-string artifact ids", () => {
    const p = parse({
      ...full,
      kind: "stage_gate",
      payload: { initiative_id: "i1", from: "validate", to: "define", artifact_ids: ["a1", 9] },
    });
    expect(stageGatePayloadOf(p)).toEqual({
      initiative_id: "i1",
      from: "validate",
      to: "define",
      artifact_ids: [],
    });
    const ok = parse({
      ...full,
      kind: "stage_gate",
      payload: { initiative_id: "i1", from: "validate", to: "define", artifact_ids: ["a1"] },
    });
    expect(stageGatePayloadOf(ok).artifact_ids).toEqual(["a1"]);
  });
});

describe("orgChangePayloadOf", () => {
  const withPayload = (payload: unknown) => ({ payload }) as Pick<Proposal, "payload">;

  it("reads an instruction change's agent and new instructions", () => {
    const fields = orgChangePayloadOf(
      withPayload({ type: "instruction_change", agent_id: "ag-1", instructions: "Be brief.", diff_summary: "Shorter", rationale: "Replies ran long" }),
    );
    expect(fields).toMatchObject({ type: "instruction_change", agent_id: "ag-1", instructions: "Be brief.", charter: null });
  });

  it("reads a charter change's fields, with its responsibilities and outputs", () => {
    const fields = orgChangePayloadOf(
      withPayload({
        type: "charter_change",
        squad_id: "sq-1",
        charter: {
          mission: "Research",
          responsibilities: [{ key: "r1", text: "Track competitors" }],
          outputs: [{ key: "o1", kind: "report", kb_key: "", cadence_hint: "weekly" }],
          boundaries: "",
          escalation: "",
          intake: "",
          response_hours: 48,
        },
      }),
    );
    expect(fields.charter?.mission).toBe("Research");
    expect(fields.charter?.responsibilities).toEqual([{ key: "r1", text: "Track competitors" }]);
    expect(fields.charter?.response_hours).toBe(48);
  });

  it("reads a malformed charter as no charter instead of failing the proposal", () => {
    const fields = orgChangePayloadOf(withPayload({ type: "charter_change", squad_id: "sq-1", charter: { mission: 7, responsibilities: "x" } }));
    expect(fields.charter).toBeNull();
    expect(fields.squad_id).toBe("sq-1");
  });

  it("reads a payload that is not an object as empty", () => {
    expect(orgChangePayloadOf(withPayload("nope"))).toMatchObject({ type: "", agent_id: "", charter: null });
  });
});

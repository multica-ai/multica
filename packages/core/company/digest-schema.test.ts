// @vitest-environment node
import { describe, expect, it } from "vitest";
import { DigestSchema, FALLBACK_DIGEST } from "./digest-schema";
import { parseWithFallback } from "../api/schema";
import { digestKeys } from "./digest";

const full = {
  since: "2026-11-01T00:00:00Z",
  until: "2026-11-02T00:00:00Z",
  workspace: {
    runs: 12,
    failed_runs: 2,
    input_tokens: 1000,
    output_tokens: 500,
    reported_cost_usd: 2.5,
    offline_runtimes: { count: 1, items: [{ id: "r1", name: "Laptop", provider: "claude" }] },
    agents_with_failed_runs: { count: 1, items: [{ id: "a1", name: "Builder", failed_runs: 2 }] },
    unseated_squads: { count: 0, items: [] },
  },
  squads: [
    {
      id: "s1",
      name: "Research",
      floor_id: "f1",
      done: 3,
      in_progress: 2,
      blocked: 1,
      stale: 1,
      failed_runs: 1,
      leader_no_action: 2,
      pending_proposal_count: 1,
      pending_proposals: [{ id: "p1", title: "Pilot pricing" }],
      recent_done: [{ id: "i1", identifier: "MUL-1", title: "Teardown", mention: "[MUL-1](mention://issue/i1)" }],
    },
  ],
  omitted_squad_count: 4,
  quiet: { count: 1, squads: [{ id: "s2", name: "Ops", in_progress: 3 }] },
  knowledge: {
    stale_published: {
      count: 12,
      items: [{ id: "k1", title: "Pricing 2024", knowledge_base: "market", published_at: "2026-06-01T00:00:00Z" }],
    },
    pending_drafts: 3,
    auto_published: {
      count: 1,
      items: [
        {
          link_id: "l1",
          title: "Ledger ADR",
          knowledge_base: "engineering",
          kind: "adr",
          mode: "auto_after_review",
          author_squad_id: "s1",
          critic_squad_id: "s2",
          published_at: "2026-07-02T09:00:00Z",
        },
      ],
    },
  },
};

describe("DigestSchema", () => {
  it("parses a full server response", () => {
    const d = parseWithFallback(full, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.squads[0]).toMatchObject({ name: "Research", blocked: 1, pending_proposal_count: 1 });
    expect(d.squads[0]!.recent_done[0]!.mention).toBe("[MUL-1](mention://issue/i1)");
    expect(d.workspace.reported_cost_usd).toBe(2.5);
    expect(d.omitted_squad_count).toBe(4);
    expect(d.quiet.squads[0]).toMatchObject({ name: "Ops", in_progress: 3 });
    expect(d.knowledge.pending_drafts).toBe(3);
    expect(d.knowledge.stale_published.count).toBe(12);
    expect(d.knowledge.stale_published.items[0]).toMatchObject({ title: "Pricing 2024", knowledge_base: "market" });
    expect(d.knowledge.auto_published.items[0]).toMatchObject({ title: "Ledger ADR", mode: "auto_after_review", critic_squad_id: "s2" });
  });

  it("fills defaults for everything an older server omits", () => {
    const d = parseWithFallback({ since: "x" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d).toMatchObject({ since: "x", squads: [], omitted_squad_count: 0, quiet: { count: 0, squads: [] } });
    expect(d.workspace.offline_runtimes).toEqual({ count: 0, items: [] });
    expect(d.knowledge).toEqual({
      stale_published: { count: 0, items: [] },
      pending_drafts: 0,
      auto_published: { count: 0, items: [] },
    });
  });

  it("treats a null knowledge list as empty and keeps the counts", () => {
    const d = parseWithFallback(
      { ...full, knowledge: { stale_published: { count: 2, items: null }, pending_drafts: 1, auto_published: { count: 0, items: null } } },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.knowledge.stale_published).toEqual({ count: 2, items: [] });
    expect(d.knowledge.pending_drafts).toBe(1);
    expect(d.knowledge.auto_published).toEqual({ count: 0, items: [] });
  });

  it("falls back to an empty knowledge section when the section is malformed", () => {
    const d = parseWithFallback(
      { ...full, knowledge: { stale_published: "many", pending_drafts: 2 } },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    // A malformed section takes the whole digest to the fallback, which is an
    // empty knowledge section, never an error.
    expect(d.knowledge).toEqual(FALLBACK_DIGEST.knowledge);
    expect(d.knowledge.pending_drafts).toBe(0);
  });

  it("treats null collections as empty", () => {
    const d = parseWithFallback(
      {
        ...full,
        squads: null,
        quiet: { count: 0, squads: null },
        workspace: { ...full.workspace, offline_runtimes: { count: 0, items: null } },
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.squads).toEqual([]);
    expect(d.quiet.squads).toEqual([]);
    expect(d.workspace.offline_runtimes.items).toEqual([]);
  });

  it("defaults a squad missing its lists and counts", () => {
    const d = parseWithFallback({ squads: [{ id: "s1", name: "Research" }] }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.squads[0]).toMatchObject({ done: 0, stale: 0, pending_proposals: [], recent_done: [], floor_id: null });
  });

  it("falls back on a malformed response", () => {
    expect(parseWithFallback("nope", DigestSchema, FALLBACK_DIGEST, { endpoint: "t" })).toEqual(FALLBACK_DIGEST);
    expect(parseWithFallback({ squads: "many" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" })).toEqual(FALLBACK_DIGEST);
  });
});

describe("DigestSchema charter and routines", () => {
  const routine = {
    autopilot_id: "ap1",
    title: "Weekly scan",
    purpose: "Weekly market scan",
    expected_output: "report",
    required: true,
    state: "late",
  };

  it("reads a chartered squad's routines and a null charter for an uncharted one", () => {
    const d = parseWithFallback(
      {
        ...full,
        squads: [
          {
            ...full.squads[0],
            charter: { silent: true, routine_count: 12, routines: [routine] },
          },
          { id: "s2", name: "Ops", charter: null },
        ],
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.squads[0]!.charter).toEqual({ silent: true, routine_count: 12, routines: [routine] });
    expect(d.squads[1]!.charter).toBeNull();
  });

  it("defaults a squad that omits the charter field to no charter", () => {
    const d = parseWithFallback(full, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.squads[0]!.charter).toBeNull();
  });

  it("reads the workspace routines not on time with their squad", () => {
    const d = parseWithFallback(
      {
        ...full,
        workspace: {
          ...full.workspace,
          routines_not_on_time: {
            count: 13,
            items: [{ autopilot_id: "ap1", squad_id: "s1", squad_name: "Research", title: "Scan", state: "failed" }],
          },
        },
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.workspace.routines_not_on_time).toEqual({
      count: 13,
      items: [{ autopilot_id: "ap1", squad_id: "s1", squad_name: "Research", title: "Scan", state: "failed" }],
    });
  });

  it("defaults the workspace routines section for an older server", () => {
    const d = parseWithFallback({ since: "x" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.workspace.routines_not_on_time).toEqual({ count: 0, items: [] });
  });

  it("reads an unknown routine state or expected output as unknown", () => {
    const d = parseWithFallback(
      {
        ...full,
        squads: [
          {
            id: "s1",
            charter: {
              silent: false,
              routine_count: 1,
              routines: [{ ...routine, state: "held", expected_output: "decision" }],
            },
          },
        ],
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.squads[0]!.charter!.routines[0]).toMatchObject({ state: "unknown", expected_output: "unknown" });
  });

  it("treats null routine lists as empty", () => {
    const d = parseWithFallback(
      {
        ...full,
        squads: [{ id: "s1", charter: { silent: false, routine_count: 0, routines: null } }],
        workspace: { ...full.workspace, routines_not_on_time: { count: 0, items: null } },
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.squads[0]!.charter!.routines).toEqual([]);
    expect(d.workspace.routines_not_on_time.items).toEqual([]);
  });

  it("drops only a malformed charter section, keeping the squad and the digest", () => {
    const d = parseWithFallback(
      { ...full, squads: [{ ...full.squads[0], charter: { silent: "no", routines: [] } }] },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.squads[0]).toMatchObject({ name: "Research", charter: null });
    expect(d.workspace.runs).toBe(12);
  });
});

describe("DigestSchema inquiries", () => {
  it("reads the Secretary's inquiries with their answers", () => {
    const d = parseWithFallback(
      {
        ...full,
        inquiries: [
          { question: "Is the pricing test live?", answered: true, answer_excerpt: "Yes, since Monday." },
          { question: "Who owns churn?", answered: false, answer_excerpt: null },
        ],
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.inquiries).toEqual([
      { question: "Is the pricing test live?", answered: true, answer_excerpt: "Yes, since Monday." },
      { question: "Who owns churn?", answered: false, answer_excerpt: null },
    ]);
  });

  it("defaults inquiries to an empty list for an older server", () => {
    const d = parseWithFallback({ since: "x" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.inquiries).toEqual([]);
    expect(FALLBACK_DIGEST.inquiries).toEqual([]);
  });

  it("treats a null inquiry list as empty", () => {
    const d = parseWithFallback({ ...full, inquiries: null }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.inquiries).toEqual([]);
  });

  it("drops a malformed inquiry alone and keeps the rest of the digest", () => {
    const d = parseWithFallback(
      {
        ...full,
        inquiries: [
          { question: "Kept", answered: false, answer_excerpt: null },
          "not an inquiry",
          { question: "Also kept", answered: "maybe", answer_excerpt: 7 },
        ],
      },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.inquiries).toEqual([
      { question: "Kept", answered: false, answer_excerpt: null },
      { question: "Also kept", answered: false, answer_excerpt: null },
    ]);
    expect(d.squads[0]).toMatchObject({ name: "Research" });
  });

  it("falls back to an empty list when the field is not a list", () => {
    const d = parseWithFallback({ ...full, inquiries: "many" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.inquiries).toEqual([]);
    expect(d.squads[0]).toMatchObject({ name: "Research" });
  });
});

describe("digestKeys", () => {
  it("scopes by workspace and by since", () => {
    expect(digestKeys.since("w1", { since: "2026-11-01T00:00:00Z" })).toEqual([
      "company-digest",
      "w1",
      "2026-11-01T00:00:00Z",
    ]);
    expect(digestKeys.since("w1")).not.toEqual(digestKeys.since("w2"));
  });
});

describe("DigestSchema pipeline and overdue requests", () => {
  const initiative = {
    id: "ini-1",
    identifier: "INI-1",
    title: "Invoicing",
    stage_key: "validate",
    stage_name: "Validate",
    owner_squad_id: "sq-1",
    status: "active",
    days_in_stage: 16,
    sla_days: 14,
    stalled: true,
    artifacts_required: 2,
    artifacts_present: 1,
    blockers: ["missing competitor_profile", "stalled: 16 days in Validate, SLA 14"],
    next_gate: "A go / no-go recommendation with evidence.",
    pending_gate_proposal_id: null,
  };

  it("reads each initiative and overdue request", () => {
    const d = DigestSchema.parse({
      pipeline: [initiative],
      overdue_requests: [{ issue_id: "i-9", identifier: "MUL-9", title: "PRD", squad_id: "sq-2", hours_overdue: 30 }],
    });
    expect(d.pipeline).toHaveLength(1);
    expect(d.pipeline[0]).toMatchObject({ identifier: "INI-1", stalled: true, blockers: initiative.blockers });
    expect(d.overdue_requests[0]).toMatchObject({ identifier: "MUL-9", hours_overdue: 30 });
  });

  it("reads neither list from an older server as none", () => {
    const d = DigestSchema.parse({});
    expect(d.pipeline).toEqual([]);
    expect(d.overdue_requests).toEqual([]);
  });

  it("drops a malformed initiative alone and keeps the others", () => {
    const d = DigestSchema.parse({
      pipeline: [initiative, "not an object", null, { ...initiative, id: "ini-2", blockers: "oops" }],
    });
    expect(d.pipeline.map((p) => p.id)).toEqual(["ini-1", "ini-2"]);
    expect(d.pipeline[1]?.blockers).toEqual([]);
  });

  it("treats a non-list pipeline as none and keeps the rest of the digest", () => {
    const d = DigestSchema.parse({ pipeline: { not: "a list" }, overdue_requests: 3, omitted_squad_count: 2 });
    expect(d.pipeline).toEqual([]);
    expect(d.overdue_requests).toEqual([]);
    expect(d.omitted_squad_count).toBe(2);
  });

  it("falls back to empty lists when the whole digest is unreadable", () => {
    expect(FALLBACK_DIGEST.pipeline).toEqual([]);
    expect(FALLBACK_DIGEST.overdue_requests).toEqual([]);
  });
});

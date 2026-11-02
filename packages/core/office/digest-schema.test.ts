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
  });

  it("fills defaults for everything an older server omits", () => {
    const d = parseWithFallback({ since: "x" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d).toMatchObject({ since: "x", squads: [], omitted_squad_count: 0, quiet: { count: 0, squads: [] } });
    expect(d.workspace.offline_runtimes).toEqual({ count: 0, items: [] });
    expect(d.knowledge).toEqual({ stale_published: { count: 0, items: [] }, pending_drafts: 0 });
  });

  it("treats a null knowledge list as empty and keeps the counts", () => {
    const d = parseWithFallback(
      { ...full, knowledge: { stale_published: { count: 2, items: null }, pending_drafts: 1 } },
      DigestSchema,
      FALLBACK_DIGEST,
      { endpoint: "t" },
    );
    expect(d.knowledge.stale_published).toEqual({ count: 2, items: [] });
    expect(d.knowledge.pending_drafts).toBe(1);
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

describe("digestKeys", () => {
  it("scopes by workspace and by since", () => {
    expect(digestKeys.since("w1", { since: "2026-11-01T00:00:00Z" })).toEqual([
      "office-digest",
      "w1",
      "2026-11-01T00:00:00Z",
    ]);
    expect(digestKeys.since("w1")).not.toEqual(digestKeys.since("w2"));
  });
});

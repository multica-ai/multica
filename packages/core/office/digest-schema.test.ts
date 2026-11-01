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
};

describe("DigestSchema", () => {
  it("parses a full server response", () => {
    const d = parseWithFallback(full, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d.squads[0]).toMatchObject({ name: "Research", blocked: 1, pending_proposal_count: 1 });
    expect(d.squads[0]!.recent_done[0]!.mention).toBe("[MUL-1](mention://issue/i1)");
    expect(d.workspace.reported_cost_usd).toBe(2.5);
    expect(d.omitted_squad_count).toBe(4);
    expect(d.quiet.squads[0]).toMatchObject({ name: "Ops", in_progress: 3 });
  });

  it("fills defaults for everything an older server omits", () => {
    const d = parseWithFallback({ since: "x" }, DigestSchema, FALLBACK_DIGEST, { endpoint: "t" });
    expect(d).toMatchObject({ since: "x", squads: [], omitted_squad_count: 0, quiet: { count: 0, squads: [] } });
    expect(d.workspace.offline_runtimes).toEqual({ count: 0, items: [] });
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

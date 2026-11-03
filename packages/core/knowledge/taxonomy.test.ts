// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { parseWithFallback } from "../api/schema";
import {
  FALLBACK_KNOWLEDGE_EVAL_CASES,
  FALLBACK_KNOWLEDGE_EVAL_RUN,
  FALLBACK_KNOWLEDGE_EVAL_RUNS,
  FALLBACK_KNOWLEDGE_HEALTH,
  FALLBACK_KNOWLEDGE_KIND,
  FALLBACK_KNOWLEDGE_KINDS,
  FALLBACK_KNOWLEDGE_POLICIES,
  FALLBACK_KNOWLEDGE_POLICY,
  KnowledgeEvalRunSchema,
  KnowledgeEvalRunsSchema,
  KnowledgeHealthSchema,
  KnowledgeKindSchema,
  KnowledgeKindsSchema,
  KnowledgePublishPoliciesSchema,
  KnowledgePublishPolicySchema,
} from "./taxonomy";
import type {
  KnowledgeEvalRun,
  KnowledgeEvalRuns,
  KnowledgeHealth,
  KnowledgeKind,
  KnowledgeKinds,
  KnowledgePublishPolicies,
  KnowledgePublishPolicy,
} from "./taxonomy-types";

afterEach(() => {
  vi.unstubAllGlobals();
});

function respond(body: unknown, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    status === 204
      ? new Response(null, { status })
      : new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const client = () => new ApiClient("http://api.test");

const adr = {
  key: "adr",
  name: "Architecture decision",
  default_base_key: "engineering",
  template: "# Architecture decision title\n\n## Context\n\n## Decision\n\n## Consequences\n\n",
  review_days: 0,
  required_sections: ["Context", "Decision", "Consequences"],
};

describe("knowledge kind schemas", () => {
  it("parses the kinds with their required headings", () => {
    const ok = parseWithFallback<KnowledgeKinds>({ kinds: [adr] }, KnowledgeKindsSchema, FALLBACK_KNOWLEDGE_KINDS, {
      endpoint: "t",
    });
    expect(ok.kinds[0]).toMatchObject({ key: "adr", default_base_key: "engineering", required_sections: ["Context", "Decision", "Consequences"] });
  });

  it("reads a missing or null list of headings as none", () => {
    const { required_sections: _omitted, ...rest } = adr;
    expect(parseWithFallback<KnowledgeKind>(rest, KnowledgeKindSchema, FALLBACK_KNOWLEDGE_KIND, { endpoint: "t" })).toMatchObject({
      required_sections: [],
    });
    expect(
      parseWithFallback<KnowledgeKind>({ ...adr, required_sections: null }, KnowledgeKindSchema, FALLBACK_KNOWLEDGE_KIND, {
        endpoint: "t",
      }),
    ).toMatchObject({ required_sections: [] });
  });

  it("falls back for a malformed kinds response", () => {
    expect(
      parseWithFallback({ kinds: [{ key: 7 }] }, KnowledgeKindsSchema, FALLBACK_KNOWLEDGE_KINDS, { endpoint: "t" }),
    ).toEqual(FALLBACK_KNOWLEDGE_KINDS);
    expect(parseWithFallback("nope", KnowledgeKindsSchema, FALLBACK_KNOWLEDGE_KINDS, { endpoint: "t" })).toEqual(FALLBACK_KNOWLEDGE_KINDS);
  });
});

describe("publish policy schemas", () => {
  it("keeps a known mode and marks a mode it cannot read as unknown, never as manual or auto", () => {
    expect(
      parseWithFallback<KnowledgePublishPolicy>(
        { scope: "kind", key: "adr", mode: "auto_after_review", updated_at: "2026-11-02T00:00:00Z" },
        KnowledgePublishPolicySchema,
        FALLBACK_KNOWLEDGE_POLICY,
        { endpoint: "t" },
      ),
    ).toMatchObject({ scope: "kind", mode: "auto_after_review" });
    expect(
      parseWithFallback<KnowledgePublishPolicy>({ scope: "base", key: "x", mode: "sometimes" }, KnowledgePublishPolicySchema, FALLBACK_KNOWLEDGE_POLICY, {
        endpoint: "t",
      }).mode,
    ).toBe("unknown");
  });

  it("defaults the list's default mode to manual when an older server omits it", () => {
    const list = parseWithFallback<KnowledgePublishPolicies>({ policies: null }, KnowledgePublishPoliciesSchema, FALLBACK_KNOWLEDGE_POLICIES, {
      endpoint: "t",
    });
    expect(list).toEqual({ policies: [], default_mode: "manual" });
  });

  it("falls back for a malformed policy list", () => {
    expect(
      parseWithFallback({ policies: [{ scope: 3 }] }, KnowledgePublishPoliciesSchema, FALLBACK_KNOWLEDGE_POLICIES, { endpoint: "t" }),
    ).toEqual(FALLBACK_KNOWLEDGE_POLICIES);
  });
});

describe("librarian health schema", () => {
  it("parses each routine's latest report and its findings", () => {
    const health = parseWithFallback<KnowledgeHealth>(
      {
        jobs: [
          {
            job: "freshness",
            ran_at: "2026-11-02T00:00:00Z",
            report: {
              job: "freshness",
              ran_at: "2026-11-02T00:00:00Z",
              counts: { review_due: 2 },
              findings: [{ link_id: "l1", title: "Q3", reason: "review_due", knowledge_kind: "market_report" }],
              truncated: false,
            },
          },
        ],
      },
      KnowledgeHealthSchema,
      FALLBACK_KNOWLEDGE_HEALTH,
      { endpoint: "t" },
    );
    expect(health.jobs[0]!.report.counts).toEqual({ review_due: 2 });
    expect(health.jobs[0]!.report.findings[0]).toMatchObject({ link_id: "l1", missing_sections: [] });
  });

  it("reads a report with no counts as an empty tally", () => {
    const health = parseWithFallback<KnowledgeHealth>(
      { jobs: [{ job: "triage", report: {} }] },
      KnowledgeHealthSchema,
      FALLBACK_KNOWLEDGE_HEALTH,
      { endpoint: "t" },
    );
    expect(health.jobs[0]!.report).toMatchObject({ counts: {}, findings: [], truncated: false });
  });

  it("falls back for a malformed health response", () => {
    expect(
      parseWithFallback({ jobs: [{ job: "audit", report: { counts: { orphaned: "many" } } }] }, KnowledgeHealthSchema, FALLBACK_KNOWLEDGE_HEALTH, {
        endpoint: "t",
      }),
    ).toEqual(FALLBACK_KNOWLEDGE_HEALTH);
  });
});

describe("eval schemas", () => {
  it("reads an offline run as offline and an unknown state as unknown", () => {
    expect(parseWithFallback<KnowledgeEvalRun>({ state: "offline" }, KnowledgeEvalRunSchema, FALLBACK_KNOWLEDGE_EVAL_RUN, { endpoint: "t" })).toMatchObject({
      state: "offline",
      results: [],
    });
    expect(parseWithFallback<KnowledgeEvalRun>({ state: "maybe" }, KnowledgeEvalRunSchema, FALLBACK_KNOWLEDGE_EVAL_RUN, { endpoint: "t" }).state).toBe(
      "unknown",
    );
  });

  it("parses a recorded run with its per-case results", () => {
    const run = parseWithFallback<KnowledgeEvalRun>(
      {
        id: "r1",
        state: "ready",
        total: 2,
        hits: 1,
        hit_rate: 0.5,
        ran_at: "2026-11-02T00:00:00Z",
        results: [{ case_id: "c1", question: "q", expected_kind: "adr", hit: true, rank: 1 }],
      },
      KnowledgeEvalRunSchema,
      FALLBACK_KNOWLEDGE_EVAL_RUN,
      { endpoint: "t" },
    );
    expect(run).toMatchObject({ hit_rate: 0.5, hits: 1, total: 2 });
    expect(run.results[0]).toMatchObject({ case_id: "c1", hit: true, rank: 1 });
  });

  it("falls back for a malformed run list", () => {
    expect(
      parseWithFallback<KnowledgeEvalRuns>({ runs: [{ hit_rate: "half" }] }, KnowledgeEvalRunsSchema, FALLBACK_KNOWLEDGE_EVAL_RUNS, {
        endpoint: "t",
      }),
    ).toEqual(FALLBACK_KNOWLEDGE_EVAL_RUNS);
  });
});

describe("taxonomy client methods", () => {
  it("lists kinds and degrades an unreadable answer to none", async () => {
    respond({ kinds: [adr] });
    expect((await client().listKnowledgeKinds()).kinds[0]!.key).toBe("adr");
    respond({ kinds: "adr" });
    expect(await client().listKnowledgeKinds()).toEqual(FALLBACK_KNOWLEDGE_KINDS);
  });

  it("patches a kind by its key", async () => {
    const fetchMock = respond({ ...adr, review_days: 365 });
    const updated = await client().updateKnowledgeKind("adr", { review_days: 365 });
    const init = fetchMock.mock.calls[0]![1] as RequestInit;
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/kinds/adr");
    expect(init.method).toBe("PATCH");
    expect(JSON.parse(String(init.body))).toEqual({ review_days: 365 });
    expect(updated.review_days).toBe(365);
  });

  it("sets a policy with a PUT and removes one with a DELETE carrying scope and key", async () => {
    let fetchMock = respond({ scope: "base", key: "engineering", mode: "auto", updated_at: "" });
    const saved = await client().setKnowledgePolicy({ scope: "base", key: "engineering", mode: "auto" });
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("PUT");
    expect(saved.mode).toBe("auto");

    fetchMock = respond(null, 204);
    await client().deleteKnowledgePolicy("kind", "adr");
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/policies?scope=kind&key=adr");
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("DELETE");
  });

  it("posts a review with its verdict", async () => {
    const fetchMock = respond({ id: "d1", state: "draft", title: "x" });
    await client().reviewKnowledgeDocument("d1", { verdict: "changes", note: "cite numbers" });
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/documents/d1/review");
    expect(JSON.parse(String((fetchMock.mock.calls[0]![1] as RequestInit).body))).toEqual({ verdict: "changes", note: "cite numbers" });
  });

  it("runs the eval with a POST and reads the offline state", async () => {
    const fetchMock = respond({ state: "offline" });
    const run = await client().runKnowledgeEval();
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("POST");
    expect(run).toMatchObject({ state: "offline", total: 0 });
  });

  it("searches by kind", async () => {
    const fetchMock = respond({ state: "ready", hits: [] });
    await client().searchKnowledge({ q: "queues", kind: "adr" });
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/search?q=queues&kind=adr");
  });
});

describe("eval and health client methods", () => {
  it("lists eval cases and degrades a malformed case list to none", async () => {
    const good = { id: "c1", question: "Where is the refund policy?", expected_kind: "policy", created_by_type: "member", created_at: "2026-11-02T00:00:00Z" };
    respond({ cases: [good] });
    expect((await client().listKnowledgeEvalCases()).cases[0]).toMatchObject({ id: "c1", owner_squad_id: null });

    respond({ cases: [{ id: "c1", question: 7 }] });
    expect(await client().listKnowledgeEvalCases()).toEqual(FALLBACK_KNOWLEDGE_EVAL_CASES);
    respond("nope");
    expect(await client().listKnowledgeEvalCases()).toEqual(FALLBACK_KNOWLEDGE_EVAL_CASES);
  });

  it("reads the health report and degrades a malformed health response to no jobs", async () => {
    respond({ jobs: [{ job: "freshness", ran_at: "2026-11-02T00:00:00Z", report: { counts: { review_due: 1 } } }] });
    expect((await client().getKnowledgeHealth()).jobs[0]!.report.counts).toEqual({ review_due: 1 });

    respond({ jobs: [{ job: "audit", report: { counts: { orphaned: "many" } } }] });
    expect(await client().getKnowledgeHealth()).toEqual(FALLBACK_KNOWLEDGE_HEALTH);
  });

  it("lists eval runs and degrades a malformed run list to none", async () => {
    respond({ runs: [{ id: "r1", state: "ready", total: 2, hits: 1, hit_rate: 0.5, ran_at: "2026-11-02T00:00:00Z", results: [] }] });
    expect((await client().listKnowledgeEvalRuns()).runs[0]).toMatchObject({ id: "r1", hit_rate: 0.5 });

    respond({ runs: [{ hit_rate: "half" }] });
    expect(await client().listKnowledgeEvalRuns()).toEqual(FALLBACK_KNOWLEDGE_EVAL_RUNS);
  });

  it("deletes an eval case by its encoded id with a DELETE", async () => {
    const fetchMock = respond(null, 204);
    await client().deleteKnowledgeEvalCase("case/1");
    expect(String(fetchMock.mock.calls[0]![0])).toBe("http://api.test/api/knowledge/eval-cases/case%2F1");
    expect((fetchMock.mock.calls[0]![1] as RequestInit).method).toBe("DELETE");
  });
});

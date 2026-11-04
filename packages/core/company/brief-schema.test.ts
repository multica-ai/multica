// @vitest-environment node
import { describe, expect, it } from "vitest";
import { BriefListSchema, BriefSchema, FALLBACK_BRIEF, type Brief } from "./brief-schema";
import { parseWithFallback } from "../api/schema";
import { briefKeys } from "./briefs";

const full = {
  id: "b1",
  workspace_id: "w1",
  kind: "daily",
  period_start: "2026-11-01T00:00:00Z",
  period_end: "2026-11-02T00:00:00Z",
  title: "Daily brief",
  body: "## Headline\nNothing needs you today.",
  proposal_ids: ["p1", "p2"],
  created_by_type: "agent",
  created_by_id: "a1",
  created_at: "2026-11-02T08:00:00Z",
};

describe("BriefSchema", () => {
  it("parses a full server response", () => {
    const b = parseWithFallback(full, BriefSchema, FALLBACK_BRIEF, { endpoint: "t" });
    expect(b).toMatchObject({ id: "b1", kind: "daily", proposal_ids: ["p1", "p2"], created_by_type: "agent" });
  });

  it("degrades an unknown kind to 'unknown' without failing the row", () => {
    const b = parseWithFallback({ ...full, kind: "quarterly" }, BriefSchema, FALLBACK_BRIEF, { endpoint: "t" });
    expect(b.kind).toBe("unknown");
    expect(b.title).toBe("Daily brief");
  });

  it("treats a null or missing proposal_ids as an empty list", () => {
    const nulled = parseWithFallback({ ...full, proposal_ids: null }, BriefSchema, FALLBACK_BRIEF, { endpoint: "t" });
    expect(nulled.proposal_ids).toEqual([]);
    const missing = parseWithFallback({ id: "b1" }, BriefSchema, FALLBACK_BRIEF, { endpoint: "t" });
    expect(missing).toMatchObject({ id: "b1", kind: "unknown", proposal_ids: [], body: "" });
  });

  it("keeps fields a newer server adds", () => {
    const b = parseWithFallback({ ...full, language: "vi" }, BriefSchema, FALLBACK_BRIEF, { endpoint: "t" });
    expect((b as unknown as Record<string, unknown>).language).toBe("vi");
  });

  it("falls back on a malformed response", () => {
    expect(parseWithFallback("nope", BriefSchema, FALLBACK_BRIEF, { endpoint: "t" })).toEqual(FALLBACK_BRIEF);
    expect(parseWithFallback<unknown[]>({ error: "x" }, BriefListSchema, [], { endpoint: "t" })).toEqual([]);
  });

  it("parses a list", () => {
    const list = parseWithFallback<Brief[]>([full, { ...full, id: "b2" }], BriefListSchema, [], { endpoint: "t" });
    expect(list.map((b) => b.id)).toEqual(["b1", "b2"]);
  });
});

describe("briefKeys", () => {
  it("scopes every key by workspace and nests filtered lists under the list prefix", () => {
    expect(briefKeys.all("w1")).toEqual(["briefs", "w1"]);
    expect(briefKeys.listFiltered("w1", { kind: "weekly", limit: 5 }).slice(0, 3)).toEqual(briefKeys.list("w1"));
    expect(briefKeys.detail("w1", "b1")).toEqual(["briefs", "w1", "detail", "b1"]);
    expect(briefKeys.all("w1")).not.toEqual(briefKeys.all("w2"));
  });
});

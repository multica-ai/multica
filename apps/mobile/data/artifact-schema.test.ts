/**
 * Drift defense for GET /api/issues/:id/artifacts — the endpoint the Workpad
 * panel reads. Root CLAUDE.md "API Response Compatibility": every endpoint
 * ships a schema plus at least one malformed-response test, so a server-side
 * shape change degrades instead of white-screening the issue screen.
 */
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@/lib/parse-response";
import {
  EMPTY_WORKPAD_ARTIFACTS,
  WorkpadArtifactListSchema,
} from "./schemas";

const parse = (raw: unknown) =>
  parseWithFallback(raw, WorkpadArtifactListSchema, EMPTY_WORKPAD_ARTIFACTS, {
    endpoint: "GET /api/issues/:id/artifacts",
  });

const PLAN = {
  id: "art-1",
  kind: "plan",
  title: "Workpad",
  body: "- [x] Step one\n- [ ] Step two",
  updated_at: "2026-07-23T11:00:00Z",
};

describe("WorkpadArtifactListSchema", () => {
  it("parses a well-formed plan", () => {
    const [plan] = parse([PLAN]);
    expect(plan?.kind).toBe("plan");
    expect(plan?.body).toContain("Step one");
  });

  it("tolerates unknown server fields instead of rejecting the artifact", () => {
    const [plan] = parse([{ ...PLAN, some_new_field: 42 }]);
    expect(plan?.id).toBe("art-1");
    expect(plan?.kind).toBe("plan");
  });

  it("coalesces missing title/body to null rather than dropping the artifact", () => {
    const [plan] = parse([{ id: "art-1", kind: "plan" }]);
    expect(plan?.title).toBeNull();
    expect(plan?.body).toBeNull();
    expect(plan?.updated_at).toBe("");
  });

  it("keeps an unknown kind instead of failing — selection filters on plan", () => {
    const [artifact] = parse([{ ...PLAN, kind: "runbook" }]);
    expect(artifact?.kind).toBe("runbook");
  });

  it("falls back to an empty list when the body is not an array", () => {
    expect(parse({ artifacts: [PLAN] })).toEqual([]);
    expect(parse(null)).toEqual([]);
  });

  it("falls back to an empty list when an entry is missing its id", () => {
    expect(parse([{ ...PLAN, id: undefined }])).toEqual([]);
  });
});

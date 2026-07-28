import { describe, expect, it } from "vitest";
import { safeParsePlanVersions, planVersionKeys } from "./plan-versions";

describe("safeParsePlanVersions", () => {
  it("parses a well-formed array of versions", () => {
    const raw = [
      { id: "v2", version_no: 2, author_type: "agent", author_id: "a", created_at: "2026-02-01T00:00:00Z" },
      { id: "v1", version_no: 1, author_type: "member", author_id: "m", created_at: "2026-01-01T00:00:00Z" },
    ];
    expect(safeParsePlanVersions(raw)).toHaveLength(2);
    expect(safeParsePlanVersions(raw)[0]?.version_no).toBe(2);
  });

  it("returns [] for malformed / drifted shapes rather than throwing", () => {
    expect(safeParsePlanVersions(null)).toEqual([]);
    expect(safeParsePlanVersions(undefined)).toEqual([]);
    expect(safeParsePlanVersions({ versions: [] })).toEqual([]);
    expect(safeParsePlanVersions("nope")).toEqual([]);
    expect(safeParsePlanVersions([{ version_no: "not-a-number" }])).toEqual([]);
  });

  it("keys are workspace- and plan-scoped", () => {
    expect(planVersionKeys.forPlan("ws", "plan-1")).toEqual(["plan-versions", "ws", "plan-1"]);
    expect(planVersionKeys.forPlan("ws-a", "p")).not.toEqual(planVersionKeys.forPlan("ws-b", "p"));
  });
});

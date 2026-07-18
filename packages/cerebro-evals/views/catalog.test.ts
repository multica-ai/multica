import { describe, expect, it } from "vitest";
import { filterEvals, matchesSearch, STATUS_FILTERS, statusCounts } from "./catalog";
import type { CerebroEval, EvalStatus } from "../types";

function makeEval(overrides: Partial<CerebroEval> = {}): CerebroEval {
  return {
    id: "e1", workspace_id: "w1", key: "customer-service-quality", version: "1.0.0",
    title: "Customer-service reply quality", description: "", status: "draft",
    owner: {}, objective: "Answer accurately and on policy.",
    target: { kind: "agent", locator: "mention://agent/x", ref: "main" },
    datasets: [], graders: [], thresholds: [], runner: {}, source: {},
    created_by_id: "u1", created_by_type: "member",
    created_at: "2026-07-18T06:00:00Z", updated_at: "2026-07-18T06:00:00Z",
    ...overrides,
  };
}

describe("matchesSearch", () => {
  it("matches everything on an empty or whitespace needle", () => {
    expect(matchesSearch(makeEval(), "")).toBe(true);
    expect(matchesSearch(makeEval(), "   ")).toBe(true);
  });
  it("matches on title, key, objective and target kind, case-insensitively", () => {
    const item = makeEval();
    expect(matchesSearch(item, "REPLY quality")).toBe(true);
    expect(matchesSearch(item, "customer-service-quality")).toBe(true);
    expect(matchesSearch(item, "policy")).toBe(true);
    expect(matchesSearch(item, "agent")).toBe(true);
  });
  it("rejects a needle that appears in no searchable field", () => {
    expect(matchesSearch(makeEval(), "refund")).toBe(false);
  });
  it("does not throw when target kind is missing", () => {
    const item = makeEval({ target: {} });
    expect(matchesSearch(item, "agent")).toBe(false);
    expect(matchesSearch(item, "")).toBe(true);
  });
});

describe("filterEvals", () => {
  const evals = [
    makeEval({ id: "d", status: "draft", title: "Refund policy check" }),
    makeEval({ id: "a", status: "active", title: "Refund tone check" }),
    makeEval({ id: "p", status: "paused", title: "Product recommendation" }),
    makeEval({ id: "r", status: "retired", title: "Legacy format check" }),
  ];

  it("keeps every status on the all facet", () => {
    expect(filterEvals(evals, "all", "").map((e) => e.id)).toEqual(["d", "a", "p", "r"]);
  });
  it("keeps only the chosen status", () => {
    expect(filterEvals(evals, "active", "").map((e) => e.id)).toEqual(["a"]);
    expect(filterEvals(evals, "retired", "").map((e) => e.id)).toEqual(["r"]);
  });
  it("combines the status facet with the search needle", () => {
    expect(filterEvals(evals, "all", "refund").map((e) => e.id)).toEqual(["d", "a"]);
    expect(filterEvals(evals, "draft", "refund").map((e) => e.id)).toEqual(["d"]);
    expect(filterEvals(evals, "active", "recommendation")).toEqual([]);
  });
});

describe("statusCounts", () => {
  it("tallies each facet plus the all total", () => {
    const evals = [
      makeEval({ status: "draft" }),
      makeEval({ status: "draft" }),
      makeEval({ status: "active" }),
      makeEval({ status: "retired" }),
    ];
    expect(statusCounts(evals)).toEqual({ all: 4, draft: 2, active: 1, paused: 0, retired: 1 });
  });
  it("returns zeros for an empty catalog", () => {
    expect(statusCounts([])).toEqual({ all: 0, draft: 0, active: 0, paused: 0, retired: 0 });
  });
});

describe("STATUS_FILTERS", () => {
  it("lists the all facet plus every EvalStatus in lifecycle order", () => {
    const ALL_STATUSES: EvalStatus[] = ["draft", "active", "paused", "retired"];
    expect(STATUS_FILTERS).toEqual(["all", ...ALL_STATUSES]);
  });
});

import { beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({ cerebroRequest: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, cerebroRequest } };
});

import {
  fetchAnalyticsCatalog,
  fetchAnalyticsQuery,
  createAnalyticsVisual,
  fetchAnalyticsVisuals,
  fetchSkillUsage,
  fetchUsageExplorer,
  parseAnalyticsQueryResult,
  parseUsageExplorer,
} from "./api";

describe("canonical analytics API", () => {
  beforeEach(() => cerebroRequest.mockReset());

  it("queries the shared analytics endpoint without changing the contract", async () => {
    cerebroRequest.mockResolvedValue({
      columns: ["model", "runs", "cost_cents"],
      rows: [{ model: "gpt-5", runs: 4, cost_cents: 125 }],
      next_cursor: "2026-07-12T08:00:00Z",
    });
    const query = {
      population: "all" as const,
      metrics: ["runs" as const, "cost_cents" as const],
      dimensions: ["model" as const],
      page: { limit: 25 },
    };

    await expect(fetchAnalyticsQuery(query)).resolves.toMatchObject({
      columns: ["model", "runs", "cost_cents"],
      rows: [{ model: "gpt-5", runs: 4, cost_cents: 125 }],
    });
    expect(cerebroRequest).toHaveBeenCalledWith("/api/analytics/query", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(query),
    });
  });

  it("loads the server catalog used by visual configuration", async () => {
    cerebroRequest.mockResolvedValue({ populations: ["all"], metrics: ["runs"], dimensions: ["time"], grains: ["day"], operators: ["in"] });
    await expect(fetchAnalyticsCatalog()).resolves.toMatchObject({ metrics: ["runs"], dimensions: ["time"] });
    expect(cerebroRequest).toHaveBeenCalledWith("/api/analytics/catalog");
  });

  it("falls back safely when an older server returns malformed query data", () => {
    expect(parseAnalyticsQueryResult({ columns: null, rows: "broken" })).toEqual({ columns: [], rows: [] });
  });

  it("lists and creates persisted visuals", async () => {
    const visual = { id: "v1", name: "Cost by model", visual_type: "bars", query: { population: "all", metrics: ["cost_cents"], dimensions: ["model"] }, display: {}, position: 0, created_at: "2026-07-12T00:00:00Z", updated_at: "2026-07-12T00:00:00Z" };
    cerebroRequest.mockResolvedValueOnce([visual]).mockResolvedValueOnce(visual);
    await expect(fetchAnalyticsVisuals()).resolves.toHaveLength(1);
    await expect(createAnalyticsVisual({ name: visual.name, visual_type: "bars", query: visual.query as never, display: {}, position: 0 })).resolves.toMatchObject({ id: "v1" });
    expect(cerebroRequest).toHaveBeenLastCalledWith("/api/analytics/visuals", expect.objectContaining({ method: "POST" }));
  });
});

describe("fetchSkillUsage", () => {
  beforeEach(() => cerebroRequest.mockReset());

  it("parses valid rows", async () => {
    cerebroRequest.mockResolvedValue([
      { skill_id: "s1", skill_name: "TDD", invocation_count: 4, run_count: 2, last_used_at: "2026-07-10T08:00:00Z" },
    ]);
    await expect(fetchSkillUsage(30, null)).resolves.toEqual([
      { skill_id: "s1", skill_name: "TDD", invocation_count: 4, run_count: 2, last_used_at: "2026-07-10T08:00:00Z" },
    ]);
  });

  it("falls back safely when an older server returns a malformed response", async () => {
    cerebroRequest.mockResolvedValue({ rows: null });
    await expect(fetchSkillUsage(30, null)).resolves.toEqual([]);
  });

  it("sends include and exclude skill filters", async () => {
    cerebroRequest.mockResolvedValue([]);
    await fetchSkillUsage(30, null, ["TDD"], ["Legacy"]);
    expect(cerebroRequest).toHaveBeenCalledWith(
      "/api/dashboard/usage/skills?days=30&skill=TDD&exclude.skill=Legacy",
    );
  });
});

describe("fetchUsageExplorer", () => {
  beforeEach(() => cerebroRequest.mockReset());

  it("normalizes the dashboard grain to the API vocabulary", async () => {
    cerebroRequest.mockResolvedValue({
      summary: { runs: 0, tokens: 0, actual_cost_cents: 0, calculated_cost_runs: 0, missing_cost_runs: 0 },
      facets: {}, runs: [], total: 0, savings: [],
    });

    await fetchUsageExplorer("days=30&grain=day&limit=50");

    expect(cerebroRequest).toHaveBeenCalledWith(
      "/api/dashboard/usage/explorer?days=30&grain=daily&limit=50",
    );
  });
});

it("falls back safely for a malformed explorer response", () => {
  expect(parseUsageExplorer({ summary: null, runs: "broken" })).toEqual({
    summary: { runs: 0, tokens: 0, actual_cost_cents: 0, calculated_cost_runs: 0, missing_cost_runs: 0 },
    facets: {}, runs: [], total: 0, savings: [],
  });
});

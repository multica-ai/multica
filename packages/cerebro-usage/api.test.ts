import { beforeEach, describe, expect, it, vi } from "vitest";

const { cerebroRequest } = vi.hoisted(() => ({ cerebroRequest: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, cerebroRequest } };
});

import { fetchSkillUsage, fetchUsageExplorer } from "./api";
import { parseUsageExplorer } from "./api";

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

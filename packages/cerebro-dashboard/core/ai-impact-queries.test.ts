import { beforeEach, describe, expect, it, vi } from "vitest";

const { fetchOverview, fetchFunctions, fetchQualityRisk, fetchPeople } = vi.hoisted(() => ({
  fetchOverview: vi.fn(),
  fetchFunctions: vi.fn(),
  fetchQualityRisk: vi.fn(),
  fetchPeople: vi.fn(),
}));

vi.mock("./ai-impact-api", () => ({
  fetchAIImpactOverview: fetchOverview,
  fetchAIImpactFunctions: fetchFunctions,
  fetchAIImpactQualityRisk: fetchQualityRisk,
  fetchAIImpactPeople: fetchPeople,
}));

import {
  aiImpactFunctionsOptions,
  aiImpactOverviewOptions,
  aiImpactPeopleOptions,
  aiImpactQualityRiskOptions,
  dashboardKeys,
} from "./queries";

describe("AI Impact dashboard queries", () => {
  beforeEach(() => {
    fetchOverview.mockReset();
    fetchFunctions.mockReset();
    fetchQualityRisk.mockReset();
    fetchPeople.mockReset();
  });

  it("keeps each read model in a workspace-scoped cache entry", async () => {
    const overview = aiImpactOverviewOptions("workspace-1");
    const functions = aiImpactFunctionsOptions("workspace-1");
    const qualityRisk = aiImpactQualityRiskOptions("workspace-1");
    const people = aiImpactPeopleOptions("workspace-1", "month");

    expect(overview.queryKey).toEqual([
      "cerebro",
      "dashboard",
      "workspace-1",
      "ai-impact",
      "overview",
    ]);
    expect(functions.queryKey).toEqual([
      "cerebro",
      "dashboard",
      "workspace-1",
      "ai-impact",
      "functions",
    ]);
    expect(qualityRisk.queryKey).toEqual([
      "cerebro",
      "dashboard",
      "workspace-1",
      "ai-impact",
      "quality-risk",
    ]);
    expect(dashboardKeys.aiImpact("workspace-1")).toEqual([
      "cerebro",
      "dashboard",
      "workspace-1",
      "ai-impact",
    ]);
    expect(people.queryKey).toEqual([
      "cerebro",
      "dashboard",
      "workspace-1",
      "ai-impact",
      "people",
      "month",
    ]);

    await overview.queryFn?.({} as never);
    await functions.queryFn?.({} as never);
    await qualityRisk.queryFn?.({} as never);
    await people.queryFn?.({} as never);

    expect(fetchOverview).toHaveBeenCalledOnce();
    expect(fetchFunctions).toHaveBeenCalledOnce();
    expect(fetchQualityRisk).toHaveBeenCalledOnce();
    expect(fetchPeople).toHaveBeenCalledWith("month");
    expect(aiImpactOverviewOptions("").enabled).toBe(false);
    expect(aiImpactFunctionsOptions("").enabled).toBe(false);
    expect(aiImpactQualityRiskOptions("").enabled).toBe(false);
    expect(aiImpactPeopleOptions("", "month").enabled).toBe(false);
  });
});

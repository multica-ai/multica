import { describe, expect, it } from "vitest";
import type { DashboardFilterState } from "./filter-state";
import {
  DASHBOARD_FILTER_CAPABILITIES,
  DASHBOARD_PANEL_FILTER_ADAPTERS,
  selectAIImpactAdapterFilters,
  selectAnalyticsAdapterFilters,
  selectDashboardMessagesAdapterFilters,
  selectDashboardOverviewAdapterFilters,
  selectPeopleAdapterFilters,
} from "./filter-capabilities";

const state: DashboardFilterState = {
  range: "7d",
  exactTimeRange: {
    start: "2026-07-13T00:00:00.000Z",
    end: "2026-07-14T00:00:00.000Z",
  },
  scope: "agents",
  actorId: "agent-1",
  actorName: "Lone",
  analyticsFilters: [
    { dimension: "agent", operator: "in", values: ["Lone"] },
    { dimension: "issue", operator: "in", values: ["FIR-2996"] },
    { dimension: "time", operator: "gte", values: ["2026-07-13T00:00:00.000Z"] },
    { dimension: "time", operator: "lte", values: ["2026-07-14T00:00:00.000Z"] },
  ],
  aiImpactSelections: {
    functionName: "Operations",
    operatingLoop: "Delivery",
    decision: "Scale",
    personType: "agent",
    personId: "agent-1",
  },
};

describe("dashboard filter capabilities", () => {
  it("declares the adapters used by every Dashboard panel", () => {
    expect(DASHBOARD_PANEL_FILTER_ADAPTERS).toEqual({
      overview: ["dashboardOverview", "analytics"],
      runs: ["analytics"],
      messages: ["dashboardOverview", "dashboardMessages", "analytics"],
      "ai-impact": ["aiImpact"],
      people: ["people"],
    });
  });

  it("keeps analytics filters inside the declared analytics dimension contract", () => {
    expect(DASHBOARD_FILTER_CAPABILITIES.analytics).toMatchObject({
      time: ["exact"],
      actor: [],
      aiImpact: [],
    });
    expect(selectAnalyticsAdapterFilters(state)).toEqual({
      supported: true,
      filters: state.analyticsFilters,
    });
  });

  it("adapts shared state to the existing Dashboard overview and message contracts", () => {
    const expectedFilters = {
      range: "7d",
      scope: "agents",
      actorId: "agent-1",
      exactTimeRange: {
        start: "2026-07-13T00:00:00.000Z",
        end: "2026-07-14T00:00:00.000Z",
      },
    };
    expect(selectDashboardOverviewAdapterFilters(state)).toEqual({
      supported: true,
      filters: expectedFilters,
    });
    expect(selectDashboardMessagesAdapterFilters(state)).toEqual({
      supported: true,
      filters: expectedFilters,
    });
  });

  it("does not silently pass unsupported shared filters to AI Impact or People", () => {
    expect(selectAIImpactAdapterFilters(state)).toEqual({
      supported: false,
      filters: null,
      reason: "Shared Dashboard filters are not supported by the current AI Impact API.",
    });
    expect(selectPeopleAdapterFilters(state)).toEqual({
      supported: false,
      filters: null,
      reason: "Shared Dashboard filters are not supported by the current People API.",
    });
  });
});

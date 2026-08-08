import { describe, expect, it } from "vitest";
import {
  addDashboardFilter,
  clearDashboardFilters,
  DEFAULT_DASHBOARD_FILTER_STATE,
  parseDashboardFilterState,
  removeDashboardFilter,
  replaceDashboardTimeRange,
  serializeDashboardFilterState,
} from "./filter-state";
import { useDashboardStore } from "./store";

describe("dashboard filter state", () => {
  it("parses the previously split Dashboard state from one backwards-compatible URL", () => {
    const state = parseDashboardFilterState(
      new URLSearchParams(
        "tab=runs&range=7d&dashboard.scope=members&dashboard.actor=member-1&dashboard.actor_name=Alex&provider=openai&exclude.status=failed&model.contains=gpt&time.gte=2026-07-13T00%3A00%3A00.000Z&time.lte=2026-07-14T00%3A00%3A00.000Z&ai.function=Support",
      ),
    );

    expect(state).toEqual({
      range: "7d",
      scope: "members",
      actorId: "member-1",
      actorName: "Alex",
      exactTimeRange: {
        start: "2026-07-13T00:00:00.000Z",
        end: "2026-07-14T00:00:00.000Z",
      },
      analyticsFilters: [
        { dimension: "time", operator: "gte", values: ["2026-07-13T00:00:00.000Z"] },
        { dimension: "time", operator: "lte", values: ["2026-07-14T00:00:00.000Z"] },
        { dimension: "provider", operator: "in", values: ["openai"] },
        { dimension: "model", operator: "contains", values: ["gpt"] },
        { dimension: "status", operator: "not_in", values: ["failed"] },
        { dimension: "person", operator: "in", values: ["Alex"] },
      ],
      aiImpactSelections: {
        functionName: "Support",
        operatingLoop: null,
        decision: null,
        personType: null,
        personId: null,
      },
    });
  });

  it("keeps every mutation inside the typed state and round-trips one clear and reload", () => {
    let state = addDashboardFilter(DEFAULT_DASHBOARD_FILTER_STATE, "provider", "openai", "in");
    state = replaceDashboardTimeRange(
      state,
      "2026-07-13T00:00:00.000Z",
      "2026-07-14T00:00:00.000Z",
    );
    state = removeDashboardFilter(state, "provider", "openai", "in");

    const cleared = clearDashboardFilters(state);
    const params = serializeDashboardFilterState(
      cleared,
      new URLSearchParams("tab=runs&provider=stale&time.gte=stale"),
    );

    expect(params.toString()).toBe("tab=runs&range=30d");
    expect(parseDashboardFilterState(params)).toEqual(DEFAULT_DASHBOARD_FILTER_STATE);
  });

  it("keeps actor, range, and analytics filters in the shared Dashboard store", () => {
    useDashboardStore.getState().reset();
    useDashboardStore.getState().setRange("7d");
    useDashboardStore.getState().setActor("agent-1", "Lone", "agent");
    useDashboardStore.getState().setAnalyticsFilters((filters) =>
      addDashboardFilter(
        { ...useDashboardStore.getState(), analyticsFilters: filters },
        "status",
        "failed",
        "not_in",
      ).analyticsFilters,
    );

    expect(useDashboardStore.getState()).toMatchObject({
      range: "7d",
      scope: "agents",
      actorId: "agent-1",
      actorName: "Lone",
      analyticsFilters: [
        { dimension: "agent", operator: "in", values: ["Lone"] },
        { dimension: "status", operator: "not_in", values: ["failed"] },
      ],
    });

    useDashboardStore.getState().clearFilters();
    expect(useDashboardStore.getState()).toMatchObject(DEFAULT_DASHBOARD_FILTER_STATE);
  });
});

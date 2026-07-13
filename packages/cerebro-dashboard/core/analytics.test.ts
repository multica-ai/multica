import { describe, expect, it } from "vitest";
import {
  buildVisualQuery,
  DEFAULT_ANALYTICS_VISUALS,
  filtersFromSearchParams,
  filtersToSearchParams,
  presentationToVisualKind,
  toggleAnalyticsFilter,
  visualPresentationFromDisplay,
} from "./analytics";

describe("dashboard analytics model", () => {
  it("builds every visual from the canonical analytics contract", () => {
    expect(buildVisualQuery(DEFAULT_ANALYTICS_VISUALS[0]!, [], "Europe/Copenhagen")).toEqual({
      population: "all",
      metrics: ["runs", "cost_cents", "saved_cents"],
      dimensions: ["time"],
      grain: "day",
      filters: [],
      sort: [{ field: "time", direction: "desc" }],
      page: { limit: 42 },
      timezone: "Europe/Copenhagen",
    });
  });

  it("applies shared include and exclude filters to every visual", () => {
    let filters = toggleAnalyticsFilter([], "provider", "openai", "in");
    filters = toggleAnalyticsFilter(filters, "status", "failed", "not_in");
    expect(buildVisualQuery(DEFAULT_ANALYTICS_VISUALS[1]!, filters, "UTC").filters).toEqual([
      { dimension: "provider", operator: "in", values: ["openai"] },
      { dimension: "status", operator: "not_in", values: ["failed"] },
    ]);
  });

  it("removes a selected filter when toggled again", () => {
    const selected = toggleAnalyticsFilter([], "model", "gpt-5", "in");
    expect(toggleAnalyticsFilter(selected, "model", "gpt-5", "in")).toEqual([]);
  });

  it("round-trips shared filters through the dashboard URL", () => {
    const filters = [
      { dimension: "provider" as const, operator: "in" as const, values: ["openai"] },
      { dimension: "status" as const, operator: "not_in" as const, values: ["failed"] },
    ];
    const params = filtersToSearchParams(filters, new URLSearchParams("tab=runs"));
    expect(params.toString()).toBe("tab=runs&provider=openai&exclude.status=failed");
    expect(filtersFromSearchParams(params)).toEqual(filters);
  });

  it("maps every visual builder presentation onto the persisted visual contract", () => {
    expect(presentationToVisualKind("line")).toBe("bars");
    expect(presentationToVisualKind("activity")).toBe("activity");
    expect(presentationToVisualKind("stacked")).toBe("bars");
    expect(presentationToVisualKind("table")).toBe("table");
    expect(presentationToVisualKind("metric")).toBe("table");
    expect(visualPresentationFromDisplay({ presentation: "metric" }, "table")).toBe("metric");
    expect(visualPresentationFromDisplay({ presentation: "unknown" }, "activity")).toBe("activity");
  });
});

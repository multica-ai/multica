import { describe, expect, it } from "vitest";
import {
  filtersFromSearchParams,
  filtersToSearchParams,
  removeAnalyticsFilterValue,
  replaceAnalyticsTimeBucket,
} from "./analytics";

describe("analytics range filters", () => {
  it("round-trips time range filters without dropping regular include filters", () => {
    const filters = filtersFromSearchParams(
      new URLSearchParams("source=issue&time.gte=2026-07-13T00%3A00%3A00.000Z&time.lte=2026-07-14T00%3A00%3A00.000Z"),
    );

    expect(filters).toEqual([
      { dimension: "time", operator: "gte", values: ["2026-07-13T00:00:00.000Z"] },
      { dimension: "time", operator: "lte", values: ["2026-07-14T00:00:00.000Z"] },
      { dimension: "source", operator: "in", values: ["issue"] },
    ]);
    expect(filtersToSearchParams(filters).toString()).toBe(
      "time.gte=2026-07-13T00%3A00%3A00.000Z&time.lte=2026-07-14T00%3A00%3A00.000Z&source=issue",
    );
  });

  it("removes one range filter chip at a time", () => {
    expect(
      removeAnalyticsFilterValue(
        [
          { dimension: "time", operator: "gte", values: ["2026-07-13T00:00:00.000Z"] },
          { dimension: "source", operator: "in", values: ["issue"] },
        ],
        "time",
        "2026-07-13T00:00:00.000Z",
        "gte",
      ),
    ).toEqual([{ dimension: "source", operator: "in", values: ["issue"] }]);
  });

  it("replaces prior time bounds with an exact daily bucket", () => {
    const start = new Date(2026, 6, 13);
    const end = new Date(start);
    end.setDate(end.getDate() + 1);

    expect(replaceAnalyticsTimeBucket([
      { dimension: "provider", operator: "in", values: ["openai"] },
      { dimension: "time", operator: "gte", values: ["2026-07-01T00:00:00.000Z"] },
      { dimension: "time", operator: "lte", values: ["2026-07-31T00:00:00.000Z"] },
    ], "2026-07-13T00:00:00Z", "day")).toEqual([
      { dimension: "provider", operator: "in", values: ["openai"] },
      { dimension: "time", operator: "gte", values: [start.toISOString()] },
      { dimension: "time", operator: "lte", values: [end.toISOString()] },
    ]);
  });
});

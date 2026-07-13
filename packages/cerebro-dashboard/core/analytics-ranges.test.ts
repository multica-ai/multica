import { describe, expect, it } from "vitest";
import {
  filtersFromSearchParams,
  filtersToSearchParams,
  removeAnalyticsFilterValue,
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
});

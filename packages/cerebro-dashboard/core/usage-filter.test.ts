import { describe, expect, it } from "vitest";
import {
  parseUsageFilter,
  serializeUsageFilter,
  toggleUsageFilterValue,
  type UsageFilter,
} from "./usage-filter";

describe("usage filter contract", () => {
  it("round-trips include and exclude values in stable order", () => {
    const filter: UsageFilter = {
      days: 30,
      group: "daily",
      include: { model: ["gpt-5", "claude"], skill: ["TDD"] },
      exclude: { status: ["failed"], provider: ["unknown"] },
    };

    const query = serializeUsageFilter(filter);
    expect(query).toBe(
      "days=30&grain=daily&model=claude&model=gpt-5&skill=TDD&exclude.provider=unknown&exclude.status=failed",
    );
    expect(parseUsageFilter(new URLSearchParams(query))).toEqual({
      ...filter,
      include: { model: ["claude", "gpt-5"], skill: ["TDD"] },
      exclude: { provider: ["unknown"], status: ["failed"] },
    });
  });

  it("toggles a selected value off and keeps include/exclude mutually exclusive", () => {
    const initial = parseUsageFilter(new URLSearchParams("model=gpt-5"));
    const excluded = toggleUsageFilterValue(initial, "model", "gpt-5", "exclude");
    expect(excluded.include.model).toBeUndefined();
    expect(excluded.exclude.model).toEqual(["gpt-5"]);
    expect(toggleUsageFilterValue(excluded, "model", "gpt-5", "exclude").exclude.model).toBeUndefined();
  });

  it("preserves unknown future trigger and saving values", () => {
    const parsed = parseUsageFilter(
      new URLSearchParams("trigger=future-source&saving=graphify-v2"),
    );
    expect(parsed.include.trigger).toEqual(["future-source"]);
    expect(parsed.include.saving).toEqual(["graphify-v2"]);
  });
});

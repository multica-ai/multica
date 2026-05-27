import { describe, expect, it } from "vitest";
import {
  estimatedValue,
  formatUsd,
  parseDashboardResponse,
  type DashboardSaving,
} from "./dashboard";

function saving(overrides: Partial<DashboardSaving>): DashboardSaving {
  return {
    savingKey: "model_routing",
    metric: "model_cost",
    shadowRunCount: 0,
    treatmentRunCount: 0,
    controlRunCount: 0,
    estimatedSavedUnits: 0,
    estimatedSavedCents: 0,
    measured: null,
    ...overrides,
  };
}

describe("parseDashboardResponse", () => {
  it("parses a well-formed response", () => {
    const got = parseDashboardResponse({
      savings: [
        {
          saving_key: "model_routing",
          metric: "model_cost",
          shadow_run_count: 1,
          treatment_run_count: 8,
          control_run_count: 2,
          estimated_saved_units: 4400,
          estimated_saved_cents: 4400,
          measured: {
            treatment_avg_cost_cents: 100,
            control_avg_cost_cents: 300,
            saved_per_run_cents: 200,
            total_saved_cents: 1600,
          },
        },
      ],
    });
    expect(got).toHaveLength(1);
    expect(got[0]).toMatchObject({
      savingKey: "model_routing",
      treatmentRunCount: 8,
      controlRunCount: 2,
      estimatedSavedCents: 4400,
    });
    expect(got[0]!.measured?.totalSavedCents).toBe(1600);
  });

  it("returns [] for a non-object or missing savings array", () => {
    expect(parseDashboardResponse(null)).toEqual([]);
    expect(parseDashboardResponse("nope")).toEqual([]);
    expect(parseDashboardResponse({})).toEqual([]);
    expect(parseDashboardResponse({ savings: "not-an-array" })).toEqual([]);
  });

  it("drops unknown saving keys (server newer than this build)", () => {
    const got = parseDashboardResponse({
      savings: [
        { saving_key: "future_saving", metric: "model_cost" },
        { saving_key: "bundled_read", metric: "platform_calls" },
      ],
    });
    expect(got.map((s) => s.savingKey)).toEqual(["bundled_read"]);
  });

  it("defaults missing/wrong-typed fields instead of throwing", () => {
    const got = parseDashboardResponse({
      savings: [
        {
          saving_key: "snapshot_prompt",
          // metric, counts and measured all missing or wrong type
          estimated_saved_units: "12",
          measured: "not-an-object",
        },
      ],
    });
    expect(got[0]!).toMatchObject({
      savingKey: "snapshot_prompt",
      estimatedSavedUnits: 0, // string coerced to default
      shadowRunCount: 0,
      measured: null,
    });
  });

  it("treats a null measured block as no A/B", () => {
    const got = parseDashboardResponse({
      savings: [
        { saving_key: "model_routing", metric: "model_cost", measured: null },
      ],
    });
    expect(got[0]!.measured).toBeNull();
  });
});

describe("formatUsd", () => {
  it("formats cents as dollars", () => {
    expect(formatUsd(12345)).toBe("$123.45");
    expect(formatUsd(0)).toBe("$0.00");
    expect(formatUsd(-200)).toBe("-$2.00");
  });
});

describe("estimatedValue", () => {
  it("renders model_cost as dollars for every sign", () => {
    expect(
      estimatedValue(saving({ estimatedSavedCents: 4400, estimatedSavedUnits: 4400 })),
    ).toBe("$44.00");
    // Zero and negative model_cost must still read as money, not "0 model cost"
    // / "-200 model cost" (the bug: the saving cost more than the baseline).
    expect(estimatedValue(saving({ estimatedSavedCents: 0, estimatedSavedUnits: 0 }))).toBe(
      "$0.00",
    );
    expect(
      estimatedValue(saving({ estimatedSavedCents: -200, estimatedSavedUnits: -200 })),
    ).toBe("-$2.00");
  });

  it("renders unpriced metrics in native units", () => {
    expect(
      estimatedValue(
        saving({
          savingKey: "prune_tool_results",
          metric: "context_tokens",
          estimatedSavedCents: 0,
          estimatedSavedUnits: 1500,
        }),
      ),
    ).toBe("1,500 tokens");
  });

  it("prices a positive non-model_cost metric in dollars when available", () => {
    expect(
      estimatedValue(
        saving({
          savingKey: "prune_tool_results",
          metric: "context_tokens",
          estimatedSavedCents: 320,
          estimatedSavedUnits: 1500,
        }),
      ),
    ).toBe("$3.20");
  });
});

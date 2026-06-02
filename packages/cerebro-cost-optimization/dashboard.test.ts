import { describe, expect, it } from "vitest";
import {
  appliedValue,
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
    applied: null,
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
          estimated_saved_units: 400,
          estimated_saved_cents: 400,
          applied: {
            saved_units: 4000,
            saved_cents: 4000,
            run_count: 8,
          },
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
      estimatedSavedCents: 400,
    });
    expect(got[0]!.applied?.savedCents).toBe(4000);
    expect(got[0]!.applied?.runCount).toBe(8);
    expect(got[0]!.measured?.totalSavedCents).toBe(1600);
  });

  it("treats a null or non-object applied block as no applied saving", () => {
    const got = parseDashboardResponse({
      savings: [
        { saving_key: "prune_tool_results", metric: "context_tokens", applied: null },
        { saving_key: "model_routing", metric: "model_cost", applied: "nope" },
      ],
    });
    expect(got[0]!.applied).toBeNull();
    expect(got[1]!.applied).toBeNull();
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

  it("leads with the token count and shows the price alongside (prune, priced)", () => {
    // FIR-2639: the saved context is a token quantity — the card must show the
    // token number, not hide it behind a bare dollar amount.
    expect(
      estimatedValue(
        saving({
          savingKey: "prune_tool_results",
          metric: "context_tokens",
          estimatedSavedCents: 320,
          estimatedSavedUnits: 1500,
        }),
      ),
    ).toBe("1,500 tokens ($3.20)");
  });
});

describe("appliedValue", () => {
  it("returns '—' when no run has applied the saving", () => {
    expect(appliedValue(saving({ applied: null }))).toBe("—");
  });

  it("shows saved tokens with the price at 100% rollout (prune, priced)", () => {
    // FIR-2572: prune on for everyone, no holdout — the measured number must
    // still render, not '—'. FIR-2639: lead with the token count so the saved
    // context is a clear token figure, with the dollar value alongside.
    expect(
      appliedValue(
        saving({
          savingKey: "prune_tool_results",
          metric: "context_tokens",
          applied: { savedUnits: 9000, savedCents: 450, runCount: 20 },
        }),
      ),
    ).toBe("9,000 tokens ($4.50)");
  });

  it("shows estimated tokens for a legacy call-based saving", () => {
    expect(
      appliedValue(
        saving({
          savingKey: "bundled_read",
          metric: "platform_calls",
          applied: { savedUnits: 12, savedCents: 0, runCount: 4 },
        }),
      ),
    ).toBe("9,600 tokens (est. from 12 avoided calls)");
  });
});

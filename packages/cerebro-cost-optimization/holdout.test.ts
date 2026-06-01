import { describe, expect, it } from "vitest";
import { parseHoldoutResponse } from "./holdout";
import {
  clampHoldoutPct,
  DEFAULT_COST_SAVING_HOLDOUT_PCT,
  isFullRollout,
  savingSupportsHoldout,
} from "./registry";

describe("parseHoldoutResponse", () => {
  it("reads valid per-saving overrides", () => {
    expect(
      parseHoldoutResponse({
        overrides: { model_routing: 0, prune_tool_results: 30 },
      }),
    ).toEqual({ model_routing: 0, prune_tool_results: 30 });
  });

  it("returns an empty map when there are no overrides", () => {
    expect(parseHoldoutResponse({ overrides: {} })).toEqual({});
    expect(parseHoldoutResponse({})).toEqual({});
  });

  it("clamps out-of-range numbers into 0-100", () => {
    expect(
      parseHoldoutResponse({
        overrides: { model_routing: -10, prune_tool_results: 250 },
      }),
    ).toEqual({ model_routing: 0, prune_tool_results: 100 });
    expect(parseHoldoutResponse({ overrides: { model_routing: 33.7 } })).toEqual(
      { model_routing: 34 },
    );
  });

  it("drops unknown saving keys and malformed values", () => {
    expect(
      parseHoldoutResponse({
        overrides: {
          model_routing: 20,
          not_a_saving: 50,
          prune_tool_results: "30",
        },
      }),
    ).toEqual({ model_routing: 20 });
  });

  it("degrades a malformed shape to an empty map instead of throwing", () => {
    expect(parseHoldoutResponse(null)).toEqual({});
    expect(parseHoldoutResponse(undefined)).toEqual({});
    expect(parseHoldoutResponse("nope")).toEqual({});
    expect(parseHoldoutResponse([1, 2, 3])).toEqual({});
    expect(parseHoldoutResponse({ overrides: null })).toEqual({});
    expect(parseHoldoutResponse({ overrides: "x" })).toEqual({});
  });
});

describe("clampHoldoutPct", () => {
  it("rounds and clamps to 0-100", () => {
    expect(clampHoldoutPct(50)).toBe(50);
    expect(clampHoldoutPct(-1)).toBe(0);
    expect(clampHoldoutPct(101)).toBe(100);
    expect(clampHoldoutPct(12.4)).toBe(12);
  });

  it("falls back to the registry default on non-finite input", () => {
    expect(clampHoldoutPct(Number.NaN)).toBe(DEFAULT_COST_SAVING_HOLDOUT_PCT);
    expect(clampHoldoutPct(Number.POSITIVE_INFINITY)).toBe(
      DEFAULT_COST_SAVING_HOLDOUT_PCT,
    );
  });
});

describe("isFullRollout", () => {
  it("is true only at holdout 0 (no control arm)", () => {
    expect(isFullRollout(0)).toBe(true);
    expect(isFullRollout(20)).toBe(false);
    expect(isFullRollout(100)).toBe(false);
  });
});

describe("savingSupportsHoldout", () => {
  it("is true only for savings with a runtime control arm", () => {
    expect(savingSupportsHoldout("model_routing")).toBe(true);
    expect(savingSupportsHoldout("prune_tool_results")).toBe(true);
    expect(savingSupportsHoldout("snapshot_prompt")).toBe(false);
    expect(savingSupportsHoldout("bundled_read")).toBe(false);
  });
});

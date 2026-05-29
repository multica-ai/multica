import { describe, expect, it } from "vitest";
import {
  COST_SAVINGS,
  COST_SAVING_DEFAULTS,
  isDefaultMode,
  runtimeScopeBadge,
  runtimeScopeNote,
  type CostSavingKey,
} from "./registry";

describe("cost-saving registry", () => {
  it("has a definition for every default key", () => {
    const defined = new Set(COST_SAVINGS.map((s) => s.key));
    const keys = Object.keys(COST_SAVING_DEFAULTS) as CostSavingKey[];
    for (const key of keys) {
      expect(defined.has(key)).toBe(true);
    }
    expect(COST_SAVINGS.length).toBe(keys.length);
  });

  it("defaults every saving to off (opt-in, no prod behavior change)", () => {
    for (const mode of Object.values(COST_SAVING_DEFAULTS)) {
      expect(mode).toBe("off");
    }
  });

  it("has no duplicate keys", () => {
    const keys = COST_SAVINGS.map((s) => s.key);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("isDefaultMode is true only for a saving's registry default", () => {
    // Every default is currently "off"; selecting "off" clears the override,
    // selecting "shadow" or "on" writes it.
    for (const key of Object.keys(COST_SAVING_DEFAULTS) as CostSavingKey[]) {
      expect(isDefaultMode(key, COST_SAVING_DEFAULTS[key])).toBe(true);
      expect(isDefaultMode(key, "shadow")).toBe(false);
      expect(isDefaultMode(key, "on")).toBe(false);
    }
  });

  it("declares a runtime scope for every saving", () => {
    const valid = new Set(["daemon", "gateway", "both"]);
    for (const def of COST_SAVINGS) {
      expect(valid.has(def.runtimeScope)).toBe(true);
    }
  });

  it("marks model_routing and prune_tool_results as gateway-only", () => {
    // These two only change cost in the gateway runtime; the UI must badge them
    // so a daemon-only workspace is not shown a dead toggle. snapshot_prompt and
    // bundled_read apply in both runtimes, so they carry no scope badge.
    const scope = (key: CostSavingKey) =>
      COST_SAVINGS.find((s) => s.key === key)?.runtimeScope;
    expect(scope("model_routing")).toBe("gateway");
    expect(scope("prune_tool_results")).toBe("gateway");
    expect(scope("snapshot_prompt")).toBe("both");
    expect(scope("bundled_read")).toBe("both");
  });

  it("renders a badge + note only for runtime-scoped savings", () => {
    expect(runtimeScopeBadge("gateway")).toBeTruthy();
    expect(runtimeScopeNote("gateway")).toBeTruthy();
    expect(runtimeScopeBadge("daemon")).toBeTruthy();
    expect(runtimeScopeBadge("both")).toBeNull();
    expect(runtimeScopeNote("both")).toBeNull();
  });
});

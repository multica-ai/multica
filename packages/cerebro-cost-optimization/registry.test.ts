import { describe, expect, it } from "vitest";
import {
  COST_SAVINGS,
  COST_SAVING_DEFAULTS,
  isDefaultMode,
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
});

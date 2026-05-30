import { describe, expect, it } from "vitest";
import {
  CEREBRO_FLAGS,
  CEREBRO_FLAG_GROUPS,
  CEREBRO_FLAG_DEFAULTS,
  flagsForGroup,
} from "./registry";

describe("cerebro feature flag grouping", () => {
  const groupKeys = new Set(CEREBRO_FLAG_GROUPS.map((g) => g.key));

  it("assigns every flag to a defined group", () => {
    for (const flag of CEREBRO_FLAGS) {
      expect(groupKeys.has(flag.group), `flag ${flag.key} → unknown group ${flag.group}`).toBe(true);
    }
  });

  it("has no empty groups (every group owns at least one flag)", () => {
    for (const group of CEREBRO_FLAG_GROUPS) {
      expect(flagsForGroup(group.key).length, `group ${group.key} is empty`).toBeGreaterThan(0);
    }
  });

  it("covers every flag exactly once across all groups", () => {
    const grouped = CEREBRO_FLAG_GROUPS.flatMap((g) => flagsForGroup(g.key));
    expect(grouped).toHaveLength(CEREBRO_FLAGS.length);
  });

  it("keeps a default for every flag", () => {
    for (const flag of CEREBRO_FLAGS) {
      expect(flag.key in CEREBRO_FLAG_DEFAULTS, `missing default for ${flag.key}`).toBe(true);
    }
  });

  it("has unique group keys", () => {
    expect(groupKeys.size).toBe(CEREBRO_FLAG_GROUPS.length);
  });
});

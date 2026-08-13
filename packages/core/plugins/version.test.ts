import { describe, expect, it } from "vitest";
import { comparePluginVersions } from "./version";

describe("comparePluginVersions", () => {
  it.each([
    ["1.1.0", "1.0.0", 1],
    ["1.0.0", "1.0.0", 0],
    ["1.0.0", "1.0.0-rc.1", 1],
    ["1.0.0-rc.10", "1.0.0-rc.9", 1],
    ["1.0.0-beta.1", "1.0.0-1", 1],
    ["1.0.0-rc.1", "1.0.0-rc.1.1", -1],
    ["1.2.1+r1", "1.2.0", 1],
    ["1.2.1+r2", "1.2.1+r1", 0],
    ["100000000000000000000.0.0", "99999999999999999999.0.0", 1],
  ])("orders %s against %s", (left, right, expectedSign) => {
    expect(Math.sign(comparePluginVersions(left, right))).toBe(expectedSign);
  });
});

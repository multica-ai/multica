// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ROUTINE_PRESET_CRON, cadenceOfCron } from "./routine-schedule";

describe("cadenceOfCron", () => {
  it("names each preset by the expression it saves", () => {
    for (const [cadence, cron] of Object.entries(ROUTINE_PRESET_CRON)) {
      expect(cadenceOfCron(cron)).toBe(cadence);
    }
  });

  it("ignores surrounding whitespace", () => {
    expect(cadenceOfCron("  0 9 * * 1 ")).toBe("weekly");
  });

  it("returns null for a custom expression", () => {
    expect(cadenceOfCron("30 7 * * *")).toBeNull();
    expect(cadenceOfCron("")).toBeNull();
  });
});

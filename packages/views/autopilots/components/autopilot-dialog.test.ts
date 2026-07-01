import { describe, expect, it } from "vitest";
import { computeNextRun } from "./autopilot-dialog";
import type { TriggerConfig } from "./trigger-config";

describe("computeNextRun", () => {
  it("uses the trigger timezone when computing the next daily run", () => {
    const cfg: TriggerConfig = {
      frequency: "daily",
      time: "09:00",
      daysOfWeek: [1],
      cronExpression: "0 9 * * *",
      timezone: "Asia/Shanghai",
    };

    const next = computeNextRun(cfg, new Date("2026-01-01T00:30:00Z"));

    expect(next?.toISOString()).toBe("2026-01-01T01:00:00.000Z");
  });

  it("uses the trigger timezone when computing weekday eligibility", () => {
    const cfg: TriggerConfig = {
      frequency: "weekdays",
      time: "09:00",
      daysOfWeek: [1, 2, 3, 4, 5],
      cronExpression: "0 9 * * 1-5",
      timezone: "Asia/Shanghai",
    };

    const next = computeNextRun(cfg, new Date("2026-01-02T12:00:00Z"));

    expect(next?.toISOString()).toBe("2026-01-05T01:00:00.000Z");
  });
});

import { describe, it, expect } from "vitest";
import {
  computeNextRun,
  parseCronExpression,
  toCronExpression,
  getDefaultTriggerConfig,
  type TriggerConfig,
} from "./trigger-config";

describe("parseCronExpression", () => {
  it("round-trips hourly", () => {
    const cfg = { ...getDefaultTriggerConfig(), frequency: "hourly" as const, time: "00:15" };
    const cron = toCronExpression(cfg);
    const parsed = parseCronExpression(cron, "UTC");
    expect(parsed.frequency).toBe("hourly");
  });

  it("round-trips daily at 09:30", () => {
    const cfg = { ...getDefaultTriggerConfig(), frequency: "daily" as const, time: "09:30" };
    const cron = toCronExpression(cfg);
    const parsed = parseCronExpression(cron, "UTC");
    expect(parsed.frequency).toBe("daily");
    expect(parsed.time).toBe("09:30");
  });

  it("recognises weekdays pattern", () => {
    const parsed = parseCronExpression("0 9 * * 1-5", "UTC");
    expect(parsed.frequency).toBe("weekdays");
    expect(parsed.time).toBe("09:00");
  });

  it("recognises weekly with multiple days", () => {
    const parsed = parseCronExpression("0 9 * * 1,3,5", "UTC");
    expect(parsed.frequency).toBe("weekly");
    expect(parsed.daysOfWeek).toEqual([1, 3, 5]);
    expect(parsed.time).toBe("09:00");
  });

  it("falls back to custom for non-matching pattern", () => {
    const parsed = parseCronExpression("*/15 * * * *", "UTC");
    expect(parsed.frequency).toBe("custom");
    expect(parsed.cronExpression).toBe("*/15 * * * *");
  });

  it("falls back to custom for malformed input", () => {
    const parsed = parseCronExpression("not a cron", "UTC");
    expect(parsed.frequency).toBe("custom");
  });

  it("preserves provided timezone", () => {
    const parsed = parseCronExpression("0 9 * * *", "Asia/Shanghai");
    expect(parsed.timezone).toBe("Asia/Shanghai");
  });

  it("rejects out-of-range minute", () => {
    expect(parseCronExpression("60 * * * *", "UTC").frequency).toBe("custom");
  });

  it("rejects out-of-range hour", () => {
    expect(parseCronExpression("0 24 * * *", "UTC").frequency).toBe("custom");
  });

  it("round-trips weekly preserving daysOfWeek", () => {
    const cfg = { ...getDefaultTriggerConfig(), frequency: "weekly" as const, time: "14:45", daysOfWeek: [0, 2, 6] };
    const parsed = parseCronExpression(toCronExpression(cfg), "UTC");
    expect(parsed.frequency).toBe("weekly");
    expect(parsed.time).toBe("14:45");
    expect(parsed.daysOfWeek).toEqual([0, 2, 6]);
  });
});

// The configured time is wall-clock in cfg.timezone, never the host/browser
// zone. Every case asserts the resulting INSTANT (toISOString) so it is
// independent of where the test runs — exactly the property the old
// browser-local computeNextRun violated (it read 09:00 as 09:00 host-local).
describe("computeNextRun", () => {
  const base = (over: Partial<TriggerConfig>): TriggerConfig => ({
    ...getDefaultTriggerConfig(),
    ...over,
  });

  it("daily resolves the time in the schedule timezone (DST, GMT-4)", () => {
    // now = 2026-06-26 11:11 in Asia/Shanghai = 2026-06-25 23:11 EDT.
    const now = new Date("2026-06-26T03:11:00Z");
    const cfg = base({ frequency: "daily", time: "09:00", timezone: "America/New_York" });
    // Next 09:00 EDT (UTC-4) = 13:00Z, NOT 09:00 host-local.
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-06-26T13:00:00.000Z");
  });

  it("daily resolves the time in a positive-offset zone", () => {
    // 11:11 CST is past 09:00, so it rolls to the next day.
    const now = new Date("2026-06-26T03:11:00Z");
    const cfg = base({ frequency: "daily", time: "09:00", timezone: "Asia/Shanghai" });
    // 2026-06-27 09:00 CST (UTC+8) = 2026-06-27T01:00:00Z.
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-06-27T01:00:00.000Z");
  });

  it("daily uses standard offset in winter (GMT-5)", () => {
    const now = new Date("2026-01-15T20:00:00Z"); // 15:00 EST
    const cfg = base({ frequency: "daily", time: "09:00", timezone: "America/New_York" });
    // Next 09:00 EST (UTC-5) = 14:00Z — proves the offset tracks the season.
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-01-16T14:00:00.000Z");
  });

  it("daily rolls forward when today's time already passed in UTC", () => {
    const now = new Date("2026-06-26T10:00:00Z");
    const cfg = base({ frequency: "daily", time: "09:00", timezone: "UTC" });
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-06-27T09:00:00.000Z");
  });

  it("hourly fires at the configured minute in UTC", () => {
    const now = new Date("2026-06-26T10:20:00Z");
    const cfg = base({ frequency: "hourly", time: "00:15", timezone: "UTC" });
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-06-26T11:15:00.000Z");
  });

  it("weekdays skips the weekend in the schedule timezone", () => {
    const now = new Date("2026-06-27T12:00:00Z"); // Saturday
    const cfg = base({ frequency: "weekdays", time: "09:00", timezone: "UTC" });
    // Next weekday is Monday 2026-06-29.
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-06-29T09:00:00.000Z");
  });

  it("weekly lands on the configured day in the schedule timezone", () => {
    const now = new Date("2026-06-26T00:00:00Z"); // Friday
    const cfg = base({ frequency: "weekly", time: "14:00", daysOfWeek: [3], timezone: "UTC" });
    // Next Wednesday is 2026-07-01.
    expect(computeNextRun(cfg, now)?.toISOString()).toBe("2026-07-01T14:00:00.000Z");
  });

  it("returns null for custom cron (server computes it)", () => {
    const cfg = base({ frequency: "custom", timezone: "UTC" });
    expect(computeNextRun(cfg, new Date("2026-06-26T00:00:00Z"))).toBeNull();
  });

  it("returns null for weekly with no days selected", () => {
    const cfg = base({ frequency: "weekly", daysOfWeek: [], timezone: "UTC" });
    expect(computeNextRun(cfg, new Date("2026-06-26T00:00:00Z"))).toBeNull();
  });
});

import { describe, it, expect } from "vitest";
import {
  parseCronExpression,
  toCronExpression,
  getDefaultTriggerConfig,
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

import { describe, it, expect } from "vitest";
import {
  parseCronExpression,
  toCronExpression,
  getDefaultTriggerConfig,
  type TriggerConfig,
} from "./trigger-config";

// The backend accepts standard 5-field cron (robfig/cron v3: minute, hour,
// dom, month, dow — no seconds, no @descriptors). Each field may be `*`, a
// plain number, a range `a-b`, a step `*/n` | `a/n` | `a-b/n`, a list, or a
// name (JAN/MON). The form only models four exact shapes:
//
//   hourly    M * * * *
//   daily     M H * * *
//   weekdays  M H * * 1-5
//   weekly    M H * * d[,d...]   (plain days 0-6)
//
// where M/H/d are plain in-range integers. Every other expression must parse
// as `custom` with the original string preserved in cronExpression.

describe("parseCronExpression", () => {
  describe("preset recognition", () => {
    it("recognises hourly at minute 0", () => {
      const parsed = parseCronExpression("0 * * * *", "UTC");
      expect(parsed.frequency).toBe("hourly");
      expect(parsed.time).toBe("00:00");
    });

    it("recognises hourly at minute 30", () => {
      const parsed = parseCronExpression("30 * * * *", "UTC");
      expect(parsed.frequency).toBe("hourly");
      expect(parsed.time).toBe("00:30");
    });

    it("recognises hourly with a zero-padded minute", () => {
      const parsed = parseCronExpression("05 * * * *", "UTC");
      expect(parsed.frequency).toBe("hourly");
      expect(parsed.time).toBe("00:05");
    });

    it("recognises daily at midnight", () => {
      const parsed = parseCronExpression("0 0 * * *", "UTC");
      expect(parsed.frequency).toBe("daily");
      expect(parsed.time).toBe("00:00");
    });

    it("recognises daily at the last minute of the day", () => {
      const parsed = parseCronExpression("59 23 * * *", "UTC");
      expect(parsed.frequency).toBe("daily");
      expect(parsed.time).toBe("23:59");
    });

    it("recognises daily with zero-padded fields", () => {
      const parsed = parseCronExpression("05 09 * * *", "UTC");
      expect(parsed.frequency).toBe("daily");
      expect(parsed.time).toBe("09:05");
    });

    it("recognises the weekdays pattern", () => {
      const parsed = parseCronExpression("30 18 * * 1-5", "UTC");
      expect(parsed.frequency).toBe("weekdays");
      expect(parsed.time).toBe("18:30");
      expect(parsed.daysOfWeek).toEqual([1, 2, 3, 4, 5]);
    });

    it("recognises weekly on a single day", () => {
      const parsed = parseCronExpression("0 9 * * 0", "UTC");
      expect(parsed.frequency).toBe("weekly");
      expect(parsed.daysOfWeek).toEqual([0]);
    });

    it("recognises weekly on Saturday (upper bound)", () => {
      const parsed = parseCronExpression("0 9 * * 6", "UTC");
      expect(parsed.frequency).toBe("weekly");
      expect(parsed.daysOfWeek).toEqual([6]);
    });

    it("recognises weekly with multiple days", () => {
      const parsed = parseCronExpression("0 9 * * 1,3,5", "UTC");
      expect(parsed.frequency).toBe("weekly");
      expect(parsed.daysOfWeek).toEqual([1, 3, 5]);
      expect(parsed.time).toBe("09:00");
    });

    it("normalises unsorted, duplicated weekly days", () => {
      const parsed = parseCronExpression("0 9 * * 5,1,5", "UTC");
      expect(parsed.frequency).toBe("weekly");
      expect(parsed.daysOfWeek).toEqual([1, 5]);
    });

    it("tolerates surrounding and repeated whitespace", () => {
      const parsed = parseCronExpression("  0  9  *  *  *  ", "UTC");
      expect(parsed.frequency).toBe("daily");
      expect(parsed.time).toBe("09:00");
    });

    it("tolerates tab separators", () => {
      const parsed = parseCronExpression("0\t9\t*\t*\t*", "UTC");
      expect(parsed.frequency).toBe("daily");
    });
  });

  describe("custom fallback — minute field", () => {
    it.each([
      ["range", "0-30 9 * * *"],
      ["list", "0,30 9 * * *"],
      ["step over wildcard", "*/15 9 * * *"],
      ["step from anchor", "0/5 9 * * *"],
      ["step over range", "0-30/5 9 * * *"],
      ["wildcard (every minute of the hour)", "* 9 * * *"],
      ["wildcard everywhere (every minute)", "* * * * *"],
      ["out of range", "60 9 * * *"],
      ["out of range with wildcard hour", "60 * * * *"],
      ["negative", "-5 9 * * *"],
    ])("falls back to custom for minute %s", (_label, cron) => {
      const parsed = parseCronExpression(cron, "UTC");
      expect(parsed.frequency).toBe("custom");
      expect(parsed.cronExpression).toBe(cron);
    });
  });

  describe("custom fallback — hour field", () => {
    it.each([
      ["range", "0 9-21 * * *"],
      ["list", "0 9,12,15 * * *"],
      ["step over wildcard", "0 */2 * * *"],
      ["step from anchor", "0 9/2 * * *"],
      ["step over range", "0 9-17/2 * * *"],
      ["out of range", "0 24 * * *"],
      ["range with weekday dow", "0 9-21 * * 1-5"],
      ["range with listed dow", "0 9-21 * * 1,3"],
    ])("falls back to custom for hour %s", (_label, cron) => {
      const parsed = parseCronExpression(cron, "UTC");
      expect(parsed.frequency).toBe("custom");
      expect(parsed.cronExpression).toBe(cron);
    });
  });

  describe("custom fallback — day-of-month and month fields", () => {
    it.each([
      ["specific day of month", "0 9 1 * *"],
      ["day-of-month range", "0 9 1-15 * *"],
      ["day-of-month step", "0 9 */2 * *"],
      ["question mark day of month", "0 9 ? * *"],
      ["specific month", "0 9 * 6 *"],
      ["month range", "0 9 * 1-6 *"],
      ["month name", "0 9 * JAN *"],
      ["fully pinned date", "15 3 15 6 2"],
    ])("falls back to custom for %s", (_label, cron) => {
      const parsed = parseCronExpression(cron, "UTC");
      expect(parsed.frequency).toBe("custom");
      expect(parsed.cronExpression).toBe(cron);
    });
  });

  describe("custom fallback — day-of-week field", () => {
    it.each([
      ["range other than 1-5", "0 9 * * 2-4"],
      ["full-week range", "0 9 * * 0-6"],
      ["day name", "0 9 * * MON"],
      ["day name list", "0 9 * * MON,WED"],
      ["step", "0 9 * * */2"],
      ["step over range", "0 9 * * 1-5/2"],
      ["mixed list and range", "0 9 * * 1,3-5"],
      ["out-of-range day 7", "0 9 * * 7"],
      ["question mark", "0 9 * * ?"],
      ["hourly minute with weekday dow", "0 * * * 1-5"],
      ["hourly minute with single dow", "30 * * * 1"],
    ])("falls back to custom for dow %s", (_label, cron) => {
      const parsed = parseCronExpression(cron, "UTC");
      expect(parsed.frequency).toBe("custom");
      expect(parsed.cronExpression).toBe(cron);
    });
  });

  describe("custom fallback — malformed structure", () => {
    it.each([
      ["too few fields", "0 9 * *"],
      ["too many fields (seconds cron)", "0 0 9 * * *"],
      ["free text", "not a cron"],
      ["empty string", ""],
      ["whitespace only", "   "],
      ["@daily descriptor", "@daily"],
      ["@every descriptor", "@every 1h"],
    ])("falls back to custom for %s", (_label, cron) => {
      const parsed = parseCronExpression(cron, "UTC");
      expect(parsed.frequency).toBe("custom");
      expect(parsed.cronExpression).toBe(cron);
    });
  });

  describe("invariants", () => {
    it("preserves the raw expression on preset matches too", () => {
      expect(parseCronExpression("30 18 * * 1-5", "UTC").cronExpression).toBe(
        "30 18 * * 1-5",
      );
    });

    it("preserves provided timezone", () => {
      expect(parseCronExpression("0 9 * * *", "Asia/Shanghai").timezone).toBe(
        "Asia/Shanghai",
      );
      expect(parseCronExpression("0 9-21 * * *", "Asia/Shanghai").timezone).toBe(
        "Asia/Shanghai",
      );
    });
  });

  describe("round-trips through toCronExpression", () => {
    const presets: Array<[string, Partial<TriggerConfig>]> = [
      ["hourly", { frequency: "hourly", time: "00:15" }],
      ["daily", { frequency: "daily", time: "09:30" }],
      ["weekdays", { frequency: "weekdays", time: "08:00" }],
      ["weekly", { frequency: "weekly", time: "14:45", daysOfWeek: [0, 2, 6] }],
    ];

    it.each(presets)("round-trips %s", (_label, overrides) => {
      const cfg = { ...getDefaultTriggerConfig(), ...overrides };
      const parsed = parseCronExpression(toCronExpression(cfg), "UTC");
      expect(parsed.frequency).toBe(cfg.frequency);
      expect(parsed.time).toBe(cfg.time);
      if (cfg.frequency === "weekly") {
        expect(parsed.daysOfWeek).toEqual(cfg.daysOfWeek);
      }
    });

    it("serialises weekly with no days selected to Monday as a safety fallback", () => {
      const cfg = {
        ...getDefaultTriggerConfig(),
        frequency: "weekly" as const,
        time: "09:00",
        daysOfWeek: [],
      };
      expect(toCronExpression(cfg)).toBe("0 9 * * 1");
    });

    it("round-trips a custom expression verbatim", () => {
      const cfg = {
        ...getDefaultTriggerConfig(),
        frequency: "custom" as const,
        cronExpression: "0 9-21 * * *",
      };
      const parsed = parseCronExpression(toCronExpression(cfg), "UTC");
      expect(parsed.frequency).toBe("custom");
      expect(parsed.cronExpression).toBe("0 9-21 * * *");
      expect(toCronExpression(parsed)).toBe("0 9-21 * * *");
    });
  });
});

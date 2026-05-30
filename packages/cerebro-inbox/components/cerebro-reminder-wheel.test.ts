import { describe, expect, it } from "vitest";
import { buildDayOptions } from "./cerebro-reminder-wheel";

describe("buildDayOptions", () => {
  it("labels the first two rows Today / Tomorrow and the rest with a date", () => {
    const from = new Date(2026, 4, 29, 14, 30); // 29 May 2026, 14:30 local
    const days = buildDayOptions(from, 4, "Today", "Tomorrow", "en-US");

    expect(days).toHaveLength(4);
    expect(days[0]!.label).toBe("Today");
    expect(days[1]!.label).toBe("Tomorrow");
    // Third row is a formatted weekday/day/month, not the special labels.
    expect(days[2]!.label).not.toBe("Today");
    expect(days[2]!.label).toMatch(/May/);
  });

  it("normalizes each day to local midnight and advances by one day", () => {
    const from = new Date(2026, 4, 29, 14, 30);
    const days = buildDayOptions(from, 3, "Today", "Tomorrow", "en-US");

    for (const day of days) {
      expect(day.date.getHours()).toBe(0);
      expect(day.date.getMinutes()).toBe(0);
    }
    expect(days[0]!.date.getDate()).toBe(29);
    expect(days[1]!.date.getDate()).toBe(30);
    expect(days[2]!.date.getDate()).toBe(31);
  });
});

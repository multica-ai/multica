// @vitest-environment node
import { describe, expect, it } from "vitest";
import { formatSnoozeTime } from "./inbox-snooze-time";

const at = (y: number, m: number, d: number, h: number, min = 0) => new Date(y, m - 1, d, h, min);
// A Wednesday afternoon.
const now = at(2026, 6, 17, 14, 20);

describe("formatSnoozeTime", () => {
  it("shows the time alone for later today", () => {
    expect(formatSnoozeTime(at(2026, 6, 17, 18), now, "en-GB")).toBe("18:00");
  });

  it("names tomorrow", () => {
    expect(formatSnoozeTime(at(2026, 6, 18, 9), now, "en-GB")).toMatch(/^tomorrow 0?9:00$/);
    expect(formatSnoozeTime(at(2026, 6, 18, 9), now, "zh-Hans")).toMatch(/^明天 /);
  });

  it("names the weekday within the week", () => {
    expect(formatSnoozeTime(at(2026, 6, 22, 9), now, "en-GB")).toMatch(/^Mon 0?9:00$/);
  });

  it("shows the date further out, and the year once it differs", () => {
    expect(formatSnoozeTime(at(2026, 7, 3, 9), now, "en-GB")).toMatch(/^3 Jul,? 0?9:00$/);
    expect(formatSnoozeTime(at(2027, 1, 4, 9), now, "en-GB")).toMatch(/^4 Jan 2027,? 0?9:00$/);
  });
});

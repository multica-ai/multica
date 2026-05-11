import { describe, it, expect } from "vitest";
import { splitAtMidnight, displayEndForCalendar } from "./calendar-events-builder";

// ── splitAtMidnight ───────────────────────────────────────────────────────────

describe("splitAtMidnight", () => {
  it("returns a single segment when start and end are on the same day", () => {
    const start = new Date("2024-01-15T09:00:00");
    const end = new Date("2024-01-15T17:30:00");
    const segments = splitAtMidnight(start, end);
    expect(segments).toHaveLength(1);
    expect(segments[0]?.start).toEqual(start);
    expect(segments[0]?.end).toEqual(end);
  });

  it("splits a cross-midnight entry into two segments", () => {
    const start = new Date("2024-01-15T22:00:00");
    const end = new Date("2024-01-16T02:00:00");
    const segments = splitAtMidnight(start, end);
    expect(segments).toHaveLength(2);

    // First segment: start → 23:59:59.999
    const midnight = new Date("2024-01-16T00:00:00");
    expect(segments[0]?.start).toEqual(start);
    expect(segments[0]?.end.getTime()).toBe(midnight.getTime() - 1);

    // Second segment: midnight → end
    expect(segments[1]?.start).toEqual(midnight);
    expect(segments[1]?.end).toEqual(end);
  });

  it("splits across two midnights into three segments", () => {
    const start = new Date("2024-01-14T23:00:00");
    const end = new Date("2024-01-16T01:00:00");
    const segments = splitAtMidnight(start, end);
    expect(segments).toHaveLength(3);
  });

  it("handles an entry that ends exactly at midnight without creating a zero-duration segment", () => {
    const start = new Date("2024-01-15T22:00:00");
    const end = new Date("2024-01-16T00:00:00");
    const segments = splitAtMidnight(start, end);
    // The first segment ends 1ms before midnight, second would be zero-length — skip it.
    expect(segments).toHaveLength(1);
  });
});

// ── displayEndForCalendar ─────────────────────────────────────────────────────

describe("displayEndForCalendar", () => {
  it("returns the original end when start and end are in different minutes", () => {
    const start = new Date("2024-01-15T09:00:00");
    const end = new Date("2024-01-15T09:01:30");
    expect(displayEndForCalendar(start, end)).toEqual(end);
  });

  it("bumps end to next minute boundary when start and end share the same minute", () => {
    const start = new Date("2024-01-15T09:00:10");
    const end = new Date("2024-01-15T09:00:50");
    const adjusted = displayEndForCalendar(start, end);
    // Next minute = 09:01:00
    const expectedEnd = new Date("2024-01-15T09:01:00");
    expect(adjusted).toEqual(expectedEnd);
  });

  it("returns original end when end is exactly on the minute boundary of the next minute", () => {
    const start = new Date("2024-01-15T09:00:00");
    const end = new Date("2024-01-15T09:01:00");
    expect(displayEndForCalendar(start, end)).toEqual(end);
  });

  it("handles zero-duration entries (start === end)", () => {
    const start = new Date("2024-01-15T09:00:30");
    const end = new Date("2024-01-15T09:00:30");
    const adjusted = displayEndForCalendar(start, end);
    const expectedEnd = new Date("2024-01-15T09:01:00");
    expect(adjusted).toEqual(expectedEnd);
  });
});

import { describe, it, expect } from "vitest";
import {
  toDateOnly,
  dateOnlyToUTCDate,
  dateOnlyToLocalDate,
  formatDateOnly,
  isPastDateOnly,
  todayDateOnlyInTimeZone,
  addDaysDateOnlyInTimeZone,
} from "./date";

describe("issue date-only helpers", () => {
  it("serializes a picked local day to YYYY-MM-DD with the local calendar", () => {
    // A calendar picker hands back local midnight of the clicked day.
    expect(toDateOnly(new Date(2026, 2, 1))).toBe("2026-03-01");
    expect(toDateOnly(new Date(2026, 0, 5))).toBe("2026-01-05"); // zero-padded
  });

  it("formats a date-only string timezone-safely (no day shift)", () => {
    // The bug: a calendar day must render as the same day in every timezone.
    expect(
      formatDateOnly("2026-03-01", { month: "short", day: "numeric" }, "en-US"),
    ).toBe("Mar 1");
    expect(formatDateOnly("2026-03-01", undefined, "en-US")).toBe("Mar 1");
    expect(formatDateOnly(null)).toBe("");
    expect(formatDateOnly("")).toBe("");
  });

  it("round-trips a picked day back to the same displayed day", () => {
    const picked = new Date(2026, 2, 1); // user clicks March 1 locally
    const stored = toDateOnly(picked);
    expect(stored).toBe("2026-03-01");
    expect(formatDateOnly(stored, { month: "short", day: "numeric" }, "en-US")).toBe(
      "Mar 1",
    );
  });

  it("anchors a date-only value at UTC midnight", () => {
    expect(dateOnlyToUTCDate("2026-03-01")?.toISOString()).toBe(
      "2026-03-01T00:00:00.000Z",
    );
    expect(dateOnlyToUTCDate(null)).toBeNull();
  });

  it("tolerates a legacy RFC3339 instant by reading its UTC day", () => {
    // Old clients stored local-midnight-as-UTC; read the stored UTC calendar day.
    expect(dateOnlyToUTCDate("2026-02-28T16:00:00Z")?.toISOString()).toBe(
      "2026-02-28T00:00:00.000Z",
    );
  });

  it("builds a local-midnight Date for the picker's selected day", () => {
    const d = dateOnlyToLocalDate("2026-03-01");
    expect(d?.getFullYear()).toBe(2026);
    expect(d?.getMonth()).toBe(2);
    expect(d?.getDate()).toBe(1);
    expect(dateOnlyToLocalDate(null)).toBeUndefined();
  });

  it("offsets days from today in the viewer's timezone (calendar-day math)", () => {
    const tz = "Asia/Shanghai";
    const pad = (n: number) => String(n).padStart(2, "0");
    const [ty, tm, td] = todayDateOnlyInTimeZone(tz).split("-").map(Number);
    const day = (offset: number): string => {
      const d = new Date(Date.UTC(ty!, tm! - 1, td! + offset));
      return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
    };
    expect(addDaysDateOnlyInTimeZone(0, tz)).toBe(day(0));
    expect(addDaysDateOnlyInTimeZone(1, tz)).toBe(day(1));
    expect(addDaysDateOnlyInTimeZone(7, tz)).toBe(day(7));
    expect(addDaysDateOnlyInTimeZone(-6, tz)).toBe(day(-6));
    // An unknown timezone falls back to UTC instead of throwing.
    expect(() => addDaysDateOnlyInTimeZone(1, "Not/AZone")).not.toThrow();
  });

  it("detects past calendar days relative to today in the viewer's timezone", () => {
    const tz = "Asia/Shanghai";
    const pad = (n: number) => String(n).padStart(2, "0");
    // Anchor on "today" in the viewer's tz, then walk the calendar.
    const [ty, tm, td] = todayDateOnlyInTimeZone(tz).split("-").map(Number);
    const day = (offset: number): string => {
      const d = new Date(Date.UTC(ty!, tm! - 1, td! + offset));
      return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
    };
    expect(isPastDateOnly(day(-1), tz)).toBe(true);
    expect(isPastDateOnly(day(0), tz)).toBe(false);
    expect(isPastDateOnly(day(1), tz)).toBe(false);
    expect(isPastDateOnly(null, tz)).toBe(false);
  });

  it("anchors today in the requested timezone", () => {
    expect(todayDateOnlyInTimeZone("UTC")).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    // An unknown timezone falls back to UTC instead of throwing.
    expect(() => todayDateOnlyInTimeZone("Not/AZone")).not.toThrow();
    expect(todayDateOnlyInTimeZone("Not/AZone")).toBe(
      todayDateOnlyInTimeZone("UTC"),
    );
  });
});

import { describe, it, expect } from "vitest";
import {
  toDateOnly,
  todayDateOnly,
  addDaysDateOnly,
  dateOnlyToUTCDate,
  dateOnlyToLocalDate,
  formatDateOnly,
  isPastDateOnly,
  hasTime,
  formatDateTime,
  formatScheduleDate,
  isoToLocalDate,
  localDateTimeToIso,
  DEFAULT_DUE_TIME,
  DEFAULT_START_TIME,
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

  it("detects past calendar days relative to today", () => {
    expect(isPastDateOnly(addDaysDateOnly(-1))).toBe(true);
    expect(isPastDateOnly(todayDateOnly())).toBe(false);
    expect(isPastDateOnly(addDaysDateOnly(1))).toBe(false);
    expect(isPastDateOnly(null)).toBe(false);
  });
});

describe("issue date-time helpers", () => {
  it("detects whether a value carries a time-of-day", () => {
    expect(hasTime("2026-02-01T14:30:00Z")).toBe(true);
    expect(hasTime("2026-02-01")).toBe(false);
    expect(hasTime(null)).toBe(false);
    expect(hasTime("")).toBe(false);
  });

  it("formats an instant in the given timezone", () => {
    // `timeZone` is a test-only pin; production omits it and uses the viewer's TZ.
    expect(formatDateTime("2026-02-01T14:30:00Z", undefined, "en-US", "UTC")).toBe("Feb 1, 14:30");
    // Same instant, viewer in New York (UTC-5) → localized.
    expect(formatDateTime("2026-02-01T14:30:00Z", undefined, "en-US", "America/New_York")).toBe("Feb 1, 09:30");
    expect(formatDateTime(null)).toBe("");
    expect(formatDateTime("nonsense")).toBe("");
  });

  it("renders a timed value with time and a date-only value without", () => {
    expect(formatScheduleDate("2026-02-01T14:30:00Z", "en-US", "UTC")).toBe("Feb 1, 14:30");
    expect(formatScheduleDate("2026-03-01", "en-US")).toBe("Mar 1"); // date-only stays day-only (UTC-safe)
    expect(formatScheduleDate(null)).toBe("");
  });
});

describe("issue picker date-time helpers", () => {
  it("round-trips an instant through isoToLocalDate -> localDateTimeToIso", () => {
    const d = isoToLocalDate("2026-02-01T14:30:00Z");
    expect(d).toBeInstanceOf(Date);
    expect(d && localDateTimeToIso(d)).toBe("2026-02-01T14:30:00.000Z");
    expect(isoToLocalDate(null)).toBeUndefined();
    expect(isoToLocalDate("nonsense")).toBeUndefined();
  });

  it("seeds from a legacy date-only value without shifting the day", () => {
    const d = isoToLocalDate("2026-03-01");
    expect(d?.getFullYear()).toBe(2026);
    expect(d?.getMonth()).toBe(2);
    expect(d?.getDate()).toBe(1); // local March 1 — no UTC-midnight day shift
  });

  it("combines a local wall-clock Date into a UTC instant (preserving the instant)", () => {
    const local = new Date(2026, 1, 1, 14, 30); // local Feb 1, 14:30
    const back = new Date(localDateTimeToIso(local));
    expect(back.getFullYear()).toBe(2026);
    expect(back.getMonth()).toBe(1);
    expect(back.getDate()).toBe(1);
    expect(back.getHours()).toBe(14);
    expect(back.getMinutes()).toBe(30);
  });

  it("exposes sensible default times", () => {
    expect(DEFAULT_DUE_TIME).toEqual({ h: 23, m: 59 });
    expect(DEFAULT_START_TIME).toEqual({ h: 9, m: 0 });
  });
});

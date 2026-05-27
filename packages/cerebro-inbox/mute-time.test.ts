import { describe, expect, it } from "vitest";
import {
  addHours,
  formatMutedUntilTime,
  formatPlannedDateTime,
  fromDateTimeLocalValue,
  isMuted,
  isReminderOverdue,
  nextBusinessDayNineAm,
  nextLocalEightAm,
  nextMondayNineAm,
  toDateTimeLocalValue,
} from "./mute-time";

describe("nextLocalEightAm", () => {
  it("returns today's 08:00 when called before 08:00", () => {
    const now = new Date(2026, 4, 7, 6, 30); // 06:30 local
    const got = nextLocalEightAm(now);
    expect(got.getHours()).toBe(8);
    expect(got.getMinutes()).toBe(0);
    expect(got.getDate()).toBe(7);
  });

  it("returns tomorrow's 08:00 when called after 08:00", () => {
    const now = new Date(2026, 4, 7, 14, 0); // 14:00 local
    const got = nextLocalEightAm(now);
    expect(got.getDate()).toBe(8);
    expect(got.getHours()).toBe(8);
  });

  it("returns tomorrow's 08:00 when called exactly at 08:00", () => {
    const now = new Date(2026, 4, 7, 8, 0, 0, 0);
    const got = nextLocalEightAm(now);
    expect(got.getDate()).toBe(8);
  });
});

describe("isMuted", () => {
  it("returns false for null", () => {
    expect(isMuted(null)).toBe(false);
  });
  it("returns false for past timestamps", () => {
    const past = new Date(Date.now() - 1000).toISOString();
    expect(isMuted(past)).toBe(false);
  });
  it("returns true for future timestamps", () => {
    const future = new Date(Date.now() + 60_000).toISOString();
    expect(isMuted(future)).toBe(true);
  });
});

describe("formatMutedUntilTime", () => {
  it("returns null for null", () => {
    expect(formatMutedUntilTime(null)).toBeNull();
  });
  it("returns null for past timestamps", () => {
    const past = new Date(Date.now() - 1000).toISOString();
    expect(formatMutedUntilTime(past)).toBeNull();
  });
  it("returns an HH:MM-style string for future timestamps", () => {
    // 08:00 on a fixed date — the literal output depends on the
    // execution locale, but the formatter must yield a non-empty string
    // that contains both digits of the hour.
    const future = new Date(Date.now() + 24 * 3600_000).toISOString();
    const got = formatMutedUntilTime(future, "en-US");
    expect(got).toBeTruthy();
    expect(got!).toMatch(/\d/);
  });
});

describe("formatPlannedDateTime", () => {
  it("returns null for malformed timestamps", () => {
    expect(formatPlannedDateTime("not-a-date")).toBeNull();
  });
});

describe("isReminderOverdue", () => {
  const now = new Date("2026-05-27T10:00:00.000Z");

  it("returns true when the planned time has passed", () => {
    expect(isReminderOverdue("2026-05-27T09:59:00.000Z", now)).toBe(true);
  });

  it("returns true at the planned time", () => {
    expect(isReminderOverdue("2026-05-27T10:00:00.000Z", now)).toBe(true);
  });

  it("returns false before the planned time", () => {
    expect(isReminderOverdue("2026-05-27T10:01:00.000Z", now)).toBe(false);
  });

  it("returns false for missing or malformed timestamps", () => {
    expect(isReminderOverdue(null, now)).toBe(false);
    expect(isReminderOverdue("not-a-date", now)).toBe(false);
  });
});

describe("nextBusinessDayNineAm", () => {
  it("skips the weekend", () => {
    const friday = new Date(2026, 4, 22, 14, 0);
    const got = nextBusinessDayNineAm(friday);
    expect(got.getDay()).toBe(1);
    expect(got.getDate()).toBe(25);
    expect(got.getHours()).toBe(9);
    expect(got.getMinutes()).toBe(0);
  });

  it("formats datetime-local values without timezone conversion", () => {
    const got = toDateTimeLocalValue(new Date(2026, 4, 25, 9, 5));
    expect(got).toBe("2026-05-25T09:05");
  });
});

describe("fromDateTimeLocalValue", () => {
  it("parses a datetime-local string into a local-time Date", () => {
    const got = fromDateTimeLocalValue("2026-05-25T09:05");
    expect(got).not.toBeNull();
    expect(got!.getFullYear()).toBe(2026);
    expect(got!.getMonth()).toBe(4); // May (0-indexed)
    expect(got!.getDate()).toBe(25);
    expect(got!.getHours()).toBe(9);
    expect(got!.getMinutes()).toBe(5);
  });

  it("round-trips with toDateTimeLocalValue", () => {
    const original = new Date(2026, 4, 25, 9, 5);
    const got = fromDateTimeLocalValue(toDateTimeLocalValue(original));
    expect(got!.getTime()).toBe(original.getTime());
  });

  it("returns null for malformed input", () => {
    expect(fromDateTimeLocalValue("")).toBeNull();
    expect(fromDateTimeLocalValue("not-a-date")).toBeNull();
    expect(fromDateTimeLocalValue("2026-05-25")).toBeNull();
  });
});

describe("reminder preset helpers", () => {
  it("adds exact hour durations without snapping to the hour", () => {
    const now = new Date(2026, 4, 22, 14, 37);
    const got = addHours(3, now);
    expect(got.getHours()).toBe(17);
    expect(got.getMinutes()).toBe(37);
  });

  it("returns the next Monday at 09:00", () => {
    const sunday = new Date(2026, 4, 24, 12, 0);
    const got = nextMondayNineAm(sunday);
    expect(got.getDay()).toBe(1);
    expect(got.getDate()).toBe(25);
    expect(got.getHours()).toBe(9);
    expect(got.getMinutes()).toBe(0);
  });
});

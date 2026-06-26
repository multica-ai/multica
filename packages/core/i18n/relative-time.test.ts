import { describe, expect, it } from "vitest";
import { RELATIVE_DAYS_TO_MONTHS, relativeTimeBucket } from "./relative-time";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const ms = (iso: string) => new Date(iso).getTime();

describe("relativeTimeBucket", () => {
  it("collapses sub-minute and small-skew diffs to just_now (both directions)", () => {
    const now = ms("2026-03-01T12:00:00Z");
    expect(relativeTimeBucket(now, now, "UTC")).toEqual({ kind: "just_now" });
    expect(relativeTimeBucket(now - (MINUTE - 1), now, "UTC")).toEqual({
      kind: "just_now",
    });
    // A sub-minute future timestamp (clock skew) also collapses to just_now.
    expect(relativeTimeBucket(now + (MINUTE - 1), now, "UTC")).toEqual({
      kind: "just_now",
    });
  });

  it("buckets minutes and hours by elapsed time, timezone-independent", () => {
    const now = ms("2026-03-01T12:00:00Z");
    expect(relativeTimeBucket(now - MINUTE, now, "UTC")).toEqual({
      kind: "minutes",
      count: 1,
      future: false,
    });
    expect(relativeTimeBucket(now - 59 * MINUTE, now, "UTC")).toEqual({
      kind: "minutes",
      count: 59,
      future: false,
    });
    expect(relativeTimeBucket(now - HOUR, now, "UTC")).toEqual({
      kind: "hours",
      count: 1,
      future: false,
    });
    expect(relativeTimeBucket(now - 23 * HOUR, now, "UTC")).toEqual({
      kind: "hours",
      count: 23,
      future: false,
    });
  });

  it("mirrors the sub-day buckets into the future direction", () => {
    const now = ms("2026-03-01T12:00:00Z");
    expect(relativeTimeBucket(now + 5 * MINUTE, now, "UTC")).toEqual({
      kind: "minutes",
      count: 5,
      future: true,
    });
    expect(relativeTimeBucket(now + 3 * HOUR, now, "UTC")).toEqual({
      kind: "hours",
      count: 3,
      future: true,
    });
  });

  it("counts day granularity by calendar days, not elapsed 24h windows", () => {
    // The motivating bug: ~1d 23h59m apart but two calendar days → "2d ago",
    // not "1d ago".
    expect(
      relativeTimeBucket(
        ms("2026-06-23T15:05:52Z"),
        ms("2026-06-25T15:05:50Z"),
        "UTC",
      ),
    ).toEqual({ kind: "days", count: 2, future: false });

    // 25h apart but one calendar day → "1d ago" (yesterday).
    expect(
      relativeTimeBucket(
        ms("2026-03-01T10:00:00Z"),
        ms("2026-03-02T11:00:00Z"),
        "UTC",
      ),
    ).toEqual({ kind: "days", count: 1, future: false });
  });

  it("mirrors the day bucket into the future direction", () => {
    expect(
      relativeTimeBucket(
        ms("2026-03-04T12:00:00Z"),
        ms("2026-03-01T12:00:00Z"),
        "UTC",
      ),
    ).toEqual({ kind: "days", count: 3, future: true });
  });

  it("computes calendar days in the supplied timezone", () => {
    const then = ms("2026-03-01T20:00:00Z");
    const now = ms("2026-03-03T02:00:00Z");
    // UTC: Mar 1 → Mar 3 = 2 calendar days.
    expect(relativeTimeBucket(then, now, "UTC")).toEqual({
      kind: "days",
      count: 2,
      future: false,
    });
    // Asia/Shanghai (+8): Mar 2 04:00 → Mar 3 10:00 = 1 calendar day.
    expect(relativeTimeBucket(then, now, "Asia/Shanghai")).toEqual({
      kind: "days",
      count: 1,
      future: false,
    });
  });

  it("keeps an hours label when a ≥24h gap stays on one DST fall-back day", () => {
    // 2025-11-02 is a 25h day in America/New_York (clocks fall back 02:00→01:00).
    // 00:30 EDT and 23:30 EST are both Nov 2 there, yet 24h apart in real ms.
    // Must read "24h", not a contradictory "1d ago" beside a "today" date.
    const then = ms("2025-11-02T04:30:00Z"); // 00:30 EDT, Nov 2
    const now = ms("2025-11-03T04:30:00Z"); // 23:30 EST, still Nov 2
    expect(relativeTimeBucket(then, now, "America/New_York")).toEqual({
      kind: "hours",
      count: 24,
      future: false,
    });
  });

  it("returns invalid for non-finite input instead of throwing", () => {
    const now = ms("2026-03-01T12:00:00Z");
    expect(relativeTimeBucket(NaN, now, "UTC")).toEqual({ kind: "invalid" });
    expect(relativeTimeBucket(now, NaN, "UTC")).toEqual({ kind: "invalid" });
  });

  it("switches from days to calendar months past the day cap", () => {
    const now = ms("2026-03-31T00:00:00Z");
    // Exactly 30 calendar days → still days.
    expect(
      relativeTimeBucket(ms("2026-03-01T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "days", count: RELATIVE_DAYS_TO_MONTHS, future: false });
    // 31 calendar days, spanning a month boundary → "1mo".
    expect(
      relativeTimeBucket(ms("2026-02-28T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "months", count: 1, future: false });
  });

  it("counts whole calendar months and rolls into years at 12", () => {
    const now = ms("2026-06-15T00:00:00Z");
    // Just under a year stays at 11 months.
    expect(
      relativeTimeBucket(ms("2025-06-16T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "months", count: 11, future: false });
    // Exactly a year → "1y".
    expect(
      relativeTimeBucket(ms("2025-06-15T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "years", count: 1, future: false });
    // Multiple years.
    expect(
      relativeTimeBucket(ms("2023-06-15T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "years", count: 3, future: false });
  });

  it("mirrors months and years into the future direction", () => {
    const now = ms("2026-06-15T00:00:00Z");
    expect(
      relativeTimeBucket(ms("2026-09-15T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "months", count: 3, future: true });
    expect(
      relativeTimeBucket(ms("2028-06-15T00:00:00Z"), now, "UTC"),
    ).toEqual({ kind: "years", count: 2, future: true });
  });
});

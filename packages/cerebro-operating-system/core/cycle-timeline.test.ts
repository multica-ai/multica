import { describe, expect, it } from "vitest";
import { buildCycleTimeline, formatCycleDate, shiftByCadence } from "./cycle-timeline";

describe("shiftByCadence", () => {
  it("steps by days and weeks honoring the count", () => {
    expect(shiftByCadence("2026-01-15", "day", 3, 2)).toBe("2026-01-21");
    expect(shiftByCadence("2026-01-15", "week", 1, 1)).toBe("2026-01-22");
    expect(shiftByCadence("2026-01-15", "week", 2, 1)).toBe("2026-01-29");
  });

  it("steps by months and quarters, forwards and backwards", () => {
    expect(shiftByCadence("2026-01-15", "month", 1, 1)).toBe("2026-02-15");
    expect(shiftByCadence("2026-01-15", "quarter", 1, 1)).toBe("2026-04-15");
    expect(shiftByCadence("2026-01-15", "quarter", 2, 1)).toBe("2026-07-15");
    expect(shiftByCadence("2026-01-15", "month", 1, -2)).toBe("2025-11-15");
  });

  // A month step from a day the target month does not have must land on that
  // month's last day, the way a calendar reads it. Letting Date normalise
  // "Feb 31" into "Mar 3" silently skips a whole month.
  it("clamps a month step to the last day of a shorter target month", () => {
    expect(shiftByCadence("2026-01-31", "month", 1, 1)).toBe("2026-02-28");
    expect(shiftByCadence("2028-01-31", "month", 1, 1)).toBe("2028-02-29");
    expect(shiftByCadence("2026-03-31", "month", 1, 1)).toBe("2026-04-30");
    expect(shiftByCadence("2026-03-31", "month", 1, -1)).toBe("2026-02-28");
    expect(shiftByCadence("2026-11-30", "quarter", 1, 1)).toBe("2027-02-28");
  });

  it("keeps a clamped month step on the same day when the month is long enough", () => {
    // Clamping must not drag later steps down with it: Jan 31 -> Feb 28 -> Mar 31,
    // not Feb 28 -> Mar 28. Every step is measured from the original anchor.
    expect([1, 2, 3, 4].map((step) => shiftByCadence("2026-01-31", "month", 1, step)))
      .toEqual(["2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31"]);
  });
});

describe("buildCycleTimeline", () => {
  it("returns nothing for a manual or invalid cadence", () => {
    expect(buildCycleTimeline("manual", 1, "2026-01-01")).toEqual([]);
    expect(buildCycleTimeline("week", 0, "2026-01-01")).toEqual([]);
  });

  it("projects weekly cycles with one past and highlights the current cycle", () => {
    const timeline = buildCycleTimeline("week", 1, "2026-01-15", { past: 1, upcoming: 2 });
    expect(timeline.map((occ) => occ.date)).toEqual(["2026-01-08", "2026-01-15", "2026-01-22", "2026-01-29"]);
    expect(timeline.map((occ) => occ.relative)).toEqual(["past", "current", "upcoming", "upcoming"]);
    expect(timeline.map((occ) => occ.offset)).toEqual([-1, 0, 1, 2]);
  });

  it("defaults to one past and five upcoming cycles", () => {
    expect(buildCycleTimeline("month", 1, "2026-01-15")).toHaveLength(7);
  });

  // Anchoring on a month end used to render the past cycle AFTER the current
  // one (Mar 31 - 1 month became Mar 3), so the timeline read out of order.
  it("stays in chronological order when the cycle is anchored on a month end", () => {
    const dates = buildCycleTimeline("month", 1, "2026-03-31", { past: 1, upcoming: 3 }).map((occ) => occ.date);
    expect(dates).toEqual(["2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31", "2026-06-30"]);
    expect([...dates].sort()).toEqual(dates);
  });
});

describe("formatCycleDate", () => {
  it("formats an ISO date as an abbreviated month, day and year", () => {
    expect(formatCycleDate("2026-01-15")).toBe("Jan 15, 2026");
    expect(formatCycleDate("2026-12-01")).toBe("Dec 1, 2026");
  });
});

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
});

describe("formatCycleDate", () => {
  it("formats an ISO date as an abbreviated month, day and year", () => {
    expect(formatCycleDate("2026-01-15")).toBe("Jan 15, 2026");
    expect(formatCycleDate("2026-12-01")).toBe("Dec 1, 2026");
  });
});

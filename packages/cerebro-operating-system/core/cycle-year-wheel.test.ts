import { describe, expect, it } from "vitest";

import { buildYearWheel } from "./cycle-year-wheel";
import type { MeetingNoteType } from "./types";

function noteType(overrides: Partial<MeetingNoteType>): MeetingNoteType {
  return { id: "id", name: "Note", cadence_unit: "month", cadence_count: 1, enabled: true, ...overrides };
}

describe("buildYearWheel", () => {
  it("buckets each Note's occurrences into the rolling 12 months", () => {
    const wheel = buildYearWheel(
      [noteType({ id: "fin", name: "Finance Review", year_dates: ["2026-07-20", "2026-08-17", "2027-01-18"] })],
      "2026-07-01",
    );
    expect(wheel).toHaveLength(12);
    expect(wheel[0]?.label).toBe("Jul 2026");
    expect(wheel[0]?.entries[0]?.days).toEqual([20]);
    expect(wheel[1]?.label).toBe("Aug 2026");
    expect(wheel[1]?.entries[0]?.days).toEqual([17]);
    // Jan 2027 is the 7th cell (index 6) in a rolling year from Jul 2026.
    expect(wheel[6]?.label).toBe("Jan 2027");
    expect(wheel[6]?.entries[0]?.days).toEqual([18]);
  });

  it("groups multiple occurrences in the same month and sorts the days", () => {
    const wheel = buildYearWheel(
      [noteType({ cadence_unit: "week", year_dates: ["2026-07-27", "2026-07-06", "2026-07-13", "2026-07-20"] })],
      "2026-07-01",
    );
    expect(wheel[0]?.entries[0]?.days).toEqual([6, 13, 20, 27]);
  });

  it("excludes manual and disabled Notes", () => {
    const wheel = buildYearWheel(
      [
        noteType({ id: "m", cadence_unit: "manual", year_dates: ["2026-07-20"] }),
        noteType({ id: "off", enabled: false, year_dates: ["2026-07-20"] }),
      ],
      "2026-07-01",
    );
    expect(wheel.every((month) => month.entries.length === 0)).toBe(true);
  });
});

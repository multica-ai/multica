import { describe, expect, it } from "vitest";

import { formatMeetingDate, planRecurringNotes, recurrenceSummary, relativeDayLabel } from "./cycle-planner";
import type { MeetingNoteType } from "./types";

function noteType(overrides: Partial<MeetingNoteType>): MeetingNoteType {
  return { id: "id", name: "Note", cadence_unit: "month", cadence_count: 1, enabled: true, ...overrides };
}

describe("recurrenceSummary", () => {
  it("describes a monthly Nth-weekday anchor", () => {
    expect(recurrenceSummary(noteType({ cadence_unit: "month", anchor_weekday: 1, anchor_week_of_month: 3 }))).toBe("Every month on the third Monday");
  });
  it("describes the last weekday of the month", () => {
    expect(recurrenceSummary(noteType({ cadence_unit: "month", anchor_weekday: 5, anchor_week_of_month: -1 }))).toBe("Every month on the last Friday");
  });
  it("describes a weekly anchor", () => {
    expect(recurrenceSummary(noteType({ cadence_unit: "week", cadence_count: 2, anchor_weekday: 1 }))).toBe("Every 2 weeks on Monday");
  });
  it("falls back to a plain interval without an anchor", () => {
    expect(recurrenceSummary(noteType({ cadence_unit: "month", cadence_count: 1 }))).toBe("Every month");
  });
});

describe("formatMeetingDate", () => {
  it("prefixes the weekday", () => {
    expect(formatMeetingDate("2026-07-20")).toBe("Mon, Jul 20, 2026");
  });
});

describe("relativeDayLabel", () => {
  it("labels near-term distances", () => {
    expect(relativeDayLabel("2026-07-20", "2026-07-20")).toBe("Today");
    expect(relativeDayLabel("2026-07-21", "2026-07-20")).toBe("Tomorrow");
    expect(relativeDayLabel("2026-07-25", "2026-07-20")).toBe("In 5 days");
    expect(relativeDayLabel("2026-08-03", "2026-07-20")).toBe("In 2 weeks");
  });
});

describe("planRecurringNotes", () => {
  it("drops manual + disabled types and sorts by soonest next meeting", () => {
    const plan = planRecurringNotes([
      noteType({ id: "later", cadence_unit: "month", next_meeting_date: "2026-08-17", upcoming_dates: ["2026-08-17"] }),
      noteType({ id: "manual", cadence_unit: "manual" }),
      noteType({ id: "off", enabled: false, next_meeting_date: "2026-07-01" }),
      noteType({ id: "soon", cadence_unit: "week", next_meeting_date: "2026-07-27", upcoming_dates: ["2026-07-27", "2026-08-03"] }),
    ], "2026-07-20");
    expect(plan.map((p) => p.noteType.id)).toEqual(["soon", "later"]);
    expect(plan[0]?.nextLabel).toBe("Mon, Jul 27, 2026");
    expect(plan[0]?.upcoming).toEqual(["Mon, Aug 3, 2026"]);
  });
});

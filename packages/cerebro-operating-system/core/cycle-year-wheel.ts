import type { MeetingNoteType } from "./types";
import { recurrenceSummary } from "./cycle-planner";

// The year-wheel is a rolling 12-month overview of the operating cadence: each
// month shows which recurring Notes meet that month and on which day(s). All
// dates come from the backend (year_dates); this module only buckets them.

const MONTH_ABBR = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export interface YearWheelEntry {
  noteType: MeetingNoteType;
  /** Day-of-month numbers this Note meets in the cell's month, ascending. */
  days: number[];
  /** Recurrence summary, e.g. "Every month on the third Monday". */
  summary: string;
}

export interface YearWheelMonth {
  /** yyyy-mm key of the month. */
  key: string;
  /** e.g. "Jul 2026". */
  label: string;
  entries: YearWheelEntry[];
}

function parseIso(value: string) {
  const [y, m, d] = value.slice(0, 10).split("-");
  return { year: Number(y ?? 0), month: Number(m ?? 1), day: Number(d ?? 1) };
}

const pad2 = (value: number) => String(value).padStart(2, "0");

/**
 * Build a rolling `months`-long overview starting at `today`'s month. Each
 * month lists the recurring Notes that meet in it, with their day numbers.
 * Manual and disabled Notes are excluded.
 */
export function buildYearWheel(
  noteTypes: MeetingNoteType[],
  today = new Date().toISOString().slice(0, 10),
  months = 12,
): YearWheelMonth[] {
  const start = parseIso(today);
  const cells: YearWheelMonth[] = [];
  const index = new Map<string, YearWheelMonth>();
  for (let offset = 0; offset < months; offset++) {
    const date = new Date(Date.UTC(start.year, start.month - 1 + offset, 1));
    const year = date.getUTCFullYear();
    const month = date.getUTCMonth() + 1;
    const key = `${year}-${pad2(month)}`;
    const cell: YearWheelMonth = { key, label: `${MONTH_ABBR[month - 1]} ${year}`, entries: [] };
    cells.push(cell);
    index.set(key, cell);
  }

  for (const noteType of noteTypes) {
    if (!noteType.enabled || noteType.cadence_unit === "manual") continue;
    const daysByMonth = new Map<string, number[]>();
    for (const iso of noteType.year_dates ?? []) {
      const { year, month, day } = parseIso(iso);
      const key = `${year}-${pad2(month)}`;
      if (!index.has(key)) continue;
      const list = daysByMonth.get(key) ?? [];
      list.push(day);
      daysByMonth.set(key, list);
    }
    const summary = recurrenceSummary(noteType);
    for (const [key, days] of daysByMonth) {
      index.get(key)?.entries.push({ noteType, days: days.sort((a, b) => a - b), summary });
    }
  }

  return cells;
}

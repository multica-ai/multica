import type { MeetingCadenceUnit, MeetingNoteType } from "./types";
import { formatCycleDate } from "./cycle-timeline";

// The recurring planner projects every recurring Note onto a single agenda:
// what runs next, and when. All date maths comes from the backend
// (next_meeting_date / upcoming_dates) — this module only shapes it for display.

const WEEKDAY_ABBR = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const WEEKDAY_FULL = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const CADENCE_NOUN: Record<Exclude<MeetingCadenceUnit, "manual">, [string, string]> = {
  day: ["day", "days"],
  week: ["week", "weeks"],
  month: ["month", "months"],
  quarter: ["quarter", "quarters"],
};
const ORDINAL: Record<number, string> = { 1: "first", 2: "second", 3: "third", 4: "fourth", 5: "fifth", [-1]: "last" };

export interface PlannedNote {
  noteType: MeetingNoteType;
  /** ISO yyyy-mm-dd of the next meeting, or undefined when unknown. */
  nextDate?: string;
  /** e.g. "Mon, Jul 20, 2026". */
  nextLabel?: string;
  /** e.g. "Today" / "In 5 days" / "In 2 weeks". */
  relative?: string;
  /** Human recurrence summary, e.g. "Every month on the 3rd Monday". */
  summary: string;
  /** Formatted upcoming dates after the next one. */
  upcoming: string[];
}

function parseIso(value: string) {
  const [y, m, d] = value.slice(0, 10).split("-");
  return { year: Number(y ?? 0), month: Number(m ?? 1), day: Number(d ?? 1) };
}

function isoWeekdayName(value: string, table: string[]): string {
  const { year, month, day } = parseIso(value);
  return table[new Date(Date.UTC(year, month - 1, day)).getUTCDay()] ?? "";
}

/** "Mon, Jul 20, 2026" — weekday-prefixed calendar date. */
export function formatMeetingDate(iso: string): string {
  return `${isoWeekdayName(iso, WEEKDAY_ABBR)}, ${formatCycleDate(iso)}`;
}

function daysBetween(fromIso: string, toIso: string): number {
  const a = parseIso(fromIso);
  const b = parseIso(toIso);
  const from = Date.UTC(a.year, a.month - 1, a.day);
  const to = Date.UTC(b.year, b.month - 1, b.day);
  return Math.round((to - from) / 86_400_000);
}

/** Plain-language distance from `today` to the meeting date. */
export function relativeDayLabel(iso: string, today: string): string {
  const diff = daysBetween(today, iso);
  if (diff === 0) return "Today";
  if (diff === 1) return "Tomorrow";
  if (diff === -1) return "Yesterday";
  if (diff < 0) return `${-diff} days ago`;
  if (diff < 7) return `In ${diff} days`;
  if (diff < 31) {
    const weeks = Math.round(diff / 7);
    return weeks <= 1 ? "In 1 week" : `In ${weeks} weeks`;
  }
  const months = Math.round(diff / 30);
  return months <= 1 ? "In 1 month" : `In ${months} months`;
}

/** Human recurrence summary for a recurring Note type. */
export function recurrenceSummary(noteType: MeetingNoteType): string {
  const { cadence_unit: unit, cadence_count: count } = noteType;
  if (unit === "manual") return "No recurring timing";
  const [singular, plural] = CADENCE_NOUN[unit];
  const interval = count <= 1 ? `Every ${singular}` : `Every ${count} ${plural}`;
  if (unit === "month" && noteType.anchor_week_of_month != null && noteType.anchor_weekday) {
    const ordinal = ORDINAL[noteType.anchor_week_of_month] ?? "selected";
    const weekday = WEEKDAY_FULL[noteType.anchor_weekday % 7] ?? "selected weekday";
    return `${interval} on the ${ordinal} ${weekday}`;
  }
  if (unit === "week" && noteType.anchor_weekday) {
    return `${interval} on ${WEEKDAY_FULL[noteType.anchor_weekday % 7] ?? "selected weekday"}`;
  }
  return interval;
}

export function todayIso(): string {
  return new Date().toISOString().slice(0, 10);
}

/**
 * Project every recurring Note onto the planner, sorted by the soonest next
 * meeting. Manual and disabled types are dropped; types with no projected date
 * sort last.
 */
export function planRecurringNotes(
  noteTypes: MeetingNoteType[],
  today = todayIso(),
): PlannedNote[] {
  return noteTypes
    .filter((noteType) => noteType.enabled && noteType.cadence_unit !== "manual")
    .map((noteType) => {
      const dates = noteType.upcoming_dates?.length
        ? noteType.upcoming_dates
        : noteType.next_meeting_date
          ? [noteType.next_meeting_date]
          : [];
      const nextDate = dates[0];
      return {
        noteType,
        nextDate,
        nextLabel: nextDate ? formatMeetingDate(nextDate) : undefined,
        relative: nextDate ? relativeDayLabel(nextDate, today) : undefined,
        summary: recurrenceSummary(noteType),
        upcoming: dates.slice(1).map(formatMeetingDate),
      };
    })
    .sort((a, b) => {
      if (a.nextDate === b.nextDate) return 0;
      if (!a.nextDate) return 1;
      if (!b.nextDate) return -1;
      return a.nextDate < b.nextDate ? -1 : 1;
    });
}

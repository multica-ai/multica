// Issue start_date / due_date are calendar days, not instants: the pickers
// offer no time-of-day input, so "Mar 1" must mean Mar 1 for every viewer
// regardless of timezone. They are transported as a date-only "YYYY-MM-DD"
// string. These helpers convert between that string and a Date WITHOUT letting
// the local timezone shift the day — the bug behind GH #3618 / MUL-2925 was
// serializing a local-midnight Date via toISOString() (which injects a tz) and
// reading it back through UTC day boundaries.
//
// Pure functions only (no React / DOM) so they can be shared with mobile.

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})/;

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/**
 * Serialize a Date the user picked in a calendar (local midnight of the chosen
 * day) to a "YYYY-MM-DD" string, using the LOCAL calendar components so the
 * stored day matches the day the user clicked.
 */
export function toDateOnly(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** Today as a "YYYY-MM-DD" string in the viewer's local calendar. */
export function todayDateOnly(): string {
  return toDateOnly(new Date());
}

/** "YYYY-MM-DD" of `days` from today in the viewer's local calendar. */
export function addDaysDateOnly(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return toDateOnly(d);
}

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — dynamic relative date ranges.
// A relative range is stored as (direction × amount × unit) and re-derived from
// "today" every time it is matched/rendered, so "in the last 7 days" stays live
// instead of freezing to the day it was picked. See relativeDateWindow().
export type RelativeDateUnit = "day" | "week" | "month" | "quarter" | "year";
export type RelativeDateDirection = "past" | "next";

export interface RelativeDateSpec {
  direction: RelativeDateDirection;
  amount: number;
  unit: RelativeDateUnit;
}

/**
 * Shift a Date by `amount` of `unit`, mutating in place. Month/quarter/year use
 * calendar math (setMonth/setFullYear) so "1 month ago" tracks the calendar,
 * not a fixed 30 days; day/week use day math.
 */
function shiftByUnit(d: Date, amount: number, unit: RelativeDateUnit): void {
  switch (unit) {
    case "day":
      d.setDate(d.getDate() + amount);
      break;
    case "week":
      d.setDate(d.getDate() + amount * 7);
      break;
    case "month":
      d.setMonth(d.getMonth() + amount);
      break;
    case "quarter":
      d.setMonth(d.getMonth() + amount * 3);
      break;
    case "year":
      d.setFullYear(d.getFullYear() + amount);
      break;
  }
}

/**
 * Resolve a relative spec to a concrete [from, to] calendar-day window anchored
 * at today (viewer's local calendar). "Past" ends today and reaches back;
 * "next" starts today and reaches forward. For the day unit the window is
 * inclusive-of-today over `amount` days (so "last 7 days" = today and the 6
 * days before, matching how Linear/shadcn label it); other units use a plain
 * calendar shift from today.
 */
export function relativeDateWindow(spec: RelativeDateSpec): {
  from: string;
  to: string;
} {
  const amount = Math.max(1, Math.floor(spec.amount || 1));
  const today = new Date();
  if (spec.unit === "day") {
    const offset = amount - 1;
    return spec.direction === "past"
      ? { from: addDaysDateOnly(-offset), to: todayDateOnly() }
      : { from: todayDateOnly(), to: addDaysDateOnly(offset) };
  }
  const edge = new Date(today);
  shiftByUnit(edge, spec.direction === "past" ? -amount : amount, spec.unit);
  const edgeStr = toDateOnly(edge);
  return spec.direction === "past"
    ? { from: edgeStr, to: todayDateOnly() }
    : { from: todayDateOnly(), to: edgeStr };
}

/**
 * Parse a date-only string into [year, month, day], tolerating a legacy full
 * ISO timestamp by reading its UTC calendar day. Returns null when unparseable.
 */
function parseParts(value: string): [number, number, number] | null {
  const m = DATE_ONLY.exec(value);
  if (m) return [Number(m[1]), Number(m[2]), Number(m[3])];
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return null;
  return [d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate()];
}

/**
 * Date anchored at UTC midnight of the calendar day. Use for timezone-safe
 * display (format with `timeZone: "UTC"`), gantt day-bucketing, and
 * chronological comparison.
 */
export function dateOnlyToUTCDate(
  value: string | null | undefined,
): Date | null {
  if (!value) return null;
  const parts = parseParts(value);
  if (!parts) return null;
  return new Date(Date.UTC(parts[0], parts[1] - 1, parts[2]));
}

/**
 * Date at LOCAL midnight of the calendar day. Use for a calendar picker's
 * `selected` / `defaultMonth`, which match on the local-time day.
 */
export function dateOnlyToLocalDate(
  value: string | null | undefined,
): Date | undefined {
  if (!value) return undefined;
  const parts = parseParts(value);
  if (!parts) return undefined;
  return new Date(parts[0], parts[1] - 1, parts[2]);
}

/**
 * Format a calendar day for display, timezone-safely (the day never shifts with
 * the viewer's timezone). Returns "" for an empty/unparseable value.
 */
export function formatDateOnly(
  value: string | null | undefined,
  options: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" },
  locale?: string,
): string {
  const d = dateOnlyToUTCDate(value);
  if (!d) return "";
  return d.toLocaleDateString(locale, { ...options, timeZone: "UTC" });
}

/** True when the calendar day is strictly before today (viewer's local day). */
export function isPastDateOnly(value: string | null | undefined): boolean {
  const d = dateOnlyToUTCDate(value);
  if (!d) return false;
  const today = dateOnlyToUTCDate(todayDateOnly());
  return today != null && d.getTime() < today.getTime();
}

// Issue start_date / due_date are calendar days, not instants: the pickers
// offer no time-of-day input, so "Mar 1" must mean Mar 1 for every viewer
// regardless of timezone. They are transported as a date-only "YYYY-MM-DD"
// string. These helpers convert between that string and a Date WITHOUT letting
// the local timezone shift the day — the bug behind GH #3618 / MUL-2925 was
// serializing a local-midnight Date via toISOString() (which injects a tz) and
// reading it back through UTC day boundaries.
//
// Pure functions only (no React / DOM) so they can be shared with mobile.

import { safeDateTimeFormat } from "../i18n/format-date-time";

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

/**
 * An instant's calendar [year, month, day] in an IANA timezone. Falls back to
 * UTC for a stale/ICU-unsupported timezone (must not throw). Single source for
 * "what calendar day is this instant in tz X" — both overdue judgment and the
 * relative-time day bucket build on it so they never derive "today" differently.
 * Reuses safeDateTimeFormat's cache + UTC fallback so this path can never drift
 * from the instant formatters' handling of a bad zone.
 */
export function calendarPartsInTimeZone(
  date: Date,
  timeZone: string,
): [number, number, number] {
  const parts = safeDateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const get = (type: string) =>
    Number(parts.find((p) => p.type === type)?.value);
  return [get("year"), get("month"), get("day")];
}

/**
 * Today as a "YYYY-MM-DD" string in the given IANA timezone. Falls back to UTC
 * if the timezone is unknown (a stale/ICU-unsupported value must not throw).
 */
export function todayDateOnlyInTimeZone(timeZone: string): string {
  const [y, m, d] = calendarPartsInTimeZone(new Date(), timeZone);
  return `${y}-${pad(m)}-${pad(d)}`;
}

/**
 * "YYYY-MM-DD" of `days` from today (negative = past) in the given IANA
 * timezone. Anchors on today-in-tz, then shifts by whole UTC days so a DST
 * transition never nudges the result onto a neighbouring day. Falls back to
 * UTC for a stale/ICU-unsupported timezone (must not throw). Use instead of a
 * browser-local "today" so a "due today / next week" preset agrees with the
 * Viewing-tz overdue judgment (`isPastDateOnly`).
 */
export function addDaysDateOnlyInTimeZone(
  days: number,
  timeZone: string,
): string {
  const [y, m, d] = calendarPartsInTimeZone(new Date(), timeZone);
  const shifted = new Date(Date.UTC(y, m - 1, d) + days * 86_400_000);
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

/**
 * True when the calendar day is strictly before today, where "today" is the
 * calendar day in the viewer's Viewing timezone — so the overdue badge agrees
 * with the due date the viewer sees on screen. See
 * docs/timezone-display-spec.md §4.
 */
export function isPastDateOnly(
  value: string | null | undefined,
  timeZone: string,
): boolean {
  const d = dateOnlyToUTCDate(value);
  if (!d) return false;
  const today = dateOnlyToUTCDate(todayDateOnlyInTimeZone(timeZone));
  return today != null && d.getTime() < today.getTime();
}

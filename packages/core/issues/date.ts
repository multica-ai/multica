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

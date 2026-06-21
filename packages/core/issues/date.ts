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

// ---------------------------------------------------------------------------
// Date-TIME helpers. Unlike the date-only helpers above, a value carrying a
// time-of-day is a real INSTANT and is shown in the viewer's local timezone
// (the same instant renders as different wall-clock times in different zones —
// correct for a timed deadline, and does not reintroduce #3618, which was
// specifically about all-day days shifting).
// ---------------------------------------------------------------------------

const DATE_TIME_OPTIONS: Intl.DateTimeFormatOptions = {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
};

/**
 * True when the value carries a time-of-day (an RFC3339 instant such as
 * "2026-02-01T14:30:00Z"), as opposed to a bare "YYYY-MM-DD" calendar day.
 */
export function hasTime(value: string | null | undefined): boolean {
  return typeof value === "string" && /T\d{2}:\d{2}/.test(value);
}

/**
 * Format an RFC3339 instant for display in the viewer's local timezone, e.g.
 * "Feb 1, 14:30". `timeZone` is a test-only pin for determinism; production
 * omits it so the host timezone is used. Returns "" for empty/unparseable.
 */
export function formatDateTime(
  value: string | null | undefined,
  options: Intl.DateTimeFormatOptions = DATE_TIME_OPTIONS,
  locale?: string,
  timeZone?: string,
): string {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(locale, timeZone ? { ...options, timeZone } : options);
}

/**
 * Render an issue start/due value: with time-of-day when it is a timed instant
 * (local TZ), or as a bare calendar day when it is date-only (UTC-safe, never
 * shifts). `timeZone` is for tests; production omits it.
 */
export function formatScheduleDate(
  value: string | null | undefined,
  locale?: string,
  timeZone?: string,
): string {
  return hasTime(value)
    ? formatDateTime(value, DATE_TIME_OPTIONS, locale, timeZone)
    : formatDateOnly(value, { month: "short", day: "numeric" }, locale);
}

/** Default time-of-day applied when a picker gains a day but no time yet. */
export const DEFAULT_DUE_TIME = { h: 23, m: 59 } as const;
export const DEFAULT_START_TIME = { h: 9, m: 0 } as const;

/**
 * Parse a stored schedule value into a local Date for seeding a picker (its
 * Calendar `selected` day and time-of-day field). A timed instant is parsed
 * directly; a legacy date-only value goes through dateOnlyToLocalDate so its
 * day does not shift across the UTC boundary. Returns undefined when empty or
 * unparseable.
 */
export function isoToLocalDate(value: string | null | undefined): Date | undefined {
  if (!value) return undefined;
  if (hasTime(value)) {
    const d = new Date(value);
    return Number.isNaN(d.getTime()) ? undefined : d;
  }
  return dateOnlyToLocalDate(value);
}

/**
 * Combine a local wall-clock Date (the picker's chosen day + time) into an
 * RFC3339 UTC instant. This is the timezone-safe write path: the user's local
 * selection becomes an unambiguous instant, so it renders back as the same
 * wall-clock time for that user and the correct local time for everyone else.
 */
export function localDateTimeToIso(date: Date): string {
  return date.toISOString();
}

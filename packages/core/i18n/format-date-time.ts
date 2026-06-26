// Pure Intl-based formatters for INSTANTS (TIMESTAMPTZ values). Both the
// visible time and its tooltip render here so the timezone + locale enter in
// exactly one place. No React / DOM, so this is testable and reusable by
// mobile. Calendar days (issue start_date / due_date) are NOT instants and
// must NOT pass through here — they stay in issues/date.ts `formatDateOnly`.

export type InstantFormatMode = "datetime" | "date" | "time";

export interface FormatInstantOptions {
  /** BCP-47 locale, e.g. the i18next short code "en" / "zh-Hans". */
  locale: string;
  /** IANA timezone the instant is rendered in (the viewer's Viewing tz). */
  timeZone: string;
  /** Which components to show. Defaults to full date + time. */
  mode?: InstantFormatMode;
}

function toDate(value: string | number | Date | null | undefined): Date | null {
  if (value == null) return null;
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

// Intl.DateTimeFormat construction is one of the costliest Intl ops, and lists
// re-render per row per frame, so cache built formatters by (locale, options).
// The locale/timeZone/mode space is tiny (≤4 locales × a handful of zones ×
// 3 modes), so the cache stays small; the JSON key is far cheaper than the
// construction it avoids.
const formatterCache = new Map<string, Intl.DateTimeFormat>();

// Intl.DateTimeFormat throws RangeError on an unknown timeZone or locale. A
// stale or ICU-unsupported user.timezone (Go tzdata and the browser's ICU do
// not accept exactly the same zone set) — or a malformed Language locale —
// must never white-screen every page that renders a timestamp. The fallback
// ladder degrades the LOCALE before the timeZone: a wrong display language is
// far less harmful than discarding the viewer's timezone and silently
// rendering every instant in UTC, hours off the real wall-clock. So a bad
// locale alone keeps the requested zone; only a genuinely unsupported zone
// falls back to UTC. Exported because the scheduling-axis formatters reuse the
// same cache + safe construction. Offset-token extraction does NOT route
// through here — see shortOffsetToken, which must not mask a bad zone as UTC.
export function safeDateTimeFormat(
  locale: string,
  options: Intl.DateTimeFormatOptions,
): Intl.DateTimeFormat {
  const key = `${locale}\u0000${JSON.stringify(options)}`;
  const cached = formatterCache.get(key);
  if (cached) return cached;
  let fmt: Intl.DateTimeFormat;
  try {
    fmt = new Intl.DateTimeFormat(locale, options);
  } catch {
    try {
      // Bad locale only: keep the requested timeZone, degrade language to "en".
      fmt = new Intl.DateTimeFormat("en", options);
    } catch {
      try {
        // Bad timeZone: keep the locale, fall back to UTC.
        fmt = new Intl.DateTimeFormat(locale, { ...options, timeZone: "UTC" });
      } catch {
        // Both unsupported: neutral locale + UTC.
        fmt = new Intl.DateTimeFormat("en", { ...options, timeZone: "UTC" });
      }
    }
  }
  formatterCache.set(key, fmt);
  return fmt;
}

// Explicit component options, never dateStyle/timeStyle — the latter cannot be
// combined with timeZoneName, which the offset tooltip needs. Visible
// "datetime" omits seconds (noise for inline cells); the tooltip keeps full
// precision via FULL_OPTS below.
function componentOptions(mode: InstantFormatMode): Intl.DateTimeFormatOptions {
  switch (mode) {
    case "date":
      return { year: "numeric", month: "short", day: "numeric" };
    case "time":
      return { hour: "2-digit", minute: "2-digit", second: "2-digit" };
    default:
      return {
        year: "numeric",
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      };
  }
}

// Tooltip precision: full date + time WITH seconds.
const FULL_OPTS: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
};

// The calendar year of an instant in a timezone. Uses this module's own safe
// formatter (not issues/date.ts calendarPartsInTimeZone, which would form an
// import cycle) so the year-of and the visible rendering share the same UTC
// fallback for a bad zone.
function yearInTimeZone(date: Date, timeZone: string): number {
  const parts = safeDateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
  }).formatToParts(date);
  return Number(parts.find((p) => p.type === "year")?.value);
}

/**
 * Render an instant in the given timezone + locale. Returns "" for an
 * empty/unparseable value so callers can render nothing without guarding.
 *
 * In "date" mode the year is dropped when the instant falls in the viewer's
 * current calendar year (evaluated in `timeZone`) and kept otherwise, so a
 * same-year date reads "Mar 1" while other years stay unambiguous ("Mar 1,
 * 2025"). datetime/time modes always keep their full components.
 */
export function formatInstant(
  value: string | number | Date | null | undefined,
  { locale, timeZone, mode = "datetime" }: FormatInstantOptions,
): string {
  const date = toDate(value);
  if (!date) return "";
  let options = componentOptions(mode);
  if (
    mode === "date" &&
    yearInTimeZone(date, timeZone) === yearInTimeZone(new Date(), timeZone)
  ) {
    options = { month: "short", day: "numeric" };
  }
  return safeDateTimeFormat(locale, { ...options, timeZone }).format(date);
}

// CJK locales use full-width parentheses; everything else uses spaced
// half-width ones (the leading space keeps the en tooltip readable).
function usesFullWidthPunctuation(locale: string): boolean {
  return /^(zh|ja|ko)\b/i.test(locale);
}

// Offset extraction deliberately does NOT use safeDateTimeFormat: its UTC
// fallback would yield a misleading "GMT+0" for a stale/ICU-unsupported zone,
// hiding the failure from callers that want to degrade to the raw zone name. A
// bad zone (or `shortOffset` rejected by an old engine: Safari < 15.4 / old
// WebViews) means the offset is genuinely unknowable, so return null and let
// the caller decide. Only the LOCALE degrades to "en" (the token's VALUE is
// locale-independent). Cached per (locale, timeZone); null is cached too so a
// rejected zone is not retried each render.
const offsetFormatterCache = new Map<string, Intl.DateTimeFormat | null>();

function offsetFormatter(
  locale: string,
  timeZone: string,
): Intl.DateTimeFormat | null {
  const key = `${locale} ${timeZone}`;
  if (offsetFormatterCache.has(key)) {
    return offsetFormatterCache.get(key) ?? null;
  }
  // timeZoneName needs at least one displayed field; hour is discarded, only
  // the offset token is kept.
  const options: Intl.DateTimeFormatOptions = {
    timeZone,
    timeZoneName: "shortOffset",
    hour: "numeric",
  };
  let fmt: Intl.DateTimeFormat | null;
  try {
    fmt = new Intl.DateTimeFormat(locale, options);
  } catch {
    try {
      // Bad locale only: keep the zone, degrade language to "en".
      fmt = new Intl.DateTimeFormat("en", options);
    } catch {
      // Unsupported zone or `shortOffset` value: no knowable offset.
      fmt = null;
    }
  }
  offsetFormatterCache.set(key, fmt);
  return fmt;
}

/**
 * The GMT-offset token (e.g. "GMT+8") for an instant in a timezone, or "" if
 * the engine can't produce one (unknown zone, or no shortOffset support).
 * Single source for offset extraction — the instant tooltip here plus the
 * scheduling-axis timezone pickers all reuse it instead of re-implementing the
 * formatToParts dance. "" lets callers fall back to the raw zone name rather
 * than show a wrong offset.
 */
export function shortOffsetToken(
  date: Date,
  locale: string,
  timeZone: string,
): string {
  const fmt = offsetFormatter(locale, timeZone);
  if (!fmt) return "";
  const parts = fmt.formatToParts(date);
  return parts.find((p) => p.type === "timeZoneName")?.value ?? "";
}

/**
 * The tooltip string for an instant: full date-time in the viewer's timezone,
 * with a GMT-offset suffix appended manually (built-in timeZoneName lands
 * mid-string in CJK locales, breaking cross-locale ordering).
 *   en → "Mar 1, 2026, 02:30:45 PM (GMT+8)"
 *   zh → "2026年3月1日 14:30:45（GMT+8）"
 */
export function formatInstantWithOffset(
  value: string | number | Date | null | undefined,
  { locale, timeZone }: Omit<FormatInstantOptions, "mode">,
): string {
  const date = toDate(value);
  if (!date) return "";
  const base = safeDateTimeFormat(locale, {
    ...FULL_OPTS,
    timeZone,
  }).format(date);
  const offset = shortOffsetToken(date, locale, timeZone);
  if (!offset) return base;
  const [open, close] = usesFullWidthPunctuation(locale)
    ? ["（", "）"]
    : [" (", ")"];
  return `${base}${open}${offset}${close}`;
}

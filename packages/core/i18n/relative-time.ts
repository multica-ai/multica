// Single source of truth for the relative-time gradient ("3h ago" / "in 3h").
// Pure: no React, no i18n, no DOM — so the views hook (i18n strings) and mobile
// (English strings) map the SAME bucketing to their own labels instead of
// drifting copies.
//
// The gradient is symmetric: an instant in the past reads "3h ago", an instant
// in the future reads "in 3h". Direction is carried by `future` on each bucket;
// `just_now` has no direction (a sub-minute gap either way, which also absorbs
// clock skew). The magnitude buckets are identical in both directions.
//
// Day granularity is CALENDAR-based: two instants on different calendar days
// read as "1d ago" even when fewer than 24h apart, so the label agrees with the
// date shown beside it. That makes the day+ bucket timezone-dependent by design
// (a near-midnight instant can be "today" for one viewer and "1d ago" for
// another), which is why callers pass the viewer's timezone. The sub-day
// buckets (minutes/hours) stay elapsed-based and are timezone-independent.
//
// There is no absolute-date fallback: past the day cap the gradient continues
// into calendar months, then calendar years, staying relative indefinitely.

import { calendarPartsInTimeZone } from "../issues/date";

export type RelativeTimeBucket =
  | { kind: "just_now" }
  | { kind: "minutes"; count: number; future: boolean }
  | { kind: "hours"; count: number; future: boolean }
  | { kind: "days"; count: number; future: boolean }
  | { kind: "months"; count: number; future: boolean }
  | { kind: "years"; count: number; future: boolean }
  // Non-finite input (unparseable date): callers render a neutral placeholder
  // instead of a blank or a thrown RangeError from the calendar-day math.
  | { kind: "invalid" };

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

// At more than this many CALENDAR days apart, "Nd" stops being useful and the
// gradient switches to calendar months.
export const RELATIVE_DAYS_TO_MONTHS = 30;
const MONTHS_PER_YEAR = 12;

// The viewer-tz calendar parts of an instant, as returned by
// calendarPartsInTimeZone: [year, month, day]. Resolving these is the costly
// Intl op (formatToParts) and is NOT result-cached, so the day+ buckets compute
// them once per instant and derive both the calendar-day and calendar-month
// difference from the same tuples rather than re-formatting.
type CalendarParts = [number, number, number];

// Day number from calendar parts (whole days since the Unix epoch in that
// zone). Lets us count calendar days between two instants the way a human
// reading a wall calendar would, instead of dividing elapsed ms.
function dayNumberFromParts([year, month, day]: CalendarParts): number {
  return Math.floor(Date.UTC(year, month - 1, day) / DAY);
}

// Whole calendar months between an earlier and a later instant, counted the way
// a wall calendar would: a month only counts once the later instant has reached
// the earlier one's day-of-month. Mirrors the calendar-day logic one level up
// so "1mo" means "roughly a month" rather than 30 elapsed days. Callers pass
// earlier→later; direction is applied separately.
function monthDiffFromParts(
  [ey, em, ed]: CalendarParts,
  [ly, lm, ld]: CalendarParts,
): number {
  let months = (ly - ey) * 12 + (lm - em);
  if (ld < ed) months -= 1;
  return months;
}

/**
 * Bucket the gap between two instants into a relative-time label descriptor.
 * The result is symmetric: `future` is true when `thenMs` is after `nowMs`.
 *  - non-finite input (unparseable date) → `invalid`
 *  - sub-minute either direction (incl. clock skew) → `just_now`
 *  - `< 1h` → `minutes`, `< 24h` → `hours` (elapsed, timezone-independent)
 *  - `≥ 24h` → calendar-day difference in `timeZone` up to
 *    `RELATIVE_DAYS_TO_MONTHS`; beyond it → calendar `months`, then `years`.
 */
export function relativeTimeBucket(
  thenMs: number,
  nowMs: number,
  timeZone: string,
): RelativeTimeBucket {
  if (!Number.isFinite(thenMs) || !Number.isFinite(nowMs)) {
    return { kind: "invalid" };
  }
  const diff = nowMs - thenMs;
  const elapsed = Math.abs(diff);
  const future = diff < 0;
  if (elapsed < MINUTE) return { kind: "just_now" };
  if (elapsed < HOUR) {
    return { kind: "minutes", count: Math.floor(elapsed / MINUTE), future };
  }
  if (elapsed < DAY) {
    return { kind: "hours", count: Math.floor(elapsed / HOUR), future };
  }
  // Day+ buckets are calendar-based; order the pair earlier→later so the same
  // arithmetic serves both directions. Resolve each instant's calendar parts
  // once and derive both the day and month difference from them — formatToParts
  // is the costly Intl op and this runs per row per render.
  const earlier = future ? nowMs : thenMs;
  const later = future ? thenMs : nowMs;
  const earlierParts = calendarPartsInTimeZone(new Date(earlier), timeZone);
  const laterParts = calendarPartsInTimeZone(new Date(later), timeZone);
  const days = dayNumberFromParts(laterParts) - dayNumberFromParts(earlierParts);
  // On a DST fall-back day (e.g. a 25h day in America/New_York) a ≥24h elapsed
  // gap can still land on the SAME calendar day → days === 0. The date shown
  // beside it reads "today", so keep an hours label rather than forcing a
  // contradictory "1d".
  if (days === 0) return { kind: "hours", count: Math.floor(elapsed / HOUR), future };
  if (days <= RELATIVE_DAYS_TO_MONTHS) return { kind: "days", count: days, future };
  // More than 30 days apart always spans at least one calendar month; clamp to
  // 1 to guard the rare end-of-month edge where the day-of-month adjustment
  // would otherwise drop a 31-day gap to 0 months.
  const months = Math.max(1, monthDiffFromParts(earlierParts, laterParts));
  if (months < MONTHS_PER_YEAR) return { kind: "months", count: months, future };
  return { kind: "years", count: Math.floor(months / MONTHS_PER_YEAR), future };
}

/**
 * Compact single-unit duration formatting for the "time in status" hover.
 *
 * Deliberately different from the two-unit formatters elsewhere in the app
 * (`formatDuration` in dashboard/utils.ts renders "2d 3h"; the agent-activity
 * card renders "1h 03m"). Those describe ONE running thing, where the second
 * unit is the precision a user is actually watching. This list describes
 * several statuses side by side, and its job is comparison — "most of the time
 * went to code review" — not precision. A column of "11d 4h" / "56m 12s" /
 * "9s" makes that comparison harder to eyeball than "11d" / "56min" / "9s",
 * and the extra digits are noise at the scale that matters here.
 *
 * Truncates rather than rounds, so a status is never credited with time that
 * has not elapsed: 59 minutes reads "59min", not "1h".
 */

/** The unit a duration renders in — the key half of the i18n lookup. */
export type StatusDurationUnit = "seconds" | "minutes" | "hours" | "days";

export interface StatusDurationParts {
  value: number;
  unit: StatusDurationUnit;
}

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * Splits a second count into the largest unit that yields a value >= 1.
 *
 * Returned as parts rather than a finished string so the caller can render the
 * suffix through i18n. CJK locales do not use the Latin "min"/"d" abbreviations,
 * and a hardcoded suffix here would be untranslatable.
 */
export function statusDurationParts(seconds: number): StatusDurationParts {
  // Negative or non-finite input means a corrupt timestamp upstream. Clamp to
  // zero: "0s" is a sane thing to show, "-3s" or "NaNd" is not.
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return { value: 0, unit: "seconds" };
  }
  if (seconds < MINUTE) return { value: Math.floor(seconds), unit: "seconds" };
  if (seconds < HOUR) return { value: Math.floor(seconds / MINUTE), unit: "minutes" };
  if (seconds < DAY) return { value: Math.floor(seconds / HOUR), unit: "hours" };
  return { value: Math.floor(seconds / DAY), unit: "days" };
}

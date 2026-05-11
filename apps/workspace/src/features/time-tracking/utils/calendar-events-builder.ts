/**
 * Utility functions for preprocessing time entries before passing to react-big-calendar.
 *
 * Two edge cases require special handling:
 * 1. Cross-midnight entries: RBC promotes them to all-day events — split at midnight instead.
 * 2. Sub-minute entries: RBC's DnD plugin adds an entire day to events that start and end
 *    within the same minute — push the display end past the minute boundary.
 */

const MINUTE_MS = 60_000;

/**
 * Splits a time range at midnight boundaries to prevent react-big-calendar from
 * promoting cross-midnight entries to all-day events.
 *
 * Each segment ends 1ms before midnight so the entry stays in the time grid.
 */
export function splitAtMidnight(start: Date, end: Date): Array<{ start: Date; end: Date }> {
  const segments: Array<{ start: Date; end: Date }> = [];
  let cursor = start;

  while (true) {
    const nextMidnight = new Date(cursor);
    nextMidnight.setHours(24, 0, 0, 0);

    if (nextMidnight > end) {
      segments.push({ end, start: cursor });
      break;
    }

    // Clip 1ms before midnight to keep it in the time grid.
    const segmentEnd = new Date(nextMidnight.getTime() - 1);
    segments.push({ end: segmentEnd, start: cursor });

    // Entry stops exactly at midnight — avoid a zero-duration segment on the next day.
    if (nextMidnight.getTime() === end.getTime()) break;

    cursor = nextMidnight;
  }

  return segments;
}

/**
 * Ensures sub-minute entries (start and end within same minute) have an end
 * time that crosses the minute boundary.
 *
 * RBC's DnD plugin incorrectly adds an entire day to events where start/end
 * share the same minute. Only affects the display end — does not modify source data.
 */
export function displayEndForCalendar(start: Date, end: Date): Date {
  const startMinute = Math.floor(start.getTime() / MINUTE_MS);
  const endMinute = Math.floor(end.getTime() / MINUTE_MS);
  if (startMinute !== endMinute) return end;
  return new Date((startMinute + 1) * MINUTE_MS);
}

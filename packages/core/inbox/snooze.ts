/** The times the Inbox offers to snooze an issue until. */
export type InboxSnoozePreset = "in_one_hour" | "later_today" | "tomorrow" | "next_week";

export interface InboxSnoozeOption {
  preset: InboxSnoozePreset;
  until: Date;
}

/** The server refuses a snooze further out than this. */
export const INBOX_SNOOZE_MAX_DAYS = 366;

const MORNING_HOUR = 9;
const EVENING_HOUR = 18;
const HOUR_MS = 60 * 60 * 1000;

function localDay(now: Date, addDays: number, hour: number): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() + addDays, hour, 0, 0, 0);
}

/**
 * Snooze choices for `now`, in the viewer's own time zone, soonest first.
 *
 * "Later today" is the evening, and is offered only while the evening is more
 * than an hour away: after that it would repeat "in one hour" or name a time
 * that has passed. "Next week" is Monday morning, and is left out on a Sunday,
 * when it is the same morning as "tomorrow".
 */
export function inboxSnoozeOptions(now: Date): InboxSnoozeOption[] {
  const options: InboxSnoozeOption[] = [
    { preset: "in_one_hour", until: new Date(now.getTime() + HOUR_MS) },
  ];
  const evening = localDay(now, 0, EVENING_HOUR);
  if (evening.getTime() - now.getTime() > HOUR_MS) {
    options.push({ preset: "later_today", until: evening });
  }
  options.push({ preset: "tomorrow", until: localDay(now, 1, MORNING_HOUR) });
  // Sunday is 0, so Monday is one day away then and seven on a Monday.
  const daysToMonday = ((8 - now.getDay()) % 7) || 7;
  if (daysToMonday > 1) {
    options.push({ preset: "next_week", until: localDay(now, daysToMonday, MORNING_HOUR) });
  }
  return options;
}

/**
 * A time the user typed into a `datetime-local` field, or null when it cannot
 * be snoozed until: unparseable, not in the future, or past the server's limit.
 */
export function parseInboxSnoozeInput(value: string, now: Date): Date | null {
  if (!value) return null;
  const until = new Date(value);
  const at = until.getTime();
  if (Number.isNaN(at) || at <= now.getTime()) return null;
  if (at > now.getTime() + INBOX_SNOOZE_MAX_DAYS * 24 * HOUR_MS) return null;
  return until;
}

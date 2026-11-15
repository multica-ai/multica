import type { InboxItem } from "../types";

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
// Before this hour "tomorrow morning" still means the coming morning.
const NIGHT_END_HOUR = 5;
const HOUR_MS = 60 * 60 * 1000;

function localDay(now: Date, addDays: number, hour: number): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate() + addDays, hour, 0, 0, 0);
}

/**
 * Snooze choices for `now`, in the viewer's own time zone, soonest first.
 *
 * "Later today" is the evening, and is offered only while the evening is more
 * than an hour away: after that it would repeat "in one hour" or name a time
 * that has passed. "Tomorrow" is the next morning, which after midnight and
 * before 5:00 is the same day's. "Next week" is Monday morning, and is left
 * out when it is the same morning as "tomorrow".
 */
export function inboxSnoozeOptions(now: Date): InboxSnoozeOption[] {
  const options: InboxSnoozeOption[] = [
    { preset: "in_one_hour", until: new Date(now.getTime() + HOUR_MS) },
  ];
  const evening = localDay(now, 0, EVENING_HOUR);
  if (evening.getTime() - now.getTime() > HOUR_MS) {
    options.push({ preset: "later_today", until: evening });
  }
  const tomorrow = localDay(now, now.getHours() < NIGHT_END_HOUR ? 0 : 1, MORNING_HOUR);
  options.push({ preset: "tomorrow", until: tomorrow });
  // Sunday is 0, so Monday is one day away then and seven on a Monday.
  const daysToMonday = ((8 - now.getDay()) % 7) || 7;
  const monday = localDay(now, daysToMonday, MORNING_HOUR);
  if (monday.getTime() !== tomorrow.getTime()) {
    options.push({ preset: "next_week", until: monday });
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

function snoozeTime(item: Pick<InboxItem, "snoozed_until">): number | null {
  if (!item.snoozed_until) return null;
  const at = new Date(item.snoozed_until).getTime();
  return Number.isNaN(at) ? null : at;
}

/** Whether the row is out of the inbox at `now`: snoozed until later. */
export function isInboxItemSnoozed(item: Pick<InboxItem, "snoozed_until">, now = Date.now()): boolean {
  const at = snoozeTime(item);
  return at !== null && at > now;
}

/**
 * When the row came back from a snooze on its own, or null. A row written
 * after the snooze ran out is newer than that and reads as itself.
 */
export function inboxSnoozeReturnedAt(
  item: Pick<InboxItem, "snoozed_until" | "created_at">,
  now = Date.now(),
): number | null {
  const at = snoozeTime(item);
  if (at === null || at > now) return null;
  return at > new Date(item.created_at).getTime() ? at : null;
}

/**
 * Where a row sorts in the inbox: when it arrived, or when it came back from a
 * snooze, whichever is later. An issue back from a snooze is on top.
 */
export function inboxRowTime(item: Pick<InboxItem, "snoozed_until" | "created_at">, now = Date.now()): number {
  return inboxSnoozeReturnedAt(item, now) ?? new Date(item.created_at).getTime();
}

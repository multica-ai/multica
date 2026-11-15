import type { InboxItem } from "../types";

/** How long the Inbox offers to snooze an issue for. */
export type InboxSnoozePreset = "hours_1" | "hours_3" | "days_1" | "days_3" | "weeks_1";

export interface InboxSnoozeOption {
  preset: InboxSnoozePreset;
  until: Date;
}

/** The server refuses a snooze further out than this. */
export const INBOX_SNOOZE_MAX_DAYS = 366;

/** Where the date picker's time starts, and where a day-long snooze made at night lands. */
export const INBOX_SNOOZE_MORNING = "09:00";

const MORNING_HOUR = 9;
// A snooze of a day or more made between these hours comes back in the morning.
const NIGHT_START_HOUR = 22;
const NIGHT_END_HOUR = 6;
const HOUR_MS = 60 * 60 * 1000;

const PRESETS: { preset: InboxSnoozePreset; hours?: number; days?: number }[] = [
  { preset: "hours_1", hours: 1 },
  { preset: "hours_3", hours: 3 },
  { preset: "days_1", days: 1 },
  { preset: "days_3", days: 3 },
  { preset: "weeks_1", days: 7 },
];

/**
 * Snooze choices for `now`, in the viewer's own time zone, soonest first.
 *
 * Hours are added as they are. Days keep the clock time, counted in calendar
 * days so a daylight-saving change does not shift it, except at night, when
 * the same time days later would be the middle of a night too: then they land
 * at 9:00.
 */
export function inboxSnoozeOptions(now: Date): InboxSnoozeOption[] {
  const hour = now.getHours();
  const night = hour >= NIGHT_START_HOUR || hour < NIGHT_END_HOUR;
  return PRESETS.map(({ preset, hours, days }) => {
    if (hours) return { preset, until: new Date(now.getTime() + hours * HOUR_MS) };
    const until = new Date(now);
    until.setDate(until.getDate() + (days ?? 0));
    if (night) until.setHours(MORNING_HOUR, 0, 0, 0);
    return { preset, until };
  });
}

/**
 * The time a day picked on the calendar and an "HH:MM" make, or null when it
 * cannot be snoozed until: not in the future, or past the server's limit.
 */
export function inboxSnoozePickedTime(day: Date, time: string, now: Date): Date | null {
  const [hours, minutes] = time.split(":").map(Number);
  if (!Number.isInteger(hours) || !Number.isInteger(minutes)) return null;
  const until = new Date(day.getFullYear(), day.getMonth(), day.getDate(), hours, minutes);
  const at = until.getTime();
  if (at <= now.getTime()) return null;
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

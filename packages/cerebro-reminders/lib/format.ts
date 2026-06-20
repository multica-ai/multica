// Reminder time + anchor formatting for the overview list and opened card.
// Kept pure (an injectable `now`) so it is trivially testable. All user-facing
// words come in via the localized string table — this module never hardcodes a
// language. Dates are formatted in the active locale, not a fixed one.

import type { AnchorType, Reminder } from "../core/types";
import type { ReminderStringTable } from "../strings";

function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

function addDays(d: Date, days: number): Date {
  const copy = new Date(d);
  copy.setDate(copy.getDate() + days);
  return copy;
}

function capitalize(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toUpperCase() + s.slice(1);
}

function timeFmt(locale: string) {
  return new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" });
}
function weekdayFmt(locale: string) {
  return new Intl.DateTimeFormat(locale, { weekday: "short" });
}
function dateFmt(locale: string) {
  return new Intl.DateTimeFormat(locale, { day: "numeric", month: "short" });
}

/**
 * Short label for a reminder row: "Today 14:00", "Tomorrow 09:00", or
 * "Fri 20 Jun" once it is further out than tomorrow — in the active locale.
 */
export function formatReminderWhen(
  iso: string,
  s: ReminderStringTable,
  locale: string,
  now: Date = new Date(),
): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const time = timeFmt(locale).format(d);
  if (isSameDay(d, now)) return `${s.date_today} ${time}`;
  if (isSameDay(d, addDays(now, 1))) return `${s.date_tomorrow} ${time}`;
  const weekday = capitalize(weekdayFmt(locale).format(d).replace(/\.$/, ""));
  return `${weekday} ${dateFmt(locale).format(d)}`;
}

/**
 * Long label for the opened card: "Due today at 14:00" / "Due tomorrow at
 * 09:00" / "Due Fri 20 Jun at 14:00" — in the active locale.
 */
export function formatReminderDue(
  iso: string,
  s: ReminderStringTable,
  locale: string,
  now: Date = new Date(),
): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const time = timeFmt(locale).format(d);
  if (isSameDay(d, now)) return `${s.due_today_prefix} ${time}`;
  if (isSameDay(d, addDays(now, 1))) return `${s.due_tomorrow_prefix} ${time}`;
  return `${s.due_prefix} ${weekdayFmt(locale).format(d)} ${dateFmt(locale).format(d)} ${s.due_at_word} ${time}`;
}

/**
 * Source line for a comment anchor: "DM · Filip", "#kampagner", or the
 * conversation title for a regular issue.
 */
export function formatReminderSource(
  kind: string | undefined,
  title: string | undefined,
  s: ReminderStringTable,
): string {
  const t = (title ?? "").trim();
  if (kind === "dm") return t ? `${s.source_dm} · ${t}` : s.source_direct_message;
  if (kind === "channel") return t ? `#${t}` : s.source_channel;
  return t || s.source_message;
}

// What the opened card's "go to" button does for each anchor type.
export interface ReminderAnchor {
  type: AnchorType;
  /** Human label for the anchor line, e.g. "DM · Filip", "Issue · Login-bug". */
  label: string;
  /** Label for the navigate button, or null when there is nowhere to go. */
  actionLabel: string | null;
}

/**
 * Describe a reminder's anchor for the overview row + opened card: the source
 * line and what "go to" should say. A free reminder (anchor "none") has no
 * source and no navigation — its text carries the whole reminder.
 */
export function formatReminderAnchor(
  reminder: Reminder,
  s: ReminderStringTable,
): ReminderAnchor {
  switch (reminder.anchor_type) {
    case "comment":
      return {
        type: "comment",
        label: formatReminderSource(reminder.conversation_kind, reminder.conversation_title, s),
        actionLabel: reminder.conversation_id ? s.go_to_message : null,
      };
    case "issue":
      return {
        type: "issue",
        label: `${s.prefix_issue} · ${(reminder.conversation_title ?? "").trim() || s.anchor_untitled}`,
        actionLabel: reminder.conversation_id ? s.go_to_issue : null,
      };
    case "project":
      return {
        type: "project",
        label: `${s.prefix_project} · ${(reminder.project_title ?? "").trim() || s.anchor_untitled}`,
        actionLabel: reminder.project_id ? s.go_to_project : null,
      };
    case "chat_message":
      return {
        type: "chat_message",
        label: `${s.prefix_chat} · ${(reminder.chat_session_title ?? "").trim() || s.anchor_untitled}`,
        actionLabel: reminder.chat_session_id ? s.go_to_chat : null,
      };
    default:
      return { type: "none", label: s.anchor_free, actionLabel: null };
  }
}

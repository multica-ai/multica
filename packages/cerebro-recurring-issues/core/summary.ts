// FIR-1639 / FIR-1682 — plain-English summary of a recurrence rule.
//
// This is a pure, react-free function so it can be unit-tested without a DOM
// and reused by the panel's live "What will happen" box. It never computes a
// concrete next date — it describes the rule, so it stays deterministic and
// free of clock/timezone coupling.

import type { Frequency, RecurrenceWriteInput } from "./types";

const STATUS_LABELS: Record<string, string> = {
  backlog: "Backlog",
  todo: "To do",
  in_progress: "In progress",
  in_review: "In review",
  done: "Done",
  blocked: "Blocked",
  cancelled: "Cancelled",
};

const WEEKDAY_NAMES: Record<number, string> = {
  1: "Monday",
  2: "Tuesday",
  3: "Wednesday",
  4: "Thursday",
  5: "Friday",
  6: "Saturday",
  7: "Sunday",
};

/** Human-friendly label for an issue status (e.g. "in_progress" → "In progress"). */
export function statusLabel(status: string | undefined): string {
  if (!status) return "To do";
  return STATUS_LABELS[status] ?? status;
}

/** "Monday", "Monday and Tuesday", "Monday, Tuesday and Wednesday". */
function joinList(items: string[]): string {
  if (items.length === 0) return "";
  if (items.length === 1) return items[0]!;
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

function weekdayPhrase(weekdays: number[] | undefined): string {
  const days = (weekdays ?? [])
    .slice()
    .sort((a, b) => a - b)
    .map((d) => WEEKDAY_NAMES[d])
    .filter((d): d is string => Boolean(d));
  return joinList(days);
}

/** How often the rule fires, e.g. "Every Monday", "Every 2 weeks", "Daily". */
export function frequencyPhrase(form: RecurrenceWriteInput): string {
  const n = form.interval_count && form.interval_count > 0 ? form.interval_count : 1;
  const freq: Frequency = form.frequency ?? "weekly";

  switch (freq) {
    case "daily":
      return n === 1 ? "Every day" : `Every ${n} days`;
    case "weekly": {
      const days = weekdayPhrase(form.weekdays);
      if (days) {
        return n === 1 ? `Every ${days}` : `Every ${n} weeks on ${days}`;
      }
      return n === 1 ? "Every week" : `Every ${n} weeks`;
    }
    case "monthly":
      return n === 1 ? "Every month" : `Every ${n} months`;
    case "yearly":
      return n === 1 ? "Every year" : `Every ${n} years`;
    case "every_weekday":
      return "Every weekday";
    case "days_after": {
      const d = form.days_after ?? 0;
      return d === 1 ? "1 day after it's closed" : `${d} days after it's closed`;
    }
    case "custom": {
      const days = weekdayPhrase(form.weekdays);
      return days ? `On ${days}` : "On a custom schedule";
    }
    default:
      return "On a schedule";
  }
}

function recurPhrase(form: RecurrenceWriteInput): string {
  if (form.recur_forever ?? true) return "It runs forever.";
  if (form.end_date) return "It runs until the end date.";
  if (form.max_occurrences && form.max_occurrences > 0) {
    return `It runs ${form.max_occurrences} times.`;
  }
  return "It runs until you stop it.";
}

/**
 * Build the plain-English "What will happen" sentence from a recurrence form,
 * covering both completion-based and schedule-based modes.
 */
export function summarizeRecurrencePlan(form: RecurrenceWriteInput): string {
  const freq = frequencyPhrase(form);
  const startsAs = statusLabel(form.new_status);

  if (form.trigger_type === "schedule") {
    const stale =
      form.stale_handling === "auto_close"
        ? `The old one is closed as ${statusLabel(form.stale_status)}.`
        : "The old one is left open.";
    return [
      `${freq}, a brand-new task is created — even if the last one isn't done.`,
      stale,
      `It starts as ${startsAs}.`,
      recurPhrase(form),
    ].join(" ");
  }

  // Completion-based.
  const trigger = statusLabel(form.trigger_status);
  const create =
    form.create_new_issue ?? true
      ? `Multica creates a fresh copy that starts as ${startsAs}`
      : `Multica reopens this same task as ${startsAs}`;
  const clock =
    form.anchor === "due_date"
      ? "The clock counts from its due date."
      : "The clock counts from the day you finish.";
  return [
    `${freq}, when you move this task to ${trigger}, ${create}.`,
    clock,
    recurPhrase(form),
  ].join(" ");
}

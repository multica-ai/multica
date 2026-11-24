import type { ChatWakeup, ChatWakeupTiming } from "@multica/core/types";

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;

// The server's limits for chat wakeups (server/internal/service/chat_wakeup.go).
// The editor applies them before saving so a choice never fails on submit.
export const WAKEUP_MAX_CHECKS = 100;
export const WAKEUP_MAX_WAIT_MS = 7 * DAY_MS;
export const WAKEUP_MAX_REMINDER_MS = 30 * DAY_MS;

export const INTERVAL_PRESETS_S = [600, 900, 1800, 3600] as const;
export const DEADLINE_PRESETS_MS = [4 * HOUR_MS, DAY_MS, 3 * DAY_MS] as const;

/** A deadline or reminder choice: kept as it is, or a time picked in the editor. */
export type TimingChoice = { kind: "keep" } | { kind: "at"; at: number };

export const KEEP: TimingChoice = { kind: "keep" };

/** A one-click time: a distance from now ("in 4 hours") or a clock time. */
export interface TimingPreset {
  at: number;
  inMs?: number;
}

export function deadlinePresets(now: number): TimingPreset[] {
  return DEADLINE_PRESETS_MS.map((inMs) => ({ at: now + inMs, inMs }));
}

/**
 * One-click reminder times: an hour and three hours out, this evening while it
 * is still afternoon, and tomorrow morning.
 */
export function reminderPresets(now: number): TimingPreset[] {
  const today = new Date(now);
  const evening = new Date(today.getFullYear(), today.getMonth(), today.getDate(), 18).getTime();
  const morning = new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1, 9).getTime();
  const presets: TimingPreset[] = [
    { at: now + HOUR_MS, inMs: HOUR_MS },
    { at: now + 3 * HOUR_MS, inMs: 3 * HOUR_MS },
  ];
  if (evening - now > 3 * HOUR_MS) presets.push({ at: evening });
  presets.push({ at: morning });
  return presets;
}

export interface CheckTiming {
  intervalSeconds: number;
  deadline: number;
  /** The deadline was moved earlier to stay within the check limit. */
  limited: boolean;
}

/**
 * What a check would run as with the editor's choices. A schedule that needs
 * more than WAKEUP_MAX_CHECKS checks ends early instead, at the last deadline
 * the limit allows, the same count the server applies.
 */
export function checkTiming(
  wakeup: Pick<ChatWakeup, "interval_seconds" | "expires_at">,
  interval: number | null,
  deadline: TimingChoice,
  now: number,
): CheckTiming {
  const intervalSeconds = interval ?? wakeup.interval_seconds ?? INTERVAL_PRESETS_S[0];
  const chosen = deadline.kind === "at" ? deadline.at : Date.parse(wakeup.expires_at);
  const limit = now + WAKEUP_MAX_CHECKS * intervalSeconds * 1000;
  return chosen > limit
    ? { intervalSeconds, deadline: limit, limited: true }
    : { intervalSeconds, deadline: chosen, limited: false };
}

/**
 * The PATCH body for the editor's choices, or null when nothing changed. Only
 * what the member changed is sent; the server keeps the rest.
 */
export function timingChange(
  wakeup: Pick<ChatWakeup, "kind" | "interval_seconds" | "expires_at">,
  choice: { interval: number | null; deadline: TimingChoice; reminder: TimingChoice },
  now: number,
): ChatWakeupTiming | null {
  switch (wakeup.kind) {
    case "at":
      return choice.reminder.kind === "at" ? { at: new Date(choice.reminder.at).toISOString() } : null;
    case "condition":
      return choice.deadline.kind === "at" ? { expires_at: new Date(choice.deadline.at).toISOString() } : null;
    case "check": {
      const interval = choice.interval !== null && choice.interval !== wakeup.interval_seconds ? choice.interval : null;
      const plan = checkTiming(wakeup, interval, choice.deadline, now);
      const change: ChatWakeupTiming = {};
      if (interval !== null) change.interval_seconds = interval;
      if (choice.deadline.kind === "at" || plan.limited) change.expires_at = new Date(plan.deadline).toISOString();
      return Object.keys(change).length > 0 ? change : null;
    }
  }
}

import type { TimePattern } from "./model";
import { pad2, timeParts } from "./model";

export type EveryPattern = Extract<TimePattern, { kind: "every" }>;
export type ScheduleWindow = { from: string; to: string };

// The window is a dimension of the schedule, not a mode of the editor: all day is
// the window that spans the day, and the model spells that one way — `window:
// null`. Every window the editor commits goes through here, so the two never drift
// into two representations of the same schedule.
//
// The bounds are read as hours. A minute-step window is hour-granular (its ends
// read :00 and :59), and an hours-step window carries the firing minute at both
// ends — neither minute has a say in whether the window spans the day.
export function isFullDay(window: ScheduleWindow): boolean {
  return timeParts(window.from).hour === 0 && timeParts(window.to).hour === 23;
}

/** The window as the controls show it. All day has no window of its own, and the
 *  fields still have to render something: the bounds it stands for. */
export function displayWindow(time: EveryPattern): ScheduleWindow {
  if (time.window !== null) return time.window;
  return time.unit === "hours"
    ? { from: `00:${pad2(time.minute)}`, to: `23:${pad2(time.minute)}` }
    : { from: "00:00", to: "23:59" };
}

// An "every N hours, all day" pattern has nowhere to keep an hour, so the last
// hour:minute the user actually expressed is carried outside the model. Without
// it, at 14:30 → interval → at silently comes back as 09:30.
//
// The hour comes from the window, the minute from `minute` — never from the
// window's own text. A minute-step window is hour-granular, so its bounds read
// :00; taking the minute from there would drop the 30 of a 14:30 the user typed
// before switching, which the model still holds.
//
// All day has no hour to give: the 00:00 its fields show is the absence of a
// bound, not a time the user picked, and carrying it over would rewrite their
// 14:30 to 00:30 on the way back to a fixed time.
export function timeAnchorOf(time: TimePattern): string | null {
  if (time.kind === "at") return time.time;
  if (time.window === null) return null;
  return `${pad2(timeParts(time.window.from).hour)}:${pad2(time.minute)}`;
}

export function defaultAtTime(prev: TimePattern, anchor: string): string {
  return (
    timeAnchorOf(prev) ??
    `${pad2(timeParts(anchor).hour)}:${pad2(prev.kind === "every" ? prev.minute : 0)}`
  );
}

// Switching to a fixed time can only carry one HH:MM, so the step, the unit and
// the window would be gone on the way back — "every 3h, 9:00–21:00" would return
// as "every hour, all day". They are remembered outside the model and restored,
// rebased on whatever time the user is now switching away from.
export function defaultEveryTime(
  prev: TimePattern,
  anchor: EveryPattern | null,
): EveryPattern {
  if (prev.kind === "every") return prev;
  const { hour, minute } = timeParts(prev.time);
  const base: EveryPattern = anchor ?? {
    kind: "every",
    interval: 1,
    unit: "hours",
    window: null,
    minute,
  };
  const time: EveryPattern = { ...base, minute };
  if (time.window === null) return time;
  // The window restarts at the time the user is switching away from — but only
  // where that still leaves a window. Rebasing "09:00–15:00" onto a 22:00 fixed
  // time would drag the end up to 22 and hand back the single hour 22:00–22:00,
  // destroying the very window this anchor exists to keep.
  // Strictly earlier: a start rebased onto the end hour itself leaves the single
  // hour 15:00–15:00, which is the same collapse by another name.
  const rebased =
    hour < timeParts(time.window.to).hour
      ? { from: `${pad2(hour)}:${pad2(minute)}`, to: time.window.to }
      : time.window;
  // Rebasing a window that ends at 23 onto a midnight fixed time spans the whole
  // day, and a window that spans the day is not one — the model holds that as all
  // day, and nothing else may hand back a second form of it.
  const window = clampWindow(rebased, time.unit, minute);
  return { ...time, window: isFullDay(window) ? null : window };
}

// Keep windows canonical: `minute` — not the window's own current text — is the
// single source of truth for the firing minute, so a hours → minutes → hours
// unit round-trip restores it instead of leaving the zeroed value behind. Minute
// intervals are hour-granular, and `to` is never earlier than `from`.
export function clampWindow(
  window: ScheduleWindow,
  unit: EveryPattern["unit"],
  minute: number,
): ScheduleWindow {
  const from = timeParts(window.from);
  const to = timeParts(window.to);
  const edgeMinute = unit === "hours" ? minute : 0;
  const endMinute = unit === "hours" ? minute : 59;
  const endHour = Math.max(to.hour, from.hour);
  return {
    from: `${pad2(from.hour)}:${pad2(edgeMinute)}`,
    to: `${pad2(endHour)}:${pad2(endMinute)}`,
  };
}

export function toggleDay(days: number[], day: number): number[] {
  if (days.includes(day)) {
    if (days.length === 1) return days;
    return days.filter((d) => d !== day);
  }
  return [...days, day].toSorted((a, b) => a - b);
}

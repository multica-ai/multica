import { getDefaultScheduleConfig, pad2, timeAnchorOf, type ScheduleConfig } from "@multica/core/autopilots/schedule";
import type { Strings } from "../strings";

const WEEKDAYS = [1, 2, 3, 4, 5];

export type ScheduleRepeat = "daily" | "weekdays" | "weekly" | "monthly" | "hourly";

export const SCHEDULE_REPEATS: readonly ScheduleRepeat[] = ["daily", "weekdays", "weekly", "monthly", "hourly"];

/** The device's IANA time zone, which new schedules run in. */
export function deviceTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

export function newSchedule(): ScheduleConfig {
  return getDefaultScheduleConfig(deviceTimezone());
}

const isWeekdays = (days: readonly number[]) => days.length === WEEKDAYS.length && WEEKDAYS.every((day) => days.includes(day));

/** Which of the phone's repeat choices a schedule is, or null for a cron the phone can't edit. */
export function scheduleRepeat(config: ScheduleConfig): ScheduleRepeat | null {
  if (config.raw !== null) return null;
  if (config.time.kind === "every") return config.time.unit === "hours" && config.days.kind === "every" ? "hourly" : null;
  if (config.days.kind === "every") return "daily";
  if (config.days.kind === "monthly") return "monthly";
  return isWeekdays(config.days.daysOfWeek) ? "weekdays" : "weekly";
}

/** Switch a schedule to another repeat, keeping its time of day where it has one. */
export function withRepeat(config: ScheduleConfig, repeat: ScheduleRepeat): ScheduleConfig {
  const at = { kind: "at" as const, time: timeAnchorOf(config.time) ?? "09:00" };
  switch (repeat) {
    case "daily":
      return { ...config, raw: null, time: at, days: { kind: "every" } };
    case "weekdays":
      return { ...config, raw: null, time: at, days: { kind: "weekly", daysOfWeek: WEEKDAYS } };
    case "weekly":
      return { ...config, raw: null, time: at, days: { kind: "weekly", daysOfWeek: config.days.kind === "weekly" && !isWeekdays(config.days.daysOfWeek) ? config.days.daysOfWeek : [1] } };
    case "monthly":
      return { ...config, raw: null, time: at, days: { kind: "monthly", dayOfMonth: config.days.kind === "monthly" ? config.days.dayOfMonth : 1 } };
    case "hourly":
      return { ...config, raw: null, time: { kind: "every", interval: 2, unit: "hours", window: null, minute: 0 }, days: { kind: "every" } };
  }
}

/** "Every day at 09:00", "工作日 09:00", "Custom schedule · 0 9 * * 1". */
export function describeSchedule(config: ScheduleConfig, strings: Strings): string {
  const d = strings.schedule.describe;
  if (config.raw !== null) return `${strings.schedule.custom} · ${config.raw}`;
  const time =
    config.time.kind === "at"
      ? d.at(config.time.time)
      : config.time.unit === "hours"
        ? d.everyHours(config.time.interval, pad2(config.time.minute))
        : d.everyMinutes(config.time.interval);
  const window = config.time.kind === "every" && config.time.window ? ` (${d.window(config.time.window.from, config.time.window.to)})` : "";
  const days =
    config.days.kind === "every"
      ? config.time.kind === "every"
        ? null
        : d.everyDay
      : config.days.kind === "monthly"
        ? d.monthly(config.days.dayOfMonth)
        : isWeekdays(config.days.daysOfWeek)
          ? d.weekdays
          : d.days(config.days.daysOfWeek.map((day) => strings.schedule.dayNames[day]).join(d.join));
  const text = [days, time].filter(Boolean).join(" ") + window;
  return text.charAt(0).toUpperCase() + text.slice(1);
}

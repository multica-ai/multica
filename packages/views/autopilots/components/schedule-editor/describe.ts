import { useCallback } from "react";
import { useT } from "../../../i18n";
import type { ScheduleConfig } from "./model";
import { consecutiveRuns, DAY_KEYS, pad2 } from "./model";

type AutopilotsT = ReturnType<typeof useT<"autopilots">>["t"];

/** Collapse runs of 3+ consecutive days into a range ("Mon–Fri"), list the rest.
 *  A run of two stays a list: "Mon–Tue" is no shorter to read than "Mon, Tue". */
function formatDayList(t: AutopilotsT, days: number[]): string {
  const name = (d: number) => t(($) => $.schedule_editor.describe.days_long[DAY_KEYS[d]!]);
  const parts: string[] = [];
  for (const [lo, hi] of consecutiveRuns(days)) {
    if (hi - lo >= 2) {
      parts.push(
        t(($) => $.schedule_editor.describe.days_range, { from: name(lo), to: name(hi) }),
      );
    } else {
      for (let d = lo; d <= hi; d++) parts.push(name(d));
    }
  }
  return parts.join(t(($) => $.schedule_editor.describe.days_join));
}

/** Plain-language readback of a structured schedule. Returns null in
 *  advanced-only mode, where there is no structured model to describe. */
export function describeSchedule(t: AutopilotsT, config: ScheduleConfig): string | null {
  if (config.raw !== null) return null;

  // Clause order follows the editor's own fields, top to bottom: time (step and
  // its window are one dimension, so they stay adjacent), then days.
  const clauses: string[] = [];
  const { time } = config;
  if (time.kind === "at") {
    clauses.push(t(($) => $.schedule_editor.describe.time_at, { time: time.time }));
  } else if (time.unit === "hours") {
    if (time.window === null) {
      const minute = pad2(time.minute);
      clauses.push(
        time.interval === 1
          ? t(($) => $.schedule_editor.describe.time_every_hour, { minute })
          : t(($) => $.schedule_editor.describe.time_every_hours, {
              interval: time.interval,
              minute,
            }),
      );
    } else {
      clauses.push(
        time.interval === 1
          ? t(($) => $.schedule_editor.describe.time_every_hour_window)
          : t(($) => $.schedule_editor.describe.time_every_hours_window, {
              interval: time.interval,
            }),
      );
      clauses.push(
        t(($) => $.schedule_editor.describe.window, {
          from: time.window.from,
          to: time.window.to,
        }),
      );
    }
  } else {
    clauses.push(
      time.interval === 1
        ? t(($) => $.schedule_editor.describe.time_every_minute)
        : t(($) => $.schedule_editor.describe.time_every_minutes, { interval: time.interval }),
    );
    if (time.window !== null) {
      clauses.push(
        t(($) => $.schedule_editor.describe.window, {
          from: time.window.from,
          to: time.window.to,
        }),
      );
    }
  }

  switch (config.days.kind) {
    case "every":
      clauses.push(t(($) => $.schedule_editor.describe.days_every));
      break;
    case "weekly":
      clauses.push(formatDayList(t, config.days.daysOfWeek));
      break;
    case "monthly":
      clauses.push(
        t(($) => $.schedule_editor.describe.days_monthly, { day: config.days.dayOfMonth }),
      );
      break;
  }

  return clauses.join(" · ");
}

export function useDescribeSchedule(): (config: ScheduleConfig) => string | null {
  const { t } = useT("autopilots");
  return useCallback((config: ScheduleConfig) => describeSchedule(t, config), [t]);
}

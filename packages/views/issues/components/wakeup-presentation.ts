"use client";

import type {
  AgentTask,
  IssueWakeup,
  WakeupPreview,
} from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { parseCron } from "../../autopilots/components/schedule-editor/cron-mapping";
import { useDescribeSchedule } from "../../autopilots/components/schedule-editor/describe";

export function wakeupState(
  w: Omit<IssueWakeup, "instruction">,
  closed = false,
  now = Date.now(),
) {
  if (closed) return "issue_closed";
  if (w.enabled) return w.kind === "event" ? "waiting" : "scheduled";
  if (w.disabled_at) return "disabled";
  if (w.last_task_id) return "triggered";
  if (w.kind === "at" && w.next_fire_at && Date.parse(w.next_fire_at) <= now)
    return "expired";
  return "inactive";
}

export function isActiveWakeupRun(status?: string | null) {
  return (
    !!status &&
    [
      "queued",
      "deferred",
      "dispatched",
      "running",
      "waiting_local_directory",
    ].includes(status)
  );
}

export function wakeupRun(wakeup: IssueWakeup, tasks: readonly AgentTask[]) {
  return (
    tasks.find(
      (task) => task.wakeup_id === wakeup.id && isActiveWakeupRun(task.status),
    ) ?? tasks.find((task) => task.id === wakeup.last_task_id)
  );
}

export function isCurrentWakeup(wakeup: IssueWakeup, task?: AgentTask) {
  return (
    wakeup.enabled || isActiveWakeupRun(task?.status ?? wakeup.last_task_status)
  );
}

export function formatWakeupTime(
  value: string,
  locale: string,
  timezone = "UTC",
  now = new Date(),
) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  const day = new Intl.DateTimeFormat(locale, {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
  const sameDay = day.format(date) === day.format(now);
  const year = new Intl.DateTimeFormat(locale, {
    timeZone: timezone,
    year: "numeric",
  });
  return new Intl.DateTimeFormat(locale, {
    timeZone: timezone,
    hour: "2-digit",
    minute: "2-digit",
    ...(sameDay ? {} : { month: "short" as const, day: "numeric" as const }),
    ...(year.format(date) === year.format(now)
      ? {}
      : { year: "numeric" as const }),
  }).format(date);
}

export function useWakeupText() {
  const { t } = useT("issues");
  const locale = useLocale();
  const describe = useDescribeSchedule();
  const eventLabels = (agent: string): Record<string, string> => ({
    "task.queued": t(($) => $.wakeups.conditions.run_queued, { agent }),
    "task.dispatched": t(($) => $.wakeups.conditions.run_dispatched, { agent }),
    "task.started": t(($) => $.wakeups.conditions.run_started, { agent }),
    "task.deferred": t(($) => $.wakeups.conditions.run_deferred, { agent }),
    "task.waiting_local_directory": t(
      ($) => $.wakeups.conditions.run_waiting_local_directory,
      { agent },
    ),
    "issue.updated": t(($) => $.wakeups.conditions.issue_updated, { agent }),
    "issue.assignee_changed": t(($) => $.wakeups.conditions.assignee_changed, {
      agent,
    }),
    "issue.parent_changed": t(($) => $.wakeups.conditions.parent_changed, {
      agent,
    }),
    "issue.project_changed": t(($) => $.wakeups.conditions.project_changed, {
      agent,
    }),
    "issue.labels_changed": t(($) => $.wakeups.conditions.labels_changed, {
      agent,
    }),
    "issue.properties_changed": t(
      ($) => $.wakeups.conditions.properties_changed,
      { agent },
    ),
    "issue.metadata_changed": t(($) => $.wakeups.conditions.metadata_changed, {
      agent,
    }),
    "comment.updated": t(($) => $.wakeups.conditions.comment_updated, {
      agent,
    }),
    "comment.deleted": t(($) => $.wakeups.conditions.comment_deleted, {
      agent,
    }),
    "comment.resolved": t(($) => $.wakeups.conditions.comment_resolved, {
      agent,
    }),
    "comment.unresolved": t(($) => $.wakeups.conditions.comment_unresolved, {
      agent,
    }),
    "reaction.added": t(($) => $.wakeups.conditions.reaction_added, { agent }),
    "reaction.removed": t(($) => $.wakeups.conditions.reaction_removed, {
      agent,
    }),
    "attachment.attached": t(($) => $.wakeups.conditions.attachment_attached, {
      agent,
    }),
    "attachment.detached": t(($) => $.wakeups.conditions.attachment_detached, {
      agent,
    }),
    "task.completed": t(($) => $.wakeups.conditions.run_completed, { agent }),
    "task.failed": t(($) => $.wakeups.conditions.run_failed, { agent }),
    "task.cancelled": t(($) => $.wakeups.conditions.run_cancelled, { agent }),
    "comment.created": t(($) => $.wakeups.conditions.comment_created, {
      agent,
    }),
    "issue.status_changed": t(($) => $.wakeups.conditions.status_changed, {
      agent,
    }),
  });

  const eventName = (
    event: string,
    agent = t(($) => $.wakeups.agent_subject),
  ) =>
    eventLabels(agent)[event] ?? t(($) => $.wakeups.unknown_event, { event });
  const frequency = (w: WakeupPreview) =>
    w.mode === "once"
      ? t(($) => $.wakeups.once)
      : t(($) => $.wakeups.continuous);
  const schedule = (w: WakeupPreview) => {
    if (w.kind === "every") {
      const seconds = w.interval_seconds ?? 0;
      return seconds === 3600
        ? t(($) => $.wakeups.hourly)
        : seconds % 3600 === 0
          ? t(($) => $.wakeups.every_hours, { hours: seconds / 3600 })
          : seconds % 60 === 0
            ? t(($) => $.wakeups.every, { minutes: seconds / 60 })
            : t(($) => $.wakeups.every_seconds, { seconds });
    }
    if (w.kind === "cron")
      return (
        describe(parseCron(w.cron_expression ?? "", w.timezone)) ??
        `${w.cron_expression} · ${w.timezone}`
      );
    return frequency(w);
  };
  const time = (value: string, timezone: string) => {
    if (!Number.isFinite(Date.parse(value))) return value;
    const day = new Intl.DateTimeFormat(locale, {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    });
    const formatted = formatWakeupTime(value, locale, timezone);
    return day.format(new Date(value)) === day.format(new Date())
      ? t(($) => $.wakeups.today_time, { time: formatted })
      : formatted;
  };
  const actorName = (w: WakeupPreview) => w.filter_actor_name ||
    (w.filter_actor_type === "member" ? t(($) => $.wakeups.selected_member) : t(($) => $.wakeups.selected_agent));
  const eventCondition = (event: string, w: WakeupPreview) => {
    const label = eventName(event, w.filter_agent_name || undefined);
    const actor = w.filter_actor_type ? actorName(w) : w.filter_agent_name;
    return actor && !event.startsWith("task.")
      ? t(($) => $.wakeups.by_actor, { condition: label, agent: actor })
      : label;
  };
  const trigger = (w: WakeupPreview) => {
    if (w.kind === "every" || w.kind === "cron") return schedule(w);
    if (w.kind === "at")
      return w.next_fire_at
        ? t(($) => $.wakeups.at_time, {
            time: time(w.next_fire_at, w.timezone),
          })
        : t(($) => $.wakeups.scheduled_time);
    const event = w.event_types[0] ?? "";
    let label = eventCondition(event, w);
    if (w.filter_task_id)
      label += ` · ${t(($) => $.wakeups.specific_run, { id: w.filter_task_id.slice(0, 8) })}`;
    return `${label}${w.event_types.length > 1 ? ` +${w.event_types.length - 1}` : ""}`;
  };
  const runLabels: Record<string, string> = {
    queued: t(($) => $.wakeups.run_states.queued),
    deferred: t(($) => $.wakeups.run_states.deferred),
    dispatched: t(($) => $.wakeups.run_states.dispatched),
    running: t(($) => $.wakeups.run_states.running),
    waiting_local_directory: t(
      ($) => $.wakeups.run_states.waiting_local_directory,
    ),
    completed: t(($) => $.wakeups.run_states.completed),
    failed: t(($) => $.wakeups.run_states.failed),
    cancelled: t(($) => $.wakeups.run_states.cancelled),
  };
  const runState = (status?: string | null) =>
    status ? (runLabels[status] ?? status) : t(($) => $.wakeups.no_run);
  const state = (w: Omit<IssueWakeup, "instruction">, closed = false) => {
    const key = wakeupState(w, closed);
    return key === "scheduled" && w.next_fire_at
      ? t(($) => $.wakeups.next_at, { time: time(w.next_fire_at, w.timezone) })
      : t(($) => $.wakeups.rule_states[key]);
  };
  const error = (err: unknown, fallback: string) => {
    const status =
      err && typeof err === "object" && "status" in err
        ? err.status
        : undefined;
    return status === 403
      ? t(($) => $.wakeups.permission_error)
      : status === 409
        ? t(($) => $.wakeups.conflict_error)
        : fallback;
  };
  return { eventName, eventCondition, actorName, trigger, schedule, frequency, runState, state, error };
}

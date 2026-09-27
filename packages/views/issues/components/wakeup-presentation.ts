"use client";

import type {
  AgentTask,
  IssueWakeup,
  WakeupCondition,
  WakeupPreview,
} from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { parseCron } from "../../autopilots/components/schedule-editor/cron-mapping";
import { useDescribeSchedule } from "../../autopilots/components/schedule-editor/describe";
import { useConditionNames } from "./wakeup-condition-names";

export function wakeupState(
  w: Omit<IssueWakeup, "instruction">,
  closed = false,
  now = Date.now(),
) {
  if (closed) return "issue_closed";
  if (w.enabled) return w.kind === "event" ? "waiting" : "scheduled";
  if (w.paused_reason) return "paused";
  if (w.timed_out_at) return "timed_out";
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
  const names = useConditionNames();
  // Fire times are absolute instants: show them in the viewer's timezone.
  // A wakeup's stored timezone only defines cron semantics.
  const viewTZ = useViewingTimezone();
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
  const time = (value: string) => {
    if (!Number.isFinite(Date.parse(value))) return value;
    const day = new Intl.DateTimeFormat(locale, {
      timeZone: viewTZ,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    });
    const formatted = formatWakeupTime(value, locale, viewTZ);
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
  const propertyValue = (c: Extract<WakeupCondition, { field: "property" }>) => {
    const property = names.property(c.property_id);
    const raw = c.value;
    if (typeof raw === "boolean") return raw ? t(($) => $.wakeups.cond.checked) : t(($) => $.wakeups.cond.unchecked);
    const option = property?.config.options?.find((o) => o.id === raw);
    return option?.name ?? String(raw);
  };
  // Every sentence form of a condition: "when it holds" for rules and
  // "waiting for it" for cards and the header.
  const conditionParts = (c: WakeupCondition) => {
    switch (c.type) {
      case "issue_field":
        switch (c.field) {
          case "status":
            return { key: "status" as const, values: { status: names.status(c.value) } };
          case "assignee":
            return { key: "assignee" as const, values: { name: names.actor(c.assignee_type, c.assignee_id) } };
          case "label":
            return { key: "label" as const, values: { label: names.label(c.label_id) ?? t(($) => $.wakeups.cond.unknown_label) } };
          case "property":
            return {
              key: "property" as const,
              values: {
                property: names.property(c.property_id)?.name ?? t(($) => $.wakeups.cond.unknown_property),
                value: propertyValue(c),
              },
            };
        }
        break;
      case "children_done":
        return c.stage ? { key: "children_stage" as const, values: { stage: c.stage } } : { key: "children_all" as const, values: {} };
      case "pull_request":
        return { key: c.event === "merged" ? ("pr_merged" as const) : ("pr_checks" as const), values: {} };
      case "other_issue":
        return {
          key: c.state === "ended" ? ("other_ended" as const) : c.state === "in_review" ? ("other_in_review" as const) : ("other_done" as const),
          values: { issue: c.identifier || t(($) => $.wakeups.cond.another_issue) },
        };
    }
    return { key: "children_all" as const, values: {} };
  };
  const condition = (c: WakeupCondition) => {
    const { key, values } = conditionParts(c);
    return String(t(($) => $.wakeups.cond[key], values as Record<string, string | number>));
  };
  const conditionWait = (c: WakeupCondition) => {
    const { key, values } = conditionParts(c);
    return String(t(($) => $.wakeups.wait[key], values as Record<string, string | number>));
  };
  const trigger = (w: WakeupPreview) => {
    if (w.condition) return condition(w.condition);
    if (w.kind === "every" || w.kind === "cron") return schedule(w);
    if (w.kind === "at")
      return w.next_fire_at
        ? t(($) => $.wakeups.at_time, {
            time: time(w.next_fire_at),
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
      ? t(($) => $.wakeups.next_at, { time: time(w.next_fire_at) })
      : t(($) => $.wakeups.rule_states[key]);
  };
  // Time left before an unanswered wait ends; null once it has passed.
  const remaining = (value: string, now = Date.now()) => {
    const ms = Date.parse(value) - now;
    if (!Number.isFinite(ms) || ms <= 0) return null;
    const minute = 60_000, hour = 60 * minute, day = 24 * hour;
    const days = Math.floor(ms / day);
    const hours = Math.floor((ms % day) / hour);
    if (days >= 3) return t(($) => $.wakeups.remaining_days, { days });
    if (days >= 1) return t(($) => $.wakeups.remaining_days_hours, { days, hours });
    if (hours >= 1) return t(($) => $.wakeups.remaining_hours, { hours });
    return t(($) => $.wakeups.remaining_minutes, { minutes: Math.max(1, Math.ceil(ms / minute)) });
  };
  const date = (value: string) =>
    new Intl.DateTimeFormat(locale, { timeZone: viewTZ, month: "short", day: "numeric" }).format(new Date(value));
  // A relative wait reads as time left; a recurring schedule's end date reads
  // as "until <date>".
  const ending = (w: Omit<IssueWakeup, "instruction">) => {
    if (!w.enabled || !w.expires_at) return null;
    return w.kind === "event" || w.expiry_seconds
      ? remaining(w.expires_at)
      : t(($) => $.wakeups.until_date, { date: date(w.expires_at) });
  };
  // The row's second line: who is woken, how often, and when the rule ends.
  const summary = (w: Omit<IssueWakeup, "instruction">, closed = false) =>
    [
      t(($) => $.wakeups.wake_agent, { agent: w.agent_name }),
      w.kind === "event" || w.kind === "at" ? frequency(w) : state(w, closed),
      !w.enabled && (w.kind === "event" || w.kind === "at") ? state(w, closed) : null,
      closed ? null : ending(w),
    ]
      .filter(Boolean)
      .join(" · ");
  const source = (w: Omit<IssueWakeup, "instruction">) => {
    const name = w.created_by_name ?? "";
    return w.created_by_agent
      ? t(($) => $.wakeups.source_agent, {
          agent: w.source_agent_name || t(($) => $.wakeups.source_agent_unknown),
          name,
        })
      : t(($) => $.wakeups.source_member, { name });
  };
  const expiry = (w: Omit<IssueWakeup, "instruction">) => {
    if (!w.expires_at) return null;
    const at = time(w.expires_at);
    if (w.timed_out_at) return t(($) => $.wakeups.expiry_timed_out, { time: time(w.timed_out_at) });
    const then =
      w.on_timeout === "wake"
        ? t(($) => $.wakeups.timeout_then_wake, { agent: w.agent_name })
        : t(($) => $.wakeups.timeout_then_end);
    return `${t(($) => $.wakeups.expiry_at, { time: at })} · ${then}`;
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
  // What an issue is waiting for, in a few words, for cards and the header.
  const waiting = (w: WakeupPreview) => {
    if (w.condition) return conditionWait(w.condition);
    if (w.kind !== "event") {
      return w.next_fire_at
        ? t(($) => $.wakeups.wait.time, { time: time(w.next_fire_at), agent: w.agent_name })
        : schedule(w);
    }
    const event = w.event_types[0] ?? "";
    if (w.event_types.length === 1 && event === "comment.created") {
      return w.filter_actor_type
        ? t(($) => $.wakeups.wait.reply_from, { name: actorName(w) })
        : t(($) => $.wakeups.wait.reply_any);
    }
    if (w.event_types.every((e) => ["task.completed", "task.failed", "task.cancelled"].includes(e))) {
      return w.filter_agent_name
        ? t(($) => $.wakeups.wait.run_end, { agent: w.filter_agent_name })
        : t(($) => $.wakeups.wait.run_end_any);
    }
    return t(($) => $.wakeups.waiting_event);
  };
  // The header reads "<agent> is waiting…"; time rules already name the agent.
  const headline = (w: WakeupPreview) =>
    w.kind !== "event" && w.next_fire_at
      ? waiting(w)
      : t(($) => $.wakeups.wait.header, { agent: w.agent_name, what: waiting(w) });
  const paused = (w: Pick<IssueWakeup, "paused_reason" | "max_fires" | "fire_count">) =>
    w.paused_reason === "max_fires"
      ? t(($) => $.wakeups.paused.max_fires, { count: w.max_fires ?? w.fire_count ?? 0 })
      : w.paused_reason === "loop"
        ? t(($) => $.wakeups.paused.loop)
        : w.paused_reason === "rate"
          ? t(($) => $.wakeups.paused.rate)
          : null;
  // The bare reason, for sentences that already say the rule was paused.
  const pausedReason = (w: Pick<IssueWakeup, "paused_reason" | "max_fires" | "fire_count">) =>
    w.paused_reason === "max_fires"
      ? t(($) => $.wakeups.paused_reason.max_fires, { count: w.max_fires ?? w.fire_count ?? 0 })
      : w.paused_reason === "loop"
        ? t(($) => $.wakeups.paused_reason.loop)
        : w.paused_reason === "rate"
          ? t(($) => $.wakeups.paused_reason.rate)
          : null;
  const fires = (w: Pick<IssueWakeup, "max_fires" | "fire_count" | "mode">) => {
    const count = w.fire_count ?? 0;
    if (w.mode !== "continuous" || (!count && !w.max_fires)) return null;
    return w.max_fires
      ? t(($) => $.wakeups.detail.fires_max, { count, max: w.max_fires })
      : t(($) => $.wakeups.detail.fires, { count });
  };
  return { eventName, eventCondition, actorName, trigger, schedule, frequency, runState, state, error, remaining, ending, summary, source, expiry, condition, conditionWait, waiting, headline, paused, pausedReason, fires };
}

import type { Autopilot, AutopilotRun, AutopilotTrigger } from "@multica/core/types";
import type { AvatarActor } from "../components/Avatar";
import type { IconName } from "../icons/Icon";
import type { Strings } from "../strings";
import type { Palette } from "../theme";
import { formatMoment } from "./dates";

/** Squads are resolved by id; agents through the workspace's actor names. */
export type AssigneeResolver = (type: string, id: string) => AvatarActor;

export interface AutopilotRowModel {
  id: string;
  title: string;
  paused: boolean;
  assignee: AvatarActor;
  /** "Schedule", "Webhook · Schedule" … what can fire it. */
  triggers: string;
  triggerIcon: IconName;
  /** Formatted, e.g. "Tomorrow 08:30". */
  next: string | null;
  lastFailed: boolean;
}

export function triggerIcon(kind: string): IconName {
  if (kind === "schedule") return "CalendarClock";
  if (kind === "webhook") return "Webhook";
  return "Link2";
}

export function autopilotRowModel(autopilot: Autopilot, resolveAssignee: AssigneeResolver, strings: Strings): AutopilotRowModel {
  const kinds = autopilot.trigger_kinds ?? [];
  return {
    id: autopilot.id,
    title: autopilot.title,
    paused: autopilot.status !== "active",
    assignee: resolveAssignee(autopilot.assignee_type, autopilot.assignee_id),
    triggers: kinds.map((kind) => strings.autopilot.kind[kind] ?? kind).join(" · ") || strings.autopilot.source.manual!,
    triggerIcon: triggerIcon(kinds[0] ?? "manual"),
    next: autopilot.status === "active" && autopilot.next_run_at ? formatMoment(autopilot.next_run_at, strings) : null,
    lastFailed: autopilot.last_run_status === "failed",
  };
}

/** Active first, then paused; archived autopilots are not listed. */
export function autopilotSections(autopilots: readonly Autopilot[], resolveAssignee: AssigneeResolver, strings: Strings) {
  const live = autopilots.filter((autopilot) => autopilot.status !== "archived");
  const byRecent = (a: Autopilot, b: Autopilot) => b.updated_at.localeCompare(a.updated_at);
  return [
    { key: "active", title: strings.autopilot.active, rows: live.filter((a) => a.status === "active").sort(byRecent) },
    { key: "paused", title: strings.autopilot.paused, rows: live.filter((a) => a.status === "paused").sort(byRecent) },
  ]
    .filter((section) => section.rows.length > 0)
    .map((section) => ({ ...section, rows: section.rows.map((autopilot) => autopilotRowModel(autopilot, resolveAssignee, strings)) }));
}

export interface RunLook {
  icon: IconName;
  color: string;
}

export function runLook(status: string, palette: Palette): RunLook {
  switch (status) {
    case "running":
      return { icon: "Loader2", color: palette.warning };
    case "issue_created":
    case "completed":
      return { icon: "CheckCircle2", color: palette.success };
    case "failed":
      return { icon: "AlertCircle", color: palette.destructive };
    case "skipped":
      return { icon: "CircleSlash", color: palette.label3 };
    default:
      return { icon: "Clock", color: palette.label3 };
  }
}

export interface AutopilotRunModel {
  id: string;
  status: string;
  /** "Today 10:00 · Scheduled" */
  when: string;
  detail: string;
  issueId: string | null;
}

export function runModel(run: AutopilotRun, strings: Strings): AutopilotRunModel {
  const status = strings.autopilot.runStatus[run.status] ?? run.status;
  return {
    id: run.id,
    status: run.status,
    when: `${formatMoment(run.triggered_at, strings)} · ${strings.autopilot.source[run.source] ?? run.source}`,
    detail: run.failure_reason && (run.status === "failed" || run.status === "skipped") ? `${status} · ${run.failure_reason}` : status,
    issueId: run.issue_id,
  };
}

export interface TriggerModel {
  id: string;
  kind: string;
  icon: IconName;
  label: string;
  detail: string;
  enabled: boolean;
}

export function triggerModel(trigger: AutopilotTrigger, describe: (trigger: AutopilotTrigger) => string, strings: Strings): TriggerModel {
  const next = trigger.enabled && trigger.next_run_at ? strings.autopilot.next(formatMoment(trigger.next_run_at, strings)) : null;
  return {
    id: trigger.id,
    kind: trigger.kind,
    icon: triggerIcon(trigger.kind),
    label: trigger.label || describe(trigger),
    detail:
      trigger.kind === "schedule"
        ? [trigger.timezone, next].filter(Boolean).join(" · ")
        : trigger.kind === "webhook"
          ? strings.autopilot.webhookDetail
          : strings.autopilot.kind[trigger.kind] ?? trigger.kind,
    enabled: trigger.enabled,
  };
}

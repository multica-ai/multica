import type {
  Autopilot,
  AutopilotAssigneeType,
  AutopilotExecutionMode,
  AutopilotRunStatus,
  AutopilotStatus,
  AutopilotTrigger,
  AutopilotTriggerKind,
  WebhookDeliveryStatus,
  WebhookEventFilter,
  WebhookSignatureStatus,
} from "@multica/core/types";
import type { Agent, Squad } from "@multica/core/types";
import type { Project } from "@multica/core/types/project";

export type TriggerFrequency = "hourly" | "daily" | "weekdays" | "weekly" | "custom";

export const AUTOPILOT_EVENT_FILTER_DOC_URL =
  "https://multica.ai/docs/zh/autopilots#%E4%BA%8B%E4%BB%B6%E8%BF%87%E6%BB%A4";

export type TriggerFormConfig = {
  frequency: TriggerFrequency;
  time: string;
  daysOfWeek: number[];
  cronExpression: string;
  timezone: string;
};

export function getLocalTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "UTC";
  }
}

export function getDefaultTriggerConfig(): TriggerFormConfig {
  return {
    frequency: "daily",
    time: "09:00",
    daysOfWeek: [1],
    cronExpression: "0 9 * * 1-5",
    timezone: getLocalTimezone(),
  };
}

export function toCronExpression(config: TriggerFormConfig): string {
  const [hourPart, minutePart] = config.time.split(":");
  const hour = parseInt(hourPart ?? "9", 10);
  const minute = parseInt(minutePart ?? "0", 10);
  const safeHour = Number.isFinite(hour) ? Math.min(Math.max(hour, 0), 23) : 9;
  const safeMinute = Number.isFinite(minute) ? Math.min(Math.max(minute, 0), 59) : 0;

  switch (config.frequency) {
    case "hourly":
      return `${safeMinute} * * * *`;
    case "daily":
      return `${safeMinute} ${safeHour} * * *`;
    case "weekdays":
      return `${safeMinute} ${safeHour} * * 1-5`;
    case "weekly": {
      const days = Array.from(new Set(config.daysOfWeek))
        .filter((day) => day >= 0 && day <= 6)
        .sort((a, b) => a - b);
      return `${safeMinute} ${safeHour} * * ${days.length > 0 ? days.join(",") : "1"}`;
    }
    case "custom":
      return config.cronExpression.trim();
  }
}

export function parseCronExpression(cron: string, timezone: string | null): TriggerFormConfig {
  const base = {
    ...getDefaultTriggerConfig(),
    timezone: timezone || getLocalTimezone(),
    cronExpression: cron,
  };
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return { ...base, frequency: "custom" };

  const [minutePart, hourPart, dayOfMonth, month, dayOfWeek] = parts;
  if (dayOfMonth !== "*" || month !== "*") return { ...base, frequency: "custom" };

  const minute = parseInt(minutePart ?? "", 10);
  if (!Number.isFinite(minute) || minute < 0 || minute > 59) {
    return { ...base, frequency: "custom" };
  }

  if (hourPart === "*" && dayOfWeek === "*") {
    return {
      ...base,
      frequency: "hourly",
      time: `00:${String(minute).padStart(2, "0")}`,
    };
  }

  const hour = parseInt(hourPart ?? "", 10);
  if (!Number.isFinite(hour) || hour < 0 || hour > 23) {
    return { ...base, frequency: "custom" };
  }

  const time = `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
  if (dayOfWeek === "*") return { ...base, frequency: "daily", time };
  if (dayOfWeek === "1-5") {
    return { ...base, frequency: "weekdays", time, daysOfWeek: [1, 2, 3, 4, 5] };
  }
  const dow = dayOfWeek ?? "";
  if (/^[0-6](,[0-6])*$/.test(dow)) {
    return {
      ...base,
      frequency: "weekly",
      time,
      daysOfWeek: dow.split(",").map((value) => parseInt(value, 10)),
    };
  }
  return { ...base, frequency: "custom" };
}

export function isValidTime(value: string): boolean {
  return /^([01]\d|2[0-3]):[0-5]\d$/.test(value);
}

export function formatDateTime(value: string | null | undefined): string {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatRelativeDate(value: string | null | undefined): string {
  if (!value) return "--";
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return "--";
  const diff = Date.now() - time;
  if (diff < 60_000) return "now";
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

export function formatDuration(start: string | null, end: string | null): string {
  if (!start || !end) return "--";
  const startMs = new Date(start).getTime();
  const endMs = new Date(end).getTime();
  if (Number.isNaN(startMs) || Number.isNaN(endMs) || endMs < startMs) return "--";
  const seconds = Math.round((endMs - startMs) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

export function getActorName(
  type: AutopilotAssigneeType | undefined,
  id: string | undefined,
  agents: Agent[],
  squads: Squad[],
): string {
  if (!id) return "Unassigned";
  if (type === "squad") {
    return squads.find((squad) => squad.id === id)?.name ?? "Unknown squad";
  }
  return agents.find((agent) => agent.id === id)?.name ?? "Unknown agent";
}

export function getProjectTitle(projectId: string | null | undefined, projects: Project[]): string {
  if (!projectId) return "No project";
  return projects.find((project) => project.id === projectId)?.title ?? "Unknown project";
}

export function getPrimaryTrigger(triggers: AutopilotTrigger[]): AutopilotTrigger | null {
  return triggers[0] ?? null;
}

export function describeTrigger(trigger: AutopilotTrigger | null): string {
  if (!trigger) return "No trigger";
  if (trigger.kind === "webhook") {
    return trigger.label ? `Webhook · ${trigger.label}` : "Webhook";
  }
  if (trigger.kind === "api") return "API";
  if (!trigger.cron_expression) return "Schedule";
  const config = parseCronExpression(trigger.cron_expression, trigger.timezone);
  switch (config.frequency) {
    case "hourly":
      return `Hourly · :${config.time.split(":")[1] ?? "00"}`;
    case "daily":
      return `Daily · ${config.time}`;
    case "weekdays":
      return `Weekdays · ${config.time}`;
    case "weekly":
      return `Weekly · ${formatDayList(config.daysOfWeek)} ${config.time}`;
    case "custom":
      return `Custom · ${trigger.cron_expression}`;
  }
}

export function formatDayList(days: number[]): string {
  const labels = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const sorted = Array.from(new Set(days)).sort((a, b) => a - b);
  if (sorted.length === 0) return "Mon";
  return sorted.map((day) => labels[day] ?? String(day)).join(", ");
}

export function parseEventFilters(value: string): WebhookEventFilter[] {
  return value
    .split(/\n+/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [eventPart, actionsPart] = line.split(":");
      const actions = actionsPart
        ?.split(",")
        .map((action) => action.trim())
        .filter(Boolean);
      return {
        event: eventPart?.trim() || line,
        ...(actions && actions.length > 0 ? { actions } : {}),
      };
    });
}

export function stringifyEventFilters(filters: WebhookEventFilter[] | null | undefined): string {
  return (filters ?? [])
    .map((filter) => {
      const actions = filter.actions?.filter(Boolean) ?? [];
      return actions.length > 0 ? `${filter.event}: ${actions.join(", ")}` : filter.event;
    })
    .join("\n");
}

export function statusLabel(status: AutopilotStatus | string): string {
  switch (status) {
    case "active":
      return "Active";
    case "paused":
      return "Paused";
    case "archived":
      return "Archived";
    default:
      return status || "Unknown";
  }
}

export function localizedStatusLabel(t: (key: string) => string, status: AutopilotStatus | string): string {
  switch (status) {
    case "active":
      return t("autopilots.status_active");
    case "paused":
      return t("autopilots.status_paused");
    case "archived":
      return t("autopilots.status_archived");
    default:
      return statusLabel(status);
  }
}

export function executionModeLabel(mode: AutopilotExecutionMode | string): string {
  switch (mode) {
    case "create_issue":
      return "Create issue";
    case "run_only":
      return "Run only";
    default:
      return mode || "Unknown";
  }
}

export function localizedExecutionModeLabel(
  t: (key: string) => string,
  mode: AutopilotExecutionMode | string,
): string {
  switch (mode) {
    case "create_issue":
      return t("autopilots.create_issue");
    case "run_only":
      return t("autopilots.run_only");
    default:
      return executionModeLabel(mode);
  }
}

export function triggerKindLabel(kind: AutopilotTriggerKind | string): string {
  switch (kind) {
    case "schedule":
      return "Schedule";
    case "webhook":
      return "Webhook";
    case "api":
      return "API";
    default:
      return kind || "Unknown";
  }
}

export function localizedTriggerKindLabel(
  t: (key: string) => string,
  kind: AutopilotTriggerKind | string,
): string {
  switch (kind) {
    case "schedule":
      return t("autopilots.schedule");
    case "webhook":
      return t("autopilots.webhook");
    case "api":
      return t("autopilots.api");
    default:
      return triggerKindLabel(kind);
  }
}

export function runStatusLabel(status: AutopilotRunStatus | string): string {
  switch (status) {
    case "issue_created":
      return "Issue created";
    case "running":
      return "Running";
    case "completed":
      return "Completed";
    case "failed":
      return "Failed";
    case "skipped":
      return "Skipped";
    default:
      return status || "Unknown";
  }
}

export function localizedRunStatusLabel(
  t: (key: string) => string,
  status: AutopilotRunStatus | string,
): string {
  switch (status) {
    case "issue_created":
      return t("autopilots.run_issue_created");
    case "running":
      return t("autopilots.run_running");
    case "completed":
      return t("autopilots.run_completed");
    case "failed":
      return t("autopilots.run_failed");
    case "skipped":
      return t("autopilots.run_skipped");
    default:
      return runStatusLabel(status);
  }
}

export function deliveryStatusLabel(status: WebhookDeliveryStatus | string): string {
  switch (status) {
    case "queued":
      return "Queued";
    case "dispatched":
      return "Dispatched";
    case "rejected":
      return "Rejected";
    case "ignored":
      return "Ignored";
    case "failed":
      return "Failed";
    default:
      return status || "Unknown";
  }
}

export function localizedDeliveryStatusLabel(
  t: (key: string) => string,
  status: WebhookDeliveryStatus | string,
): string {
  switch (status) {
    case "queued":
      return t("autopilots.delivery_queued");
    case "dispatched":
      return t("autopilots.delivery_dispatched");
    case "rejected":
      return t("autopilots.delivery_rejected");
    case "ignored":
      return t("autopilots.delivery_ignored");
    case "failed":
      return t("autopilots.delivery_failed");
    default:
      return deliveryStatusLabel(status);
  }
}

export function signatureStatusLabel(status: WebhookSignatureStatus | string): string {
  switch (status) {
    case "not_required":
      return "Not required";
    case "valid":
      return "Valid";
    case "invalid":
      return "Invalid";
    case "missing":
      return "Missing";
    default:
      return status || "Unknown";
  }
}

export function localizedSignatureStatusLabel(
  t: (key: string) => string,
  status: WebhookSignatureStatus | string,
): string {
  switch (status) {
    case "not_required":
      return t("autopilots.signature_not_required");
    case "valid":
      return t("autopilots.signature_valid");
    case "invalid":
      return t("autopilots.signature_invalid");
    case "missing":
      return t("autopilots.signature_missing");
    default:
      return signatureStatusLabel(status);
  }
}

export function summarizeAutopilots(autopilots: Autopilot[]) {
  const active = autopilots.filter((autopilot) => autopilot.status === "active").length;
  const paused = autopilots.filter((autopilot) => autopilot.status === "paused").length;
  const latestRun = autopilots
    .map((autopilot) => autopilot.last_run_at)
    .filter((value): value is string => Boolean(value))
    .sort((a, b) => new Date(b).getTime() - new Date(a).getTime())[0] ?? null;
  return { active, latestRun, paused, total: autopilots.length };
}

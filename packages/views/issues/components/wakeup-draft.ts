import type { IssueWakeupInput, WakeupCondition as PlatformCondition } from "@multica/core/types";
import type { DayPattern, ScheduleConfig } from "../../autopilots/components/schedule-editor/model";
import { cronFields } from "../../autopilots/components/schedule-editor/cron-mapping";

/** The 25 issue-scoped events, in catalog order. */
export const WAKEUP_EVENT_TYPES = [
  "task.queued",
  "task.dispatched",
  "task.started",
  "task.deferred",
  "task.waiting_local_directory",
  "task.completed",
  "task.failed",
  "task.cancelled",
  "issue.updated",
  "issue.status_changed",
  "issue.assignee_changed",
  "issue.parent_changed",
  "issue.project_changed",
  "issue.labels_changed",
  "issue.properties_changed",
  "issue.metadata_changed",
  "comment.created",
  "comment.updated",
  "comment.deleted",
  "comment.resolved",
  "comment.unresolved",
  "reaction.added",
  "reaction.removed",
  "attachment.attached",
  "attachment.detached",
] as const;

const RUN_END_EVENTS = ["task.completed", "task.failed", "task.cancelled"];

export type WakeupCondition =
  | "at"
  | "recurring"
  | "reply"
  | "field"
  | "run_end"
  | "children"
  | "pull_request"
  | "other_issue"
  | "custom";
export type WakeupField = "status" | "assignee" | "label" | "property";
export type WakeupAtPreset = "10m" | "1h" | "tomorrow" | "custom";
export type WakeupRecurrence = "hourly" | "daily" | "weekly" | "monthly";
export const WAKEUP_WAIT_DAYS = [1, 3, 7, 30] as const;
export const WAKEUP_MAX_FIRES = [5, 10, 20, 50] as const;

/**
 * Whether the draft would wake the issue's agent assignee on members'
 * comments, which already run it. The server folds such a firing into that run.
 */
export function wakesAssigneeOnComments(draft: Pick<WakeupDraft, "condition" | "replyActor" | "events" | "agentId">, assigneeAgentId: string | null) {
  if (!assigneeAgentId || draft.agentId !== assigneeAgentId) return false;
  if (draft.condition === "reply") return draft.replyActor?.type !== "agent";
  return draft.condition === "custom" && draft.events.includes("comment.created");
}

export interface WakeupDraft {
  condition: WakeupCondition | null;
  atPreset: WakeupAtPreset;
  /** A `datetime-local` value, read in the browser's timezone. */
  atCustom: string;
  recurrence: WakeupRecurrence;
  /** "HH:MM" the daily, weekly and monthly schedules fire at. */
  recurrenceTime: string;
  /** Daily schedules only: skip Saturday and Sunday. */
  recurrenceWorkdaysOnly: boolean;
  /** Weekly schedules: 0=Sun … 6=Sat, never empty (see toggleDay). */
  recurrenceDays: number[];
  /** Monthly schedules: the day of the month, 1-31. */
  recurrenceDayOfMonth: number;
  /** A `date` value; the rule ends at the end of that local day. */
  until: string;
  /** Null waits for anyone's reply. */
  replyActor: { type: "member" | "agent"; id: string } | null;
  /** Empty waits for any agent's run. */
  runAgentId: string;
  events: string[];
  field: WakeupField;
  /** Status key, label id or property id, depending on the field. */
  fieldTarget: string;
  /** Property value; select options store their option id. */
  fieldValue: string;
  assignee: { type: "member" | "agent" | "squad"; id: string } | null;
  /** Null waits for every sub-issue. */
  stage: number | null;
  prEvent: "checks_finished" | "merged";
  otherIssue: { id: string; identifier: string } | null;
  otherState: "done" | "ended" | "in_review";
  maxFires: number;
  agentId: string;
  instruction: string;
  mode: "once" | "continuous";
  waitDays: number;
  onTimeout: "wake" | "end";
  /** IANA zone that gives daily schedules their meaning. */
  timezone: string;
}

export type WakeupDraftError =
  | "missing_condition"
  | "missing_value"
  | "missing_issue"
  | "missing_agent"
  | "missing_events"
  | "instruction_invalid"
  | "future_time"
  | "until_future";

export function emptyWakeupDraft(agentId: string, timezone: string): WakeupDraft {
  return {
    condition: null,
    atPreset: "1h",
    atCustom: "",
    recurrence: "daily",
    recurrenceTime: "09:00",
    recurrenceWorkdaysOnly: false,
    recurrenceDays: [1, 2, 3, 4, 5],
    recurrenceDayOfMonth: 1,
    until: "",
    replyActor: null,
    runAgentId: "",
    events: [],
    field: "status",
    fieldTarget: "",
    fieldValue: "",
    assignee: null,
    stage: null,
    prEvent: "checks_finished",
    otherIssue: null,
    otherState: "done",
    maxFires: 20,
    agentId,
    instruction: "",
    mode: "once",
    waitDays: 7,
    onTimeout: "wake",
    timezone,
  };
}

/** Conditions that wait for something to happen, with a deadline. */
export function isEventCondition(condition: WakeupCondition | null) {
  return !!condition && condition !== "at" && condition !== "recurring";
}

/** A property value typed as the property stores it. */
export function propertyConditionValue(type: string | undefined, raw: string): unknown {
  if (type === "checkbox") return raw === "true";
  if (type === "number") {
    const n = Number(raw);
    return raw.trim() !== "" && Number.isFinite(n) ? n : raw;
  }
  return raw;
}

/** The platform-evaluated predicate for a condition choice, if it is one. */
export function platformCondition(
  d: WakeupDraft,
  propertyType?: string,
): { condition: PlatformCondition } | { error: WakeupDraftError } | null {
  switch (d.condition) {
    case "field":
      if (d.field === "assignee") {
        return d.assignee
          ? { condition: { type: "issue_field", field: "assignee", assignee_type: d.assignee.type, assignee_id: d.assignee.id } }
          : { error: "missing_value" };
      }
      if (!d.fieldTarget) return { error: "missing_value" };
      if (d.field === "status") return { condition: { type: "issue_field", field: "status", value: d.fieldTarget } };
      if (d.field === "label") return { condition: { type: "issue_field", field: "label", label_id: d.fieldTarget } };
      if (!d.fieldValue.trim()) return { error: "missing_value" };
      return {
        condition: {
          type: "issue_field",
          field: "property",
          property_id: d.fieldTarget,
          value: propertyConditionValue(propertyType, d.fieldValue.trim()),
        },
      };
    case "children":
      return { condition: d.stage ? { type: "children_done", stage: d.stage } : { type: "children_done" } };
    case "pull_request":
      return { condition: { type: "pull_request", event: d.prEvent } };
    case "other_issue":
      return d.otherIssue
        ? { condition: { type: "other_issue", issue_id: d.otherIssue.id, state: d.otherState } }
        : { error: "missing_issue" };
    default:
      return null;
  }
}

/** The repeating rule as the schedule editor's structured model, so the cron
 *  expression comes from one serializer instead of a second, drifting one. */
type RecurrenceChoice = Pick<
  WakeupDraft,
  "recurrence" | "recurrenceTime" | "recurrenceWorkdaysOnly" | "recurrenceDays" | "recurrenceDayOfMonth" | "timezone"
>;

function recurrenceSchedule(d: RecurrenceChoice): ScheduleConfig {
  let days: DayPattern = { kind: "every" };
  if (d.recurrence === "weekly") days = { kind: "weekly", daysOfWeek: d.recurrenceDays };
  else if (d.recurrence === "monthly") days = { kind: "monthly", dayOfMonth: d.recurrenceDayOfMonth };
  // "Weekdays only" is a five-day week, which the weekly pattern already
  // expresses — cron has no separate weekend-excluding flag.
  else if (d.recurrence === "daily" && d.recurrenceWorkdaysOnly) days = { kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] };
  return {
    // An hourly rule fires on the hour: the minute has no meaning there, and the
    // user sets none.
    time: d.recurrence === "hourly" ? { kind: "every", interval: 1, unit: "hours", window: null, minute: 0 } : { kind: "at", time: d.recurrenceTime },
    days,
    timezone: d.timezone,
    raw: null,
  };
}

/** The five cron fields for a repeating rule; the caller passes the timezone
 *  alongside them, so no `TZ=` prefix is needed. */
function recurrenceCron(d: RecurrenceChoice) {
  return cronFields(recurrenceSchedule(d));
}

/** Maps what a person chose onto the wakeup API; the server re-validates. */
export function buildWakeupInput(
  d: WakeupDraft,
  now = new Date(),
  propertyType?: string,
): { input: IssueWakeupInput } | { error: WakeupDraftError } {
  if (!d.condition) return { error: "missing_condition" };
  if (!d.agentId) return { error: "missing_agent" };
  const instruction = d.instruction.trim();
  if (!instruction || new TextEncoder().encode(instruction).length > 12000) {
    return { error: "instruction_invalid" };
  }
  const base = { agent_id: d.agentId, instruction };
  switch (d.condition) {
    case "at": {
      let at: Date;
      if (d.atPreset === "10m") at = new Date(now.getTime() + 10 * 60_000);
      else if (d.atPreset === "1h") at = new Date(now.getTime() + 60 * 60_000);
      else if (d.atPreset === "tomorrow") {
        at = new Date(now);
        at.setDate(at.getDate() + 1);
        at.setHours(9, 0, 0, 0);
      } else at = new Date(d.atCustom);
      if (!Number.isFinite(at.getTime()) || at.getTime() <= now.getTime()) return { error: "future_time" };
      return { input: { ...base, kind: "at", mode: "once", at: at.toISOString() } };
    }
    case "recurring": {
      const schedule: Pick<IssueWakeupInput, "kind" | "cron_expression" | "timezone"> = {
        kind: "cron",
        cron_expression: recurrenceCron(d),
        timezone: d.timezone,
      };
      // Empty until = no expiry; the rule runs indefinitely until stopped.
      if (!d.until) {
        return { input: { ...base, ...schedule, mode: "continuous" } };
      }
      const end = new Date(`${d.until}T23:59:59`);
      if (!Number.isFinite(end.getTime()) || end.getTime() <= now.getTime()) return { error: "until_future" };
      return { input: { ...base, ...schedule, mode: "continuous", expires_at: end.toISOString() } };
    }
    default: {
      const input: IssueWakeupInput = {
        ...base,
        kind: "event",
        mode: d.mode,
        expires_in_seconds: d.waitDays * 86400,
        on_timeout: d.onTimeout,
      };
      if (d.mode === "continuous") input.max_fires = d.maxFires;
      const platform = platformCondition(d, propertyType);
      if (platform && "error" in platform) return { error: platform.error };
      if (platform) {
        input.condition = platform.condition;
      } else if (d.condition === "reply") {
        input.event_types = ["comment.created"];
        if (d.replyActor) {
          input.filter_actor_type = d.replyActor.type;
          input.filter_actor_id = d.replyActor.id;
        }
      } else if (d.condition === "run_end") {
        input.event_types = RUN_END_EVENTS;
        if (d.runAgentId) input.filter_agent_id = d.runAgentId;
      } else {
        if (d.events.length === 0) return { error: "missing_events" };
        input.event_types = d.events;
      }
      return { input };
    }
  }
}

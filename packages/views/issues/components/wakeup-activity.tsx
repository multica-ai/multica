"use client";

import { Bell, Clock3, Hourglass, ListChecks, RefreshCw, TriangleAlert } from "lucide-react";
import type { TimelineEntry, WakeupCondition, WakeupPreview } from "@multica/core/types";
import { useT } from "../../i18n";
import type { useWakeupText } from "./wakeup-presentation";
import { conditionIcon } from "./wakeups-section";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];
type WakeupText = ReturnType<typeof useWakeupText>;
type ActorName = (type: string, id: string) => string;

/** Timeline entries the wakeup service and the child-done rule write. */
export const WAKEUP_ACTIVITY_ACTIONS = new Set([
  "wakeup_created",
  "wakeup_triggered",
  "wakeup_timed_out",
  "wakeup_paused",
  "wakeup_checkin",
]);

interface StoredPreview {
  id?: string;
  kind?: WakeupPreview["kind"];
  mode?: WakeupPreview["mode"];
  event_types?: string[];
  agent_id?: string;
  timezone?: string;
  condition?: WakeupCondition;
  filter_actor_type?: "member" | "agent";
  filter_actor_id?: string;
  filter_agent_id?: string;
  interval_seconds?: number;
  cron_expression?: string;
  next_fire_at?: string;
  created_by?: string;
  created_by_agent_id?: string;
}

interface WakeupDetails {
  wakeup?: StoredPreview;
  rule?: "child_done";
  stage?: number;
  total?: number;
  target_type?: string;
  target_id?: string;
  /** child_done: woke (a run), notified (a member), merged (joined a waiting run), none. */
  outcome?: "woke" | "notified" | "merged" | "none";
  events?: string[];
  actor_type?: string;
  actor_id?: string;
  woke?: boolean;
  reason?: "max_fires" | "loop" | "rate";
  limit?: number;
  note?: string;
}

function detailsOf(entry: TimelineEntry): WakeupDetails {
  return (entry.details ?? {}) as WakeupDetails;
}

/**
 * The rule as it was when the entry was written, in the shape the wakeup text
 * helpers read. Names come from the workspace directory.
 */
function previewOf(stored: StoredPreview, getActorName: ActorName): WakeupPreview {
  return {
    id: stored.id ?? "",
    issue_id: "",
    agent_id: stored.agent_id ?? "",
    agent_name: stored.agent_id ? getActorName("agent", stored.agent_id) : "",
    kind: stored.kind ?? "event",
    mode: stored.mode ?? "once",
    event_types: stored.event_types ?? [],
    filter_task_id: null,
    filter_agent_name: stored.filter_agent_id ? getActorName("agent", stored.filter_agent_id) : null,
    filter_actor_type: stored.filter_actor_type ?? null,
    filter_actor_id: stored.filter_actor_id ?? null,
    filter_actor_name: stored.filter_actor_type && stored.filter_actor_id ? getActorName(stored.filter_actor_type, stored.filter_actor_id) : null,
    interval_seconds: stored.interval_seconds ?? null,
    cron_expression: stored.cron_expression ?? null,
    timezone: stored.timezone ?? "UTC",
    next_fire_at: stored.next_fire_at ?? null,
    condition: stored.condition ?? null,
  };
}

export function formatWakeupActivity(
  entry: TimelineEntry,
  t: IssuesT,
  text: WakeupText,
  getActorName: ActorName,
): string {
  const details = detailsOf(entry);
  const preview = details.wakeup ? previewOf(details.wakeup, getActorName) : null;
  const condition = preview ? text.trigger(preview) : "";
  const agent = preview?.agent_name ?? "";
  switch (entry.action) {
    case "wakeup_created":
      return t(($) => $.activity.wakeup_created, { condition, agent });
    case "wakeup_triggered": {
      if (details.rule === "child_done") {
        const count = details.total ?? 1;
        const closed = details.stage
          ? t(($) => $.activity.wakeup_child_done_stage, { stage: details.stage, count })
          : t(($) => $.activity.wakeup_child_done_all, { count });
        // The squad's leader ran, but the entry names the assignee people see.
        const name =
          details.target_type && details.target_id
            ? getActorName(details.target_type, details.target_id)
            : t(($) => $.activity.wakeup_assignee);
        const outcome = details.outcome ?? (details.target_type ? "woke" : "none");
        switch (outcome) {
          case "woke":
            return closed + t(($) => $.activity.wakeup_child_done_woke, { name });
          case "notified":
            return closed + t(($) => $.activity.wakeup_child_done_notified, { name });
          case "merged":
            return closed + t(($) => $.activity.wakeup_child_done_merged, { name });
          default:
            return closed;
        }
      }
      if (details.events?.length === 1 && details.events[0] === "wakeup.manual" && details.actor_id) {
        return t(($) => $.activity.wakeup_triggered_manual, { name: getActorName("member", details.actor_id), agent });
      }
      return t(($) => $.activity.wakeup_triggered, { condition, agent });
    }
    case "wakeup_timed_out":
      return details.woke
        ? t(($) => $.activity.wakeup_timed_out_wake, { condition, agent })
        : t(($) => $.activity.wakeup_timed_out_end, { condition });
    case "wakeup_paused":
      return text.paused({ paused_reason: details.reason ?? "rate", max_fires: details.limit ?? null, fire_count: details.limit ?? 0 }) ?? "";
    case "wakeup_checkin": {
      const count = entry.coalesced_count ?? 1;
      const note = details.note ?? "";
      return count > 1
        ? t(($) => $.activity.wakeup_checkin_coalesced, { count, note })
        : t(($) => $.activity.wakeup_checkin, { note });
    }
    default:
      return entry.action ?? "";
  }
}

/**
 * The small tag on the right of an entry that names the rule it came from:
 * the system rule, a schedule for check-ins, or who created the rule.
 */
export function wakeupActivityChip(
  entry: TimelineEntry,
  t: IssuesT,
  text: WakeupText,
  getActorName: ActorName,
): string | null {
  const details = detailsOf(entry);
  if (details.rule === "child_done") return t(($) => $.activity.wakeup_chip_system);
  if (entry.action === "wakeup_created" || !details.wakeup) return null;
  if (entry.action === "wakeup_checkin") return text.trigger(previewOf(details.wakeup, getActorName));
  const creator = details.wakeup.created_by_agent_id
    ? getActorName("agent", details.wakeup.created_by_agent_id)
    : details.wakeup.created_by
      ? getActorName("member", details.wakeup.created_by)
      : null;
  return creator ? t(($) => $.activity.wakeup_chip_created_by, { name: creator }) : null;
}

export function WakeupActivityIcon({ entry }: { entry: TimelineEntry }) {
  const details = detailsOf(entry);
  const className = "h-4 w-4 shrink-0 text-muted-foreground";
  if (details.rule === "child_done") return <ListChecks className={className} aria-hidden="true" />;
  switch (entry.action) {
    case "wakeup_timed_out":
      return <Hourglass className={className} aria-hidden="true" />;
    case "wakeup_paused":
      return <TriangleAlert className="h-4 w-4 shrink-0 text-warning" aria-hidden="true" />;
    case "wakeup_checkin":
      return <RefreshCw className={className} aria-hidden="true" />;
  }
  const Icon = conditionIcon(details.wakeup?.condition) ?? (details.wakeup?.kind && details.wakeup.kind !== "event" ? Clock3 : Bell);
  return <Icon className={className} aria-hidden="true" />;
}

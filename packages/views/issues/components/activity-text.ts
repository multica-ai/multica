import { isBuiltInIssueStatus } from "@multica/core/issue-statuses";
import { formatDateOnly } from "@multica/core/issues/date";
import type { TimelineEntry } from "@multica/core/types";
import type { useT } from "../../i18n";
import { priorityLabel } from "../utils/priority-label";

/** The activity fields the copy reads, shared by issue timelines and the project feed. */
export type ActivityTextEntry = Pick<
  TimelineEntry,
  "action" | "details" | "actor_type" | "actor_id" | "coalesced_count"
>;

export type ActivityT = ReturnType<typeof useT<"issues">>["t"];

/**
 * Labels a status key from the activity feed. `resolveLabel` is the workspace
 * catalog resolver, which names custom statuses; without it — or for a status
 * since deleted — a built-in still gets its i18n name and anything else falls
 * back to the raw key. (MUL-6243)
 */
export function statusLabel(
  status: string,
  t: ActivityT,
  resolveLabel?: (statusKey: string) => string,
): string {
  if (resolveLabel) return resolveLabel(status);
  if (isBuiltInIssueStatus(status)) {
    return t(($) => $.status[status]);
  }
  return status;
}

export function formatActivity(
  entry: ActivityTextEntry,
  t: ActivityT,
  locale: string,
  resolveActorName?: (type: string, id: string) => string,
  resolveStatusLabel?: (statusKey: string) => string,
  requesterName?: string,
): string {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      if (requesterName && entry.actor_type === "agent") {
        return t(($) => $.activity.created_on_behalf_of, { name: requesterName });
      }
      return t(($) => $.activity.created);
    case "status_changed":
      // PR auto-complete (MUL-7429) says why the status moved.
      if (details.source === "pr_automation") {
        return t(($) => $.activity.status_changed_pr, {
          from: statusLabel(details.from ?? "?", t, resolveStatusLabel),
          to: statusLabel(details.to ?? "?", t, resolveStatusLabel),
          prs: details.pull_requests ?? "",
        });
      }
      return t(($) => $.activity.status_changed, {
        from: statusLabel(details.from ?? "?", t, resolveStatusLabel),
        to: statusLabel(details.to ?? "?", t, resolveStatusLabel),
      });
    case "pr_auto_complete_changed":
      return (entry.details as { disabled?: unknown } | undefined)?.disabled === true
        ? t(($) => $.activity.pr_auto_complete_disabled)
        : t(($) => $.activity.pr_auto_complete_enabled);
    case "priority_changed":
      return t(($) => $.activity.priority_changed, {
        from: priorityLabel(details.from ?? "?", t),
        to: priorityLabel(details.to ?? "?", t),
      });
    case "assignee_changed": {
      const isSelfAssign = details.to_type === entry.actor_type && details.to_id === entry.actor_id;
      if (isSelfAssign) return t(($) => $.activity.self_assigned);
      const toName = details.to_id && details.to_type && resolveActorName
        ? resolveActorName(details.to_type, details.to_id)
        : null;
      if (toName) return t(($) => $.activity.assigned_to, { name: toName });
      if (details.from_id && !details.to_id) return t(($) => $.activity.removed_assignee);
      return t(($) => $.activity.changed_assignee);
    }
    case "start_date_changed": {
      if (!details.to) return t(($) => $.activity.start_date_removed);
      const formatted = formatDateOnly(details.to, { month: "short", day: "numeric" }, locale);
      return t(($) => $.activity.start_date_set, { date: formatted });
    }
    case "due_date_changed": {
      if (!details.to) return t(($) => $.activity.due_date_removed);
      const formatted = formatDateOnly(details.to, { month: "short", day: "numeric" }, locale);
      return t(($) => $.activity.due_date_set, { date: formatted });
    }
    case "title_changed":
      return t(($) => $.activity.title_renamed, {
        from: details.from ?? "?",
        to: details.to ?? "?",
      });
    case "description_updated":
      return t(($) => $.activity.description_updated);
    case "duplicate_marked":
      return t(($) => $.activity.duplicate_marked, {
        identifier: details.original_identifier || "?",
      });
    case "duplicate_unmarked": {
      const identifier = details.original_identifier || "?";
      if (details.reason === "original_deleted") {
        return t(($) => $.activity.duplicate_unmarked_original_deleted, { identifier });
      }
      // The row stands in for the status row of the reopen, so it says where
      // the status went.
      if (details.to) {
        return t(($) => $.activity.duplicate_unmarked_to, {
          identifier,
          status: statusLabel(details.to, t, resolveStatusLabel),
        });
      }
      return t(($) => $.activity.duplicate_unmarked, { identifier });
    }
    case "duplicate_added":
      return t(($) => $.activity.duplicate_added, {
        identifier: details.duplicate_identifier || "?",
      });
    case "duplicate_removed":
      return t(($) => $.activity.duplicate_removed, {
        identifier: details.duplicate_identifier || "?",
      });
    case "task_completed": {
      const count = entry.coalesced_count ?? 1;
      return count === 1
        ? t(($) => $.activity.task_completed_single)
        : t(($) => $.activity.task_completed, { count });
    }
    case "task_failed": {
      const count = entry.coalesced_count ?? 1;
      return count === 1
        ? t(($) => $.activity.task_failed_single)
        : t(($) => $.activity.task_failed, { count });
    }
    case "squad_leader_evaluated": {
      const reason = details.reason?.trim();
      switch (details.outcome) {
        case "action":
          return reason
            ? t(($) => $.activity.squad_leader_action_reason, { reason })
            : t(($) => $.activity.squad_leader_action);
        case "no_action":
          return reason
            ? t(($) => $.activity.squad_leader_no_action_reason, { reason })
            : t(($) => $.activity.squad_leader_no_action);
        case "failed":
          return reason
            ? t(($) => $.activity.squad_leader_failed_reason, { reason })
            : t(($) => $.activity.squad_leader_failed);
        default:
          return t(($) => $.activity.squad_leader_evaluated);
      }
    }
    case "environment_controlled": {
      const name = details.label || details.name || "?";
      switch (details.action) {
        case "stop":
          return t(($) => $.activity.environment_stopped, { name });
        case "restart":
          return t(($) => $.activity.environment_restarted, { name });
        default:
          return t(($) => $.activity.environment_started, { name });
      }
    }
    default:
      return entry.action ?? "";
  }
}

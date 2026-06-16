/**
 * Activity-row text formatter. Subset of the web `formatActivity` in
 * packages/views/issues/components/issue-detail.tsx:95 — same actions,
 * English-only copy (mobile v1 is English-only; mirror the structure when
 * mobile gains i18n).
 *
 * Unknown actions fall through to the raw string in `entry.action`. NEVER
 * throw and NEVER drop the row — that's the API Response Compatibility rule
 * from repo-root CLAUDE.md (server may add new action enum values; older
 * mobile clients in the wild must render them as a generic fallback, not
 * crash).
 */
import type {
  IssuePriority,
  IssueStatus,
  TimelineEntry,
} from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";

const STATUS_LABEL: Record<IssueStatus, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
  blocked: "Blocked",
  cancelled: "Cancelled",
};

const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority",
};

function statusName(s: string | undefined): string {
  if (s && s in STATUS_LABEL) return STATUS_LABEL[s as IssueStatus];
  return s ?? "?";
}

function priorityName(p: string | undefined): string {
  if (p && p in PRIORITY_LABEL) return PRIORITY_LABEL[p as IssuePriority];
  return p ?? "?";
}

// start_date / due_date are calendar days — format timezone-safely (no offset
// day shift). Mirrors web's formatActivity in issue-detail.tsx.
function shortDate(date: string | undefined): string {
  if (!date) return "?";
  return formatDateOnly(date, { month: "short", day: "numeric" }, "en-US");
}

export function formatActivity(
  entry: TimelineEntry,
  resolveActorName: (
    type: string | null | undefined,
    id: string | null | undefined,
  ) => string,
): string {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return "created the issue";
    case "status_changed":
      return `changed status: ${statusName(details.from)} → ${statusName(details.to)}`;
    case "priority_changed":
      return `changed priority: ${priorityName(details.from)} → ${priorityName(details.to)}`;
    case "assignee_changed": {
      const isSelf =
        details.to_type === entry.actor_type &&
        details.to_id === entry.actor_id;
      if (isSelf) return "self-assigned";
      if (details.from_id && !details.to_id) return "removed assignee";
      const toName =
        details.to_id && details.to_type
          ? resolveActorName(details.to_type, details.to_id)
          : null;
      if (toName) return `assigned to ${toName}`;
      return "changed assignee";
    }
    case "start_date_changed": {
      if (!details.to) return "removed start date";
      return `set start date to ${shortDate(details.to)}`;
    }
    case "due_date_changed": {
      if (!details.to) return "removed due date";
      return `set due date to ${shortDate(details.to)}`;
    }
    case "title_changed":
      return `renamed: "${details.from ?? "?"}" → "${details.to ?? "?"}"`;
    case "description_updated":
      return "updated description";
    case "task_completed": {
      const n = entry.coalesced_count ?? 1;
      return n > 1 ? `completed ${n} tasks` : "completed a task";
    }
    case "task_failed": {
      const n = entry.coalesced_count ?? 1;
      return n > 1 ? `failed ${n} tasks` : "failed a task";
    }
    case "squad_leader_evaluated": {
      // Copy mirrors packages/views/locales/en/issues.json
      // (squad_leader_action / squad_leader_no_action / squad_leader_failed,
      // each with an optional `_reason` variant).
      const reason = details.reason?.trim();
      switch (details.outcome) {
        case "action":
          return reason
            ? `evaluated and took action: ${reason}`
            : "evaluated and took action";
        case "no_action":
          return reason
            ? `evaluated: no action needed (${reason})`
            : "evaluated: no action needed";
        case "failed":
          return reason
            ? `evaluation failed: ${reason}`
            : "evaluation failed";
        default:
          return "evaluated the squad trigger";
      }
    }
    default:
      return entry.action ?? "";
  }
}


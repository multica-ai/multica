/**
 * Activity-row text formatter. Subset of the web `formatActivity` in
 * packages/views/issues/components/issue-detail.tsx:95 — same actions,
 * Copy is localized through the shared mobile i18n instance.
 *
 * Unknown actions fall through to the raw string in `entry.action`. NEVER
 * throw and NEVER drop the row — that's the API Response Compatibility rule
 * from repo-root CLAUDE.md (server may add new action enum values; older
 * mobile clients in the wild must render them as a generic fallback, not
 * crash).
 */
import type { IssuePriority, TimelineEntry } from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { i18n } from "@/lib/i18n/singleton";
import { STATUS_LABEL, isBuiltInIssueStatus } from "@/lib/issue-status";

const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "issues:priority.urgent",
  high: "issues:priority.high",
  medium: "issues:priority.medium",
  low: "issues:priority.low",
  none: "issues:priority.none",
};

/**
 * Names a status KEY out of a timeline entry. `resolveLabel` comes from the
 * workspace catalog and is what names a CUSTOM status; without it (or for a key
 * the catalog never heard of) a built-in still gets its own copy and anything
 * else falls back to the raw key rather than rendering blank. Mirrors web's
 * `statusLabel` in packages/views/issues/components/issue-detail.tsx.
 * (MUL-6243)
 */
function statusName(
  s: string | undefined,
  resolveLabel?: (statusKey: string) => string,
): string {
  if (!s) return "?";
  if (resolveLabel) return resolveLabel(s);
  if (isBuiltInIssueStatus(s)) return i18n.t(STATUS_LABEL[s]);
  return s;
}

function priorityName(p: string | undefined): string {
  if (p && p in PRIORITY_LABEL) return i18n.t(PRIORITY_LABEL[p as IssuePriority]);
  return p ?? "?";
}

// start_date / due_date are calendar days — format timezone-safely (no offset
// day shift). Mirrors web's formatActivity in issue-detail.tsx.
function shortDate(date: string | undefined): string {
  if (!date) return "?";
  return formatDateOnly(
    date,
    { month: "short", day: "numeric" },
    i18n.resolvedLanguage ?? i18n.language,
  );
}

export function formatActivity(
  entry: TimelineEntry,
  resolveActorName: (
    type: string | null | undefined,
    id: string | null | undefined,
  ) => string,
  resolveStatusLabel?: (statusKey: string) => string,
): string {
  const details = (entry.details ?? {}) as Record<string, string>;
  const t = i18n.t.bind(i18n);
  switch (entry.action) {
    case "created":
      return t("issues:activity.created");
    case "status_changed":
      return t("issues:activity.status_changed", {
        from: statusName(details.from, resolveStatusLabel),
        to: statusName(details.to, resolveStatusLabel),
      });
    case "priority_changed":
      return t("issues:activity.priority_changed", {
        from: priorityName(details.from),
        to: priorityName(details.to),
      });
    case "assignee_changed": {
      const isSelf =
        details.to_type === entry.actor_type &&
        details.to_id === entry.actor_id;
      if (isSelf) return t("issues:activity.self_assigned");
      if (details.from_id && !details.to_id) {
        return t("issues:activity.removed_assignee");
      }
      const toName =
        details.to_id && details.to_type
          ? resolveActorName(details.to_type, details.to_id)
          : null;
      if (toName) return t("issues:activity.assigned_to", { name: toName });
      return t("issues:activity.assignee_changed");
    }
    case "start_date_changed": {
      if (!details.to) return t("issues:activity.start_date_removed");
      return t("issues:activity.start_date_set", { date: shortDate(details.to) });
    }
    case "due_date_changed": {
      if (!details.to) return t("issues:activity.due_date_removed");
      return t("issues:activity.due_date_set", { date: shortDate(details.to) });
    }
    case "title_changed":
      return t("issues:activity.title_changed", {
        from: details.from ?? "?",
        to: details.to ?? "?",
      });
    case "description_updated":
      return t("issues:activity.description_updated");
    // Duplicate marks (MUL-7349); copy mirrors packages/views/locales/en.
    case "duplicate_marked":
      return t("issues:activity.duplicate_marked", {
        identifier: details.original_identifier ?? "?",
      });
    case "duplicate_unmarked": {
      const identifier = details.original_identifier ?? "?";
      if (details.reason === "original_deleted") {
        return t("issues:activity.duplicate_unmarked_original_deleted", {
          identifier,
        });
      }
      if (details.to) {
        return t("issues:activity.duplicate_unmarked_to", {
          identifier,
          status: statusName(details.to, resolveStatusLabel),
        });
      }
      return t("issues:activity.duplicate_unmarked", { identifier });
    }
    case "duplicate_added":
      return t("issues:activity.duplicate_added", {
        identifier: details.duplicate_identifier ?? "?",
      });
    case "duplicate_removed":
      return t("issues:activity.duplicate_removed", {
        identifier: details.duplicate_identifier ?? "?",
      });
    case "task_completed": {
      const n = entry.coalesced_count ?? 1;
      return t("issues:activity.tasks_completed", { count: n });
    }
    case "task_failed": {
      const n = entry.coalesced_count ?? 1;
      return t("issues:activity.tasks_failed", { count: n });
    }
    case "squad_leader_evaluated": {
      // Copy mirrors packages/views/locales/en/issues.json
      // (squad_leader_action / squad_leader_no_action / squad_leader_failed,
      // each with an optional `_reason` variant).
      const reason = details.reason?.trim();
      switch (details.outcome) {
        case "action":
          return reason
            ? t("issues:activity.squad_action_with_reason", { reason })
            : t("issues:activity.squad_action");
        case "no_action":
          return reason
            ? t("issues:activity.squad_no_action_with_reason", { reason })
            : t("issues:activity.squad_no_action");
        case "failed":
          return reason
            ? t("issues:activity.squad_failed_with_reason", { reason })
            : t("issues:activity.squad_failed");
        default:
        return t("issues:activity.squad_evaluated");
      }
    }
    default:
      return entry.action ?? "";
  }
}

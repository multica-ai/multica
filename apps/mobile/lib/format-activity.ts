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
import type { IssuePriority, TimelineEntry } from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { STATUS_LABEL, isIssueStatusCategory } from "@/lib/issue-status";
import { mobileLocale, translate } from "@/i18n";

const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: translate("Urgent"),
  high: translate("High"),
  medium: translate("Medium"),
  low: translate("Low"),
  none: translate("No priority"),
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
  return isIssueStatusCategory(s) ? STATUS_LABEL[s] : s;
}

function priorityName(p: string | undefined): string {
  if (p && p in PRIORITY_LABEL) return PRIORITY_LABEL[p as IssuePriority];
  return p ?? "?";
}

// start_date / due_date are calendar days — format timezone-safely (no offset
// day shift). Mirrors web's formatActivity in issue-detail.tsx.
function shortDate(date: string | undefined): string {
  if (!date) return "?";
  return formatDateOnly(date, { month: "short", day: "numeric" }, mobileLocale);
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
  switch (entry.action) {
    case "created":
      return translate("created the issue");
    case "status_changed":
      return translate("changed status: {{from}} → {{to}}", {
        from: statusName(details.from, resolveStatusLabel),
        to: statusName(details.to, resolveStatusLabel),
      });
    case "priority_changed":
      return translate("changed priority: {{from}} → {{to}}", {
        from: priorityName(details.from),
        to: priorityName(details.to),
      });
    case "assignee_changed": {
      const isSelf =
        details.to_type === entry.actor_type &&
        details.to_id === entry.actor_id;
      if (isSelf) return translate("self-assigned");
      if (details.from_id && !details.to_id)
        return translate("removed assignee");
      const toName =
        details.to_id && details.to_type
          ? resolveActorName(details.to_type, details.to_id)
          : null;
      if (toName) return translate("assigned to {{name}}", { name: toName });
      return translate("changed assignee");
    }
    case "start_date_changed": {
      if (!details.to) return translate("removed start date");
      return translate("set start date to {{date}}", {
        date: shortDate(details.to),
      });
    }
    case "due_date_changed": {
      if (!details.to) return translate("removed due date");
      return translate("set due date to {{date}}", {
        date: shortDate(details.to),
      });
    }
    case "title_changed":
      return translate('renamed: "{{from}}" → "{{to}}"', {
        from: details.from ?? "?",
        to: details.to ?? "?",
      });
    case "description_updated":
      return translate("updated description");
    case "task_completed": {
      const n = entry.coalesced_count ?? 1;
      return n > 1
        ? translate("completed {{count}} tasks", { count: n })
        : translate("completed a task");
    }
    case "task_failed": {
      const n = entry.coalesced_count ?? 1;
      return n > 1
        ? translate("failed {{count}} tasks", { count: n })
        : translate("failed a task");
    }
    case "squad_leader_evaluated": {
      // Copy mirrors packages/views/locales/en/issues.json
      // (squad_leader_action / squad_leader_no_action / squad_leader_failed,
      // each with an optional `_reason` variant).
      const reason = details.reason?.trim();
      switch (details.outcome) {
        case "action":
          return reason
            ? translate("evaluated and took action: {{reason}}", { reason })
            : translate("evaluated and took action");
        case "no_action":
          return reason
            ? translate("evaluated: no action needed ({{reason}})", { reason })
            : translate("evaluated: no action needed");
        case "failed":
          return reason
            ? translate("evaluation failed: {{reason}}", { reason })
            : translate("evaluation failed");
        default:
          return translate("evaluated the squad trigger");
      }
    }
    default:
      return entry.action ?? "";
  }
}

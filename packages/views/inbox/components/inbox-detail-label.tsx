"use client";

import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { formatDateOnly } from "@multica/core/issues/date";
import { useActorName } from "@multica/core/workspace/hooks";
import { StatusIcon, PriorityIcon } from "../../issues/components";
import type { InboxItem, InboxItemType, IssueStatus, IssuePriority } from "@multica/core/types";
import { getQuickCreateOutcomeDetail } from "./inbox-display";
import { useLocale, useT } from "../../i18n";
import { useStatusLabel } from "../../issues/utils/status-label";
import { priorityLabel } from "../../issues/utils/priority-label";

// Hook returning the inbox-item type → human label map. Replaces the
// previous static `typeLabels` const so the labels can flow through
// i18next. Call sites keep the same `typeLabels[type]` access pattern.
export function useTypeLabels(): Record<InboxItemType, string> {
  const { t } = useT("inbox");
  return {
    issue_assigned: t(($) => $.types.issue_assigned),
    issue_subscribed: t(($) => $.types.issue_subscribed),
    unassigned: t(($) => $.types.unassigned),
    assignee_changed: t(($) => $.types.assignee_changed),
    status_changed: t(($) => $.types.status_changed),
    priority_changed: t(($) => $.types.priority_changed),
    start_date_changed: t(($) => $.types.start_date_changed),
    due_date_changed: t(($) => $.types.due_date_changed),
    new_comment: t(($) => $.types.new_comment),
    mentioned: t(($) => $.types.mentioned),
    review_requested: t(($) => $.types.review_requested),
    task_completed: t(($) => $.types.task_completed),
    task_failed: t(($) => $.types.task_failed),
    agent_blocked: t(($) => $.types.agent_blocked),
    agent_completed: t(($) => $.types.agent_completed),
    reaction_added: t(($) => $.types.reaction_added),
    quick_create_done: t(($) => $.types.quick_create_done),
    quick_create_failed: t(($) => $.types.quick_create_failed),
    quick_create_unconfirmed: t(($) => $.types.quick_create_unconfirmed),
    autopilot_paused: t(($) => $.types.autopilot_paused),
    autopilot_quota_exceeded: t(($) => $.types.autopilot_quota_exceeded),
  };
}

// start_date / due_date are calendar days — format timezone-safely so the day
// never shifts with the viewer's offset (see @multica/core/issues/date).
function shortDate(dateStr: string, locale: string): string {
  return formatDateOnly(dateStr, { month: "short", day: "numeric" }, locale);
}

export function InboxDetailLabel({ item }: { item: InboxItem }) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const locale = useLocale();
  const typeLabels = useTypeLabels();
  const { getActorName } = useActorName();
  // Inbox is a cross-workspace surface, so the catalog is read per item's own
  // workspace rather than from the route. (MUL-6243)
  const { categoryOf, colorOf } = useIssueStatuses(item.workspace_id);
  const statusLabelOf = useStatusLabel(item.workspace_id);
  const details = item.details ?? {};

  switch (item.type) {
    case "status_changed": {
      if (!details.to) return <span>{typeLabels[item.type]}</span>;
      return (
        <span className="inline-flex items-center gap-1">
          {t(($) => $.labels.set_status_to)}
          <StatusIcon
            status={details.to as IssueStatus}
            category={categoryOf(details.to)}
            color={colorOf(details.to)}
            className="h-3 w-3"
          />
          {statusLabelOf(details.to)}
        </span>
      );
    }
    case "priority_changed": {
      if (!details.to) return <span>{typeLabels[item.type]}</span>;
      const label = priorityLabel(details.to, tIssues);
      return (
        <span className="inline-flex items-center gap-1">
          {t(($) => $.labels.set_priority_to)}
          <PriorityIcon priority={details.to as IssuePriority} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "issue_assigned": {
      if (details.new_assignee_id) {
        return <span>{t(($) => $.labels.assigned_to, { name: getActorName(details.new_assignee_type ?? "member", details.new_assignee_id) })}</span>;
      }
      return <span>{typeLabels[item.type]}</span>;
    }
    case "unassigned":
      return <span>{t(($) => $.labels.removed_assignee)}</span>;
    case "assignee_changed": {
      if (details.new_assignee_id) {
        return <span>{t(($) => $.labels.assigned_to, { name: getActorName(details.new_assignee_type ?? "member", details.new_assignee_id) })}</span>;
      }
      return <span>{typeLabels[item.type]}</span>;
    }
    case "start_date_changed": {
      if (details.to) return <span>{t(($) => $.labels.set_start_date_to, { date: shortDate(details.to, locale) })}</span>;
      return <span>{t(($) => $.labels.removed_start_date)}</span>;
    }
    case "due_date_changed": {
      if (details.to) return <span>{t(($) => $.labels.set_due_date_to, { date: shortDate(details.to, locale) })}</span>;
      return <span>{t(($) => $.labels.removed_due_date)}</span>;
    }
    case "new_comment": {
      if (item.body) return <span>{item.body}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "reaction_added": {
      const emoji = details.emoji;
      if (emoji) return <span>{t(($) => $.labels.reacted_to_comment, { emoji })}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "quick_create_done": {
      const identifier = details.identifier;
      if (identifier) return <span>{t(($) => $.labels.created_with_agent, { identifier })}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "quick_create_failed": {
      const detail = getQuickCreateOutcomeDetail(item);
      if (detail) return <span>{t(($) => $.labels.failed_with_detail, { detail })}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "quick_create_unconfirmed": {
      // Deliberately NOT the failed_with_detail label: the outcome is unknown,
      // so the detail is shown as-is with no "Failed:" framing.
      const detail = getQuickCreateOutcomeDetail(item);
      if (detail) return <span>{detail}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "autopilot_quota_exceeded":
      return <span>{t(($) => $.labels.autopilot_quota_blocked)}</span>;
    default:
      return <span>{typeLabels[item.type] ?? item.type}</span>;
  }
}

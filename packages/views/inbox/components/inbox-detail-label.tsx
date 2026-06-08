"use client";

import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { useActorName } from "@multica/core/workspace/hooks";
import { StatusIcon, PriorityIcon } from "../../issues/components";
import type { InboxItem, InboxItemType, IssueStatus, IssuePriority } from "@multica/core/types";
import { getQuickCreateFailureDetail } from "./inbox-display";
import { useT } from "../../i18n";

// Hook returning the inbox-item type → human label map. Replaces the
// previous static `typeLabels` const so the labels can flow through
// i18next. Call sites keep the same `typeLabels[type]` access pattern.
export function useTypeLabels(): Record<InboxItemType, string> {
  const { t } = useT("inbox");
  return {
    issue_assigned: t(($) => $.types.issue_assigned),
    issue_started: t(($) => $.types.issue_started),
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
    reminder: t(($) => $.types.reminder), // CEREBRO-PATCH(inbox-reminders-label): render reminder type labels.
    private_agent_run_request: t(($) => $.types.private_agent_run_request), // CEREBRO-PATCH(inbox-run-request-label): FIR-2385.
    // CEREBRO-PATCH(issue-date-reminders): FIR-2412 start/due date reminder labels.
    due_date_reminder: t(($) => $.types.due_date_reminder),
    start_date_reminder: t(($) => $.types.start_date_reminder),
    // CEREBRO-PATCH(inbox-skill-change-request): FIR-2629 inline Danish labels (cerebro feature) — keeps en/ko/zh-Hans parity untouched.
    skill_change_request_created: "Foreslået ændring til skill",
    skill_change_request_reviewed: "Dit skill-forslag er behandlet",
    runtime_auto_paused: t(($) => $.types.runtime_auto_paused), // CEREBRO-PATCH(inbox-runtime-pause-label): FIR-2611.
    // CEREBRO-PATCH(inbox-detail-label-agent-comment-split): TECH-2961 — three labels for agent-authored comments split by the tag they carry.
    agent_comment_no_tag: t(($) => $.types.agent_comment_no_tag),
    agent_comment_member_tag: t(($) => $.types.agent_comment_member_tag),
    agent_comment_agent_tag: t(($) => $.types.agent_comment_agent_tag),
    manually_added: t(($) => $.types.manually_added), // CEREBRO-PATCH(cerebro-inbox-add-issue): label for manually added inbox items.
  };
}

function shortDate(dateStr: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

export function InboxDetailLabel({ item }: { item: InboxItem }) {
  const { t } = useT("inbox");
  const typeLabels = useTypeLabels();
  const { getActorName } = useActorName();
  const details = item.details ?? {};

  switch (item.type) {
    case "status_changed": {
      if (!details.to) return <span>{typeLabels[item.type]}</span>;
      const label = STATUS_CONFIG[details.to as IssueStatus]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          {t(($) => $.labels.set_status_to)}
          <StatusIcon status={details.to as IssueStatus} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "priority_changed": {
      if (!details.to) return <span>{typeLabels[item.type]}</span>;
      const label = PRIORITY_CONFIG[details.to as IssuePriority]?.label ?? details.to;
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
      if (details.to) return <span>{t(($) => $.labels.set_start_date_to, { date: shortDate(details.to) })}</span>;
      return <span>{t(($) => $.labels.removed_start_date)}</span>;
    }
    case "due_date_changed": {
      if (details.to) return <span>{t(($) => $.labels.set_due_date_to, { date: shortDate(details.to) })}</span>;
      return <span>{t(($) => $.labels.removed_due_date)}</span>;
    }
    case "new_comment":
    // CEREBRO-PATCH(inbox-detail-label-agent-comment-split): TECH-2961 — every agent-comment variant renders the same body line as a normal comment.
    case "agent_comment_no_tag":
    case "agent_comment_member_tag":
    case "agent_comment_agent_tag": {
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
      const detail = getQuickCreateFailureDetail(item);
      if (detail) return <span>{t(($) => $.labels.failed_with_detail, { detail })}</span>;
      return <span>{typeLabels[item.type]}</span>;
    }
    case "reminder": // CEREBRO-PATCH(inbox-reminders-label): show reminder body in detail labels.
      return <span>{item.body || details.text || typeLabels[item.type]}</span>;
    case "runtime_auto_paused": // CEREBRO-PATCH(inbox-runtime-pause-label): FIR-2611 — show resume/manual line.
      return <span>{item.body || typeLabels[item.type]}</span>;
    default:
      return <span>{typeLabels[item.type] ?? item.type}</span>;
  }
}

"use client";

import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT, useLocale } from "@multica/i18n/react";
import { StatusIcon, PriorityIcon } from "../../issues/components";
import type { InboxItem, InboxItemType, IssueStatus, IssuePriority } from "@multica/core/types";
import { getQuickCreateFailureDetail } from "./inbox-display";

const typeLabelKeys: Record<InboxItemType, string> = {
  issue_assigned: "type_assigned",
  unassigned: "type_unassigned",
  assignee_changed: "type_assignee_changed",
  status_changed: "type_status_changed",
  priority_changed: "type_priority_changed",
  due_date_changed: "type_due_date_changed",
  new_comment: "type_new_comment",
  mentioned: "type_mentioned",
  review_requested: "type_review_requested",
  task_completed: "type_task_completed",
  task_failed: "type_task_failed",
  agent_blocked: "type_agent_blocked",
  agent_completed: "type_agent_completed",
  reaction_added: "type_reaction",
  quick_create_done: "type_quick_create_done",
  quick_create_failed: "type_quick_create_failed",
};

export { typeLabelKeys };

function shortDate(dateStr: string, locale: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleDateString(locale, {
    month: "short",
    day: "numeric",
  });
}

export function InboxDetailLabel({ item }: { item: InboxItem }) {
  const { getActorName } = useActorName();
  const t = useT("inbox");
  const { locale } = useLocale();
  const details = item.details ?? {};

  switch (item.type) {
    case "status_changed": {
      if (!details.to) return <span>{t(typeLabelKeys[item.type])}</span>;
      const label = STATUS_CONFIG[details.to as IssueStatus]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          {t("detail_set_status")}
          <StatusIcon status={details.to as IssueStatus} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "priority_changed": {
      if (!details.to) return <span>{t(typeLabelKeys[item.type])}</span>;
      const label = PRIORITY_CONFIG[details.to as IssuePriority]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          {t("detail_set_priority")}
          <PriorityIcon priority={details.to as IssuePriority} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "issue_assigned": {
      if (details.new_assignee_id) {
        const name = getActorName(details.new_assignee_type ?? "member", details.new_assignee_id);
        return <span>{t("detail_assigned_to", { name })}</span>;
      }
      return <span>{t(typeLabelKeys[item.type])}</span>;
    }
    case "unassigned":
      return <span>{t("detail_removed_assignee")}</span>;
    case "assignee_changed": {
      if (details.new_assignee_id) {
        const name = getActorName(details.new_assignee_type ?? "member", details.new_assignee_id);
        return <span>{t("detail_assigned_to", { name })}</span>;
      }
      return <span>{t(typeLabelKeys[item.type])}</span>;
    }
    case "due_date_changed": {
      if (details.to) return <span>{t("detail_set_due_date", { date: shortDate(details.to, locale) })}</span>;
      return <span>{t("detail_removed_due_date")}</span>;
    }
    case "new_comment": {
      if (item.body) return <span>{item.body}</span>;
      return <span>{t(typeLabelKeys[item.type])}</span>;
    }
    case "reaction_added": {
      const emoji = details.emoji;
      if (emoji) return <span>{t("detail_reacted", { emoji })}</span>;
      return <span>{t(typeLabelKeys[item.type])}</span>;
    }
    case "quick_create_done": {
      const identifier = details.identifier;
      if (identifier) return <span>{t("detail_created_with_agent", { name: identifier })}</span>;
      return <span>{t(typeLabelKeys[item.type])}</span>;
    }
    case "quick_create_failed": {
      const detail = getQuickCreateFailureDetail(item);
      if (detail) return <span>{t("detail_failed", { message: detail })}</span>;
      return <span>{t(typeLabelKeys[item.type])}</span>;
    }
    default:
      return <span>{t(typeLabelKeys[item.type]) ?? item.type}</span>;
  }
}

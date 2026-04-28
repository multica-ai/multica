"use client";

import { STATUS_CONFIG, PRIORITY_CONFIG } from "@multica/core/issues/config";
import { useActorName } from "@multica/core/workspace/hooks";
import { useLocale } from "@multica/core/i18n";
import { StatusIcon, PriorityIcon } from "../../issues/components";
import type { InboxItem, IssueStatus, IssuePriority } from "@multica/core/types";
import { useInboxT } from "../i18n";

function formatShortDate(dateStr: string, locale: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleDateString(locale, {
    month: "short",
    day: "numeric",
  });
}

export function InboxDetailLabel({ item }: { item: InboxItem }) {
  const t = useInboxT();
  const { locale } = useLocale();
  const { getActorName } = useActorName();
  const details = item.details ?? {};

  switch (item.type) {
    case "status_changed": {
      if (!details.to) return <span>{t.types[item.type]}</span>;
      const label = STATUS_CONFIG[details.to as IssueStatus]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          {t.detail.setStatusTo}
          <StatusIcon status={details.to as IssueStatus} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "priority_changed": {
      if (!details.to) return <span>{t.types[item.type]}</span>;
      const label = PRIORITY_CONFIG[details.to as IssuePriority]?.label ?? details.to;
      return (
        <span className="inline-flex items-center gap-1">
          {t.detail.setPriorityTo}
          <PriorityIcon priority={details.to as IssuePriority} className="h-3 w-3" />
          {label}
        </span>
      );
    }
    case "issue_assigned": {
      if (details.new_assignee_id) {
        return <span>{t.detail.assignedTo(getActorName(details.new_assignee_type ?? "member", details.new_assignee_id))}</span>;
      }
      return <span>{t.types[item.type]}</span>;
    }
    case "unassigned":
      return <span>{t.detail.removedAssignee}</span>;
    case "assignee_changed": {
      if (details.new_assignee_id) {
        return <span>{t.detail.assignedTo(getActorName(details.new_assignee_type ?? "member", details.new_assignee_id))}</span>;
      }
      return <span>{t.types[item.type]}</span>;
    }
    case "due_date_changed": {
      if (details.to) return <span>{t.detail.setDueDateTo(formatShortDate(details.to, locale))}</span>;
      return <span>{t.detail.removedDueDate}</span>;
    }
    case "new_comment": {
      if (item.body) return <span>{item.body}</span>;
      return <span>{t.types[item.type]}</span>;
    }
    case "reaction_added": {
      const emoji = details.emoji;
      if (emoji) return <span>{t.detail.reactedToComment(emoji)}</span>;
      return <span>{t.types[item.type]}</span>;
    }
    default:
      return <span>{t.types[item.type] ?? item.type}</span>;
  }
}

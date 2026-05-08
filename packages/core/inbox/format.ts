import { PRIORITY_CONFIG, STATUS_CONFIG } from "../issues/config";
import type { InboxItem, InboxItemType, IssuePriority, IssueStatus } from "../types";

export const inboxTypeLabels: Record<InboxItemType, string> = {
  issue_assigned: "Assigned",
  unassigned: "Unassigned",
  assignee_changed: "Assignee changed",
  status_changed: "Status changed",
  priority_changed: "Priority changed",
  due_date_changed: "Due date changed",
  new_comment: "New comment",
  mentioned: "Mentioned",
  review_requested: "Review requested",
  task_completed: "Task completed",
  task_failed: "Task failed",
  agent_blocked: "Agent blocked",
  agent_completed: "Agent completed",
  reaction_added: "Reacted",
  quick_create_done: "Quick create completed",
  quick_create_failed: "Quick create failed",
};

export function formatInboxTimeAgo(dateStr: string, now = Date.now()): string {
  const diff = now - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

export function formatInboxShortDate(dateStr: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

export function formatInboxDetailText(
  item: InboxItem,
  getActorName: (type: string, id: string) => string,
): string {
  const details = item.details ?? {};

  switch (item.type) {
    case "status_changed": {
      if (!details.to) return inboxTypeLabels[item.type];
      const label = STATUS_CONFIG[details.to as IssueStatus]?.label ?? details.to;
      return `Set status to ${label}`;
    }
    case "priority_changed": {
      if (!details.to) return inboxTypeLabels[item.type];
      const label = PRIORITY_CONFIG[details.to as IssuePriority]?.label ?? details.to;
      return `Set priority to ${label}`;
    }
    case "issue_assigned": {
      if (details.new_assignee_id) {
        return `Assigned to ${getActorName(details.new_assignee_type ?? "member", details.new_assignee_id)}`;
      }
      return inboxTypeLabels[item.type];
    }
    case "unassigned":
      return "Removed assignee";
    case "assignee_changed": {
      if (details.new_assignee_id) {
        return `Assigned to ${getActorName(details.new_assignee_type ?? "member", details.new_assignee_id)}`;
      }
      return inboxTypeLabels[item.type];
    }
    case "due_date_changed": {
      if (details.to) return `Set due date to ${formatInboxShortDate(details.to)}`;
      return "Removed due date";
    }
    case "new_comment": {
      if (item.body) return item.body;
      return inboxTypeLabels[item.type];
    }
    case "reaction_added": {
      const emoji = details.emoji;
      if (emoji) return `Reacted ${emoji} to your comment`;
      return inboxTypeLabels[item.type];
    }
    default:
      return inboxTypeLabels[item.type] ?? item.type;
  }
}

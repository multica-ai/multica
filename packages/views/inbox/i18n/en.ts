import type { InboxDict } from "./types";

export function createEnDict(): InboxDict {
  return {
    page: {
      title: "Inbox",
      backToInbox: "Inbox",
      emptyListTitle: "No notifications",
      emptyDetailWithItems: "Select a notification to view details",
      emptyDetailNoItems: "Your inbox is empty",
    },
    actions: {
      markAllRead: "Mark all as read",
      archiveAll: "Archive all",
      archiveAllRead: "Archive all read",
      archiveCompleted: "Archive completed",
      archive: "Archive",
    },
    errors: {
      markRead: "Failed to mark as read",
      archive: "Failed to archive",
      markAllRead: "Failed to mark all as read",
      archiveAll: "Failed to archive all",
      archiveRead: "Failed to archive read items",
      archiveCompleted: "Failed to archive completed",
    },
    timeAgo: {
      justNow: "just now",
      minutes: (n) => `${n}m`,
      hours: (n) => `${n}h`,
      days: (n) => `${n}d`,
    },
    types: {
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
    },
    detail: {
      setStatusTo: "Set status to",
      setPriorityTo: "Set priority to",
      assignedTo: (name) => `Assigned to ${name}`,
      removedAssignee: "Removed assignee",
      setDueDateTo: (date) => `Set due date to ${date}`,
      removedDueDate: "Removed due date",
      reactedToComment: (emoji) => `Reacted ${emoji} to your comment`,
    },
  };
}

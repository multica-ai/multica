import type { IssueAssigneeType, IssuePriority, IssueStatus } from "./issue";

export type InboxSeverity = "action_required" | "attention" | "info";

export type InboxItemType =
  | "issue_assigned"
  | "issue_subscribed"
  // A new issue in a project the recipient subscribes to.
  | "issue_created"
  | "unassigned"
  | "assignee_changed"
  | "status_changed"
  | "priority_changed"
  | "start_date_changed"
  | "due_date_changed"
  | "new_comment"
  | "mentioned"
  | "review_requested"
  | "task_completed"
  | "task_failed"
  // A run the recipient started was cancelled. `details` names who by
  // (`cancelled_by_type`, `cancelled_by_id`, `cancelled_by_name`).
  | "task_cancelled"
  | "agent_blocked"
  | "agent_completed"
  | "reaction_added"
  | "quick_create_done"
  | "quick_create_failed"
  // Quick create whose outcome could not be verified. Distinct from
  // quick_create_failed because it must NOT be rendered with failure framing:
  // the issue may actually have been created.
  | "quick_create_unconfirmed"
  // System notifications are intentionally issue-less. Keep them in the
  // same Inbox model so read/archive/realtime behavior remains consistent.
  | "autopilot_paused"
  | "autopilot_quota_exceeded"
  // Sub-issues of an issue assigned to the recipient closed (the child_done
  // system rule notifies a member assignee instead of waking an agent).
  | "children_done"
  // Connected Apps (MUL-8516): an agent waits on the recipient to confirm an
  // app call, or to connect their account.
  | "app_action_requested"
  | "app_action_reminder"
  | "app_connect_requested"
  | "app_action_result";

/**
 * The Inbox section a notification was filed under when it was written.
 * `needs_you`: it is addressed to the recipient, so the issue waits on them.
 * `updates`: they only follow the issue.
 */
export type InboxSection = "needs_you" | "updates";

/**
 * One workspace's inbox counts in the cross-workspace summary
 * (`GET /api/inbox/unread-summary`). The rail badge and the workspace
 * switcher dot read it.
 */
export interface InboxWorkspaceUnread {
  workspace_id: string;
  /** Unread issues. */
  count: number;
  /**
   * Issues waiting on the user, read or not: the rows of the Inbox's "Needs
   * you" section, and what the badge shows. Absent from a server that
   * predates the sectioned Inbox, where the badge falls back to `count`.
   */
  needs_you?: number;
  /**
   * When the workspace's next snoozed issue comes back. The counts leave it
   * out until then and nothing arrives when the time passes, so the summary
   * is read again at this time. Absent when nothing is snoozed.
   */
  next_wake_at?: string;
}

export interface InboxItem {
  id: string;
  workspace_id: string;
  recipient_type: "member" | "agent";
  recipient_id: string;
  actor_type: "member" | "agent" | "system" | null;
  actor_id: string | null;
  type: InboxItemType;
  severity: InboxSeverity;
  issue_id: string | null;
  title: string;
  body: string | null;
  issue_status: IssueStatus | null;
  /**
   * Current priority of the linked issue. Optional so an installed Desktop
   * client remains compatible with an older backend that predates this Inbox
   * projection; null also covers notifications without a linked issue.
   */
  issue_priority?: IssuePriority | null;
  /**
   * The linked issue's fields the inbox filters match on. Optional for the
   * same reason as `issue_priority`; a server that has them always sends
   * `issue_label_ids` as an array.
   */
  issue_project_id?: string | null;
  issue_assignee_type?: IssueAssigneeType | null;
  issue_assignee_id?: string | null;
  issue_due_date?: string | null;
  issue_label_ids?: string[] | null;
  read: boolean;
  archived: boolean;
  created_at: string;
  details: Record<string, string> | null;
  /**
   * Absent from a server that predates the sectioned Inbox; every row then
   * belongs to the one list it had.
   */
  section?: InboxSection | null;
  /**
   * When a snoozed row comes back. A time in the past means it already has.
   * Snoozing is issue-level, so every row the issue had at the time carries
   * the same value.
   */
  snoozed_until?: string | null;
}


export interface ArchivedInboxPage {
  items: InboxItem[];
  nextCursor: string | null;
  hasMore: boolean;
}

export interface ArchivedInboxFacets {
  statuses: Record<string, number>;
  priorities: Record<string, number>;
  actors: Record<string, number>;
  types: Record<string, number>;
  projects: Record<string, number>;
  assignees: Record<string, number>;
  labels: Record<string, number>;
  unreadCount: number;
}

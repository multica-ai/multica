// CEREBRO-PATCH(core-types-inbox): cerebro modification of upstream file
import type { IssueStatus } from "./issue";

export type InboxSeverity = "action_required" | "attention" | "info";

export type InboxItemType =
  | "issue_assigned"
  | "unassigned"
  | "assignee_changed"
  | "status_changed"
  | "priority_changed"
  | "due_date_changed"
  | "new_comment"
  | "mentioned"
  | "review_requested"
  | "task_completed"
  | "task_failed"
  | "agent_blocked"
  | "agent_completed"
  | "reaction_added"
  | "quick_create_done"
  | "quick_create_failed";

// Where the item is rendered in the UI. 'inbox' = persistent inbox queue.
// 'notifications' = lightweight notifications page anchored in the bottom of
// the sidebar. The route is decided server-side at insert time from the
// recipient's preferences and is read-only on the client.
export type InboxRoute = "inbox" | "notifications";

export interface InboxItem {
  id: string;
  workspace_id: string;
  recipient_type: "member" | "agent";
  recipient_id: string;
  actor_type: "member" | "agent" | "system" | null;
  actor_id: string | null;
  type: InboxItemType;
  severity: InboxSeverity;
  route: InboxRoute;
  issue_id: string | null;
  project_id: string | null;
  title: string;
  body: string | null;
  issue_status: IssueStatus | null;
  read: boolean;
  archived: boolean;
  // CEREBRO-PATCH(core-types-inbox): mute until this RFC3339 timestamp;
  // null when the item is not muted.
  muted_until: string | null;
  created_at: string;
  details: Record<string, string> | null;
}

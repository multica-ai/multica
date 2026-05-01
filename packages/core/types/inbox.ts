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
  | "reaction_added";

export interface InboxFolder {
  id: string;
  workspace_id: string;
  user_id: string;
  name: string;
  position: number;
  created_at: string;
  parent_id: string | null;
}

export type InboxFolderItemType = "chat_session" | "notification";

export interface InboxFolderMembership {
  folder_id: string;
  item_type: InboxFolderItemType;
  item_id: string;
  added_at: string;
}

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
  actor_type: "member" | "agent" | null;
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
  created_at: string;
  details: Record<string, string> | null;
}

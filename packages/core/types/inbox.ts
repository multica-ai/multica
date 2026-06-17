import type { IssueStatus } from "./issue";

export type InboxDetails = Record<string, unknown> & {
  comment_id?: string;
  identifier?: string;
  original_prompt?: string;
  error?: string;
  source_type?: string;
  channel_id?: string;
  channel_name?: string;
  message_id?: string;
  thread_id?: string;
  reply_to_id?: string;
  link?: string;
  to?: string;
  from?: string;
  emoji?: string;
  agent_id?: string;
  new_assignee_id?: string;
  new_assignee_type?: string;
  actor?: unknown;
};

export type InboxSeverity = "action_required" | "attention" | "info";

export type InboxItemType =
  | "issue_assigned"
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
  | "agent_blocked"
  | "agent_completed"
  | "reaction_added"
  | "quick_create_done"
  | "quick_create_failed";

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
  read: boolean;
  archived: boolean;
  created_at: string;
  details: InboxDetails | null;
}

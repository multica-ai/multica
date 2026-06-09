// CEREBRO-PATCH(core-types-issue): cerebro modification of upstream file
import type { Attachment } from "./attachment";
import type { Label } from "./label";

export type IssueStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent" | "squad";

export type IssueKind = "issue" | "channel" | "dm";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

// CEREBRO-PATCH(issue-dependencies): blocks / blocked-by / related relation types.
export interface IssueDependencyRef {
  id: string;
  identifier: string;
  title: string;
  status: IssueStatus;
}

// CEREBRO-PATCH(issue-dependencies): grouped dependencies response shape.
export interface IssueDependenciesResponse {
  blocks: IssueDependencyRef[];
  blocked_by: IssueDependencyRef[];
  related: IssueDependencyRef[];
}

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  kind: IssueKind;
  title: string;
  description: string | null;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  creator_type: IssueAssigneeType;
  creator_id: string;
  parent_issue_id: string | null;
  project_id: string | null;
  position: number;
  metadata?: Record<string, unknown>;
  // Calendar days as date-only "YYYY-MM-DD" (no time, no timezone). Use the
  // helpers in @multica/core/issues/date to format/compare — never `new Date()`
  // + local formatting, which shifts the day by the viewer's offset.
  start_date: string | null;
  due_date: string | null;
  is_private?: boolean;
  reactions?: IssueReaction[];
  attachments?: Attachment[];
  labels?: Label[];
  // CEREBRO-PATCH(issue-dependencies): embedded relation snapshots (authoritative
  // source is GET /api/issues/{id}/dependencies; these are a bonus for lists).
  blocks?: IssueDependencyRef[];
  blocked_by?: IssueDependencyRef[];
  created_at: string;
  updated_at: string;
}

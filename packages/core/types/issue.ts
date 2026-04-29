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

export type IssueAssigneeType = "member" | "agent";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
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
  due_date: string | null;
  reactions?: IssueReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
}

export type IssueExecutionState =
  | "idle"
  | "queued"
  | "running"
  | "failed"
  | "completed";

export interface IssueExecutionSummary {
  issue_id: string;
  state: IssueExecutionState;
  queued_count: number;
  running_count: number;
  latest_task_id: string | null;
  latest_agent_id: string | null;
  latest_completed_at: string | null;
  latest_error: string | null;
  latest_trigger_comment_id: string | null;
  latest_trigger_excerpt: string | null;
}

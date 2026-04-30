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

export interface IssueTimerEntry {
  id: string;
  actor_type: IssueAssigneeType;
  actor_id: string;
  source: "manual" | "agent_task";
  task_id: string | null;
  started_at: string;
  stopped_at: string | null;
}

export interface IssueTimerSummary {
  issue_id: string;
  total_seconds: number;
  entry_count: number;
  active_timer: IssueTimerEntry | null;
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
  time_tracking?: IssueTimerSummary;
  created_at: string;
  updated_at: string;
}

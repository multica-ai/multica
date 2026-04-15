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

export interface AcceptanceCriterion {
  id: string;
  description: string;
  completed: boolean;
}

export interface ContextRef {
  type: "issue" | "file" | "url";
  ref: string;
  title: string;
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
  acceptance_criteria: AcceptanceCriterion[];
  context_refs: ContextRef[];
  scope: string[];
  position: number;
  due_date: string | null;
  reactions?: IssueReaction[];
  created_at: string;
  updated_at: string;
}

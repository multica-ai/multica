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
export type IssueDependencyType = "blocks" | "blocked_by" | "related" | "copy";

export interface IssueReference {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  status: IssueStatus;
  priority: IssuePriority;
  parent_issue_id: string | null;
}

export interface IssueLabel {
  id: string;
  workspace_id: string;
  name: string;
  color: string;
}

export interface IssueDependency {
  id: string;
  type: IssueDependencyType;
  issue: IssueReference;
}

export interface IssueDependencyGroups {
  blocks?: IssueDependency[];
  blocked_by?: IssueDependency[];
  related?: IssueDependency[];
  copy?: IssueDependency[];
}

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
	  issue_type_id?: string | null;
	  position: number;
  due_date: string | null;
  start_date: string | null;
  end_date: string | null;
  archived_at: string | null;
  archived_by: string | null;
  parent_issue?: IssueReference | null;
  child_issues?: IssueReference[];
  labels?: IssueLabel[];
  dependencies?: IssueDependencyGroups | null;
  reactions?: IssueReaction[];
  created_at: string;
  updated_at: string;
}

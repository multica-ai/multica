import type { Issue, IssueStatus, IssuePriority, IssueAssigneeType } from "./issue";
import type { MemberRole } from "./workspace";
import type { Project } from "./project";

// Issue API
export interface CreateIssueRequest {
  title: string;
  description?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType;
  assignee_id?: string;
  parent_issue_id?: string;
  project_id?: string;
  due_date?: string;
  attachment_ids?: string[];
}

export interface UpdateIssueRequest {
  title?: string;
  description?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  position?: number;
  due_date?: string | null;
  parent_issue_id?: string | null;
  project_id?: string | null;
}

export interface ListIssuesParams {
  limit?: number;
  offset?: number;
  workspace_id?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_id?: string;
  assignee_ids?: string[];
  creator_id?: string;
  open_only?: boolean;
}

export interface ListIssuesResponse {
  issues: Issue[];
  total: number;
  /** True total of done issues in the workspace (for load-more pagination). Not returned by backend API — set by the frontend query function. */
  doneTotal?: number;
}

export interface SearchIssueResult extends Issue {
  match_source: "title" | "description" | "comment";
  matched_snippet?: string;
}

export interface SearchIssuesResponse {
  issues: SearchIssueResult[];
  total: number;
}

export interface SearchProjectResult extends Project {
  match_source: "title" | "description";
  matched_snippet?: string;
}

export interface SearchProjectsResponse {
  projects: SearchProjectResult[];
  total: number;
}

export interface UpdateMeRequest {
  name?: string;
  avatar_url?: string;
}

export interface StructuredTaskSpec {
  goal: string;
  audience: string[];
  output: string;
  constraints: string[];
  style: string[];
  open_questions: string[];
}

export interface ClarifyStructuredTaskRequest {
  original_input: string;
}

export interface StructuredTaskClarityCheckRequest extends StructuredTaskSpec {}

export interface StructuredTaskClarityCheckResponse {
  clarity_status: "clear" | "risky" | "blocked";
  reason: string[];
  suggestions: string[];
}

export interface StructuredTaskTemplate {
  id: string;
  workspace_id: string;
  template_name: string;
  description: string;
  goal: string;
  audience: string[];
  output: string;
  constraints: string[];
  style: string[];
  parameters: StructuredTaskTemplateParameter[];
  scope: "personal" | "team" | "org";
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface StructuredTaskTemplateParameter {
  key: string;
  description: string;
  required: boolean;
  default_value: string;
}

export interface CreateStructuredTaskTemplateRequest {
  template_name: string;
  description?: string;
  goal: string;
  audience: string[];
  output: string;
  constraints: string[];
  style: string[];
  parameters?: StructuredTaskTemplateParameter[];
  scope?: "personal" | "team" | "org";
}

export interface StructuredTaskHistoryItem {
  id: string;
  workspace_id: string;
  issue_id?: string | null;
  goal: string;
  used_template_id?: string | null;
  clarity_status: "clear" | "risky" | "blocked";
  spec: StructuredTaskSpec;
  created_by?: string | null;
  executed_at: string;
}

export interface CreateStructuredTaskHistoryRequest {
  issue_id?: string;
  goal: string;
  used_template_id?: string;
  clarity_status: "clear" | "risky" | "blocked";
  spec: StructuredTaskSpec;
}

export interface CreateMemberRequest {
  email: string;
  role?: MemberRole;
}

export interface UpdateMemberRequest {
  role: MemberRole;
}

// Personal Access Tokens
export interface PersonalAccessToken {
  id: string;
  name: string;
  token_prefix: string;
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
}

export interface CreatePersonalAccessTokenRequest {
  name: string;
  expires_in_days?: number;
}

export interface CreatePersonalAccessTokenResponse extends PersonalAccessToken {
  token: string;
}

// Pagination
export interface PaginationParams {
  limit?: number;
  offset?: number;
}

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

// User communication profile (JEH-304). Mirrors the user_profile DB row.
export interface UserProfileResponse {
  user_id: string;
  persona: "utalmodig" | "ekspert" | "grundig" | "larling";
  language: "da" | "en";
  length_pref: number;
  autonomy_pref: number;
  tech_pref: number;
  anti_patterns: string[];
  updated_at: string;
}

export interface UserProfileRequest {
  persona: "utalmodig" | "ekspert" | "grundig" | "larling";
  language: "da" | "en";
  length_pref: number;
  autonomy_pref: number;
  tech_pref: number;
  anti_patterns: string[];
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

export interface CreateRuntimeSetupTokenRequest {
  device_label?: string;
}

export interface CreateRuntimeSetupTokenResponse {
  token: string;
  expires_at: string;
  install_command: string;
  server_url: string;
  workspace_id: string;
  workspace_slug: string;
}

// Pagination
export interface PaginationParams {
  limit?: number;
  offset?: number;
}

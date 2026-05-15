// CEREBRO-PATCH(core-types-api): cerebro modification of upstream file
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
  is_private?: boolean;
  /** Attachment IDs to bind to this issue alongside the description update.
   *  Used by the description editor to register newly uploaded files so they
   *  surface in `issueAttachments` and keep their preview Eye on refresh. */
  attachment_ids?: string[];
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
  project_id?: string;
  open_only?: boolean;
}

/** Raw backend response shape for `GET /api/issues`. */
export interface ListIssuesResponse {
  issues: Issue[];
  total: number;
}

/** Per-status bucket in the paginated issue cache. `total` is the server count (all pages), not the length of `issues`. */
export interface IssueStatusBucket {
  issues: Issue[];
  total: number;
}

/**
 * Frontend cache shape for the issue list. Data is bucketed by status so
 * each column can paginate independently. Assembled from per-status
 * `api.listIssues` responses by the query functions in `issues/queries.ts`.
 */
export interface ListIssuesCache {
  byStatus: Partial<Record<IssueStatus, IssueStatusBucket>>;
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
  language?: string;
}

// CEREBRO-PATCH(user-profile-v2-types): JEH-1031 — replace tech_pref with 4
// per-axis scope ratings and add custom_prompt + prompt_mode escape hatch.
// User communication profile (JEH-304 / JEH-1031). Mirrors the user_profile
// DB row. The single tech_pref slider was replaced by four 1-5 scope ratings,
// plus an optional custom prompt with a mode flag for append vs replace.
export interface UserProfileResponse {
  user_id: string;
  persona: "utalmodig" | "ekspert" | "grundig" | "larling";
  language: "da" | "en";
  length_pref: number;
  autonomy_pref: number;
  git_pref: number;
  code_pref: number;
  computer_pref: number;
  process_pref: number;
  anti_patterns: string[];
  custom_prompt: string;
  prompt_mode: "append" | "replace";
  updated_at: string;
}

export interface UserProfileRequest {
  persona: "utalmodig" | "ekspert" | "grundig" | "larling";
  language: "da" | "en";
  length_pref: number;
  autonomy_pref: number;
  git_pref: number;
  code_pref: number;
  computer_pref: number;
  process_pref: number;
  anti_patterns: string[];
  custom_prompt: string;
  prompt_mode: "append" | "replace";
}

export interface CreateMemberRequest {
  email: string;
  role?: MemberRole;
}

export interface UpdateMemberRequest {
  role: MemberRole;
}

// Web Push (per-device subscription)
export interface PushSubscriptionResponse {
  id: string;
  endpoint: string;
  user_agent?: string | null;
  created_at: string;
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

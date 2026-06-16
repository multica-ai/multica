import type { Issue, IssueMetadata, IssueStatus, IssuePriority, IssueAssigneeType } from "./issue";
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
  start_date?: string;
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
  start_date?: string | null;
  due_date?: string | null;
  parent_issue_id?: string | null;
  project_id?: string | null;
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
  /**
   * Widen the assignee filter to issues where the user is the *indirect*
   * assignee — assignee is one of the user's owned agents, or a squad that
   * involves the user (human member / leader-via-owned-agent / agent member
   * owned by the user). Direct member assignment is intentionally excluded:
   * `involves_user_id` and `assignee_id=<user>` (tab "Assigned to me") produce
   * disjoint result sets by construction.
   */
  involves_user_id?: string;
  /** JSONB containment filter on `issue.metadata`. AND across keys. */
  metadata?: IssueMetadata;
  open_only?: boolean;
  /**
   * Restrict the result to issues with at least one of `start_date` /
   * `due_date` set. Used by the Project Gantt view so it doesn't have to
   * page through every issue on the project just to discard the unscheduled
   * majority on the client.
   */
  scheduled?: boolean;
  date_field?: "created_at" | "updated_at";
  date_start?: string;
  date_end?: string;
  sort_by?: "position" | "priority" | "title" | "created_at" | "start_date" | "due_date";
  sort_direction?: "asc" | "desc";
}

export interface IssueActorRef {
  type: IssueAssigneeType;
  id: string;
}

export interface ListGroupedIssuesParams {
  group_by: "assignee";
  limit?: number;
  offset?: number;
  workspace_id?: string;
  statuses?: IssueStatus[];
  priorities?: IssuePriority[];
  assignee_types?: IssueAssigneeType[];
  assignee_id?: string;
  assignee_ids?: string[];
  creator_id?: string;
  project_id?: string;
  /** See `ListIssuesParams.involves_user_id` — same semantics. */
  involves_user_id?: string;
  /** JSONB containment filter on `issue.metadata`. AND across keys. */
  metadata?: IssueMetadata;
  assignee_filters?: IssueActorRef[];
  include_no_assignee?: boolean;
  creator_filters?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  group_assignee_type?: IssueAssigneeType | "none";
  group_assignee_id?: string;
  date_field?: "created_at" | "updated_at";
  date_start?: string;
  date_end?: string;
  sort_by?: "position" | "priority" | "title" | "created_at" | "start_date" | "due_date";
  sort_direction?: "asc" | "desc";
}

/** Raw backend response shape for `GET /api/issues`. */
export interface ListIssuesResponse {
  issues: Issue[];
  total: number;
}

export interface IssueAssigneeGroup {
  id: string;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  issues: Issue[];
  total: number;
}

/** Raw backend response shape for `GET /api/issues/grouped?group_by=assignee`. */
export interface GroupedIssuesResponse {
  groups: IssueAssigneeGroup[];
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
  matched_description_snippet?: string;
  matched_comment_snippet?: string;
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
  /** Free-form self-description (max 2000 chars). Pass "" to clear. */
  profile_description?: string;
  /** IANA tz to pin; "" clears back to browser-tz; undefined leaves untouched. */
  timezone?: string;
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

import type { Issue, IssueMetadata, IssueStatus, IssueStatusCategory, IssuePriority, IssueAssigneeType } from "./issue";
import type { PropertyFilterValue } from "./property";
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
  /** Ordered stage (>= 1) grouping this sub-issue under its parent. */
  stage?: number;
  start_date?: string;
  due_date?: string;
  attachment_ids?: string[];
  /** Issue-scoped label IDs to attach in the same transaction as the create.
   *  Unknown or non-issue ids are rejected by the server with 400. */
  label_ids?: string[];
}

export interface CreateCommentSubIssueManualRequest {
  mode: "manual";
  capture_token: string;
  issue: CreateIssueRequest;
}

export interface CreateCommentSubIssueAgentRequest {
  mode: "agent";
  capture_token: string;
  quick_create: {
    agent_id?: string;
    squad_id?: string;
    prompt: string;
    priority?: IssuePriority;
    due_date?: string;
    project_id?: string | null;
    attachment_ids?: string[];
  };
}

export type CreateCommentSubIssueRequest =
  | CreateCommentSubIssueManualRequest
  | CreateCommentSubIssueAgentRequest;

export interface UpdateIssueRequest {
  /** Legacy aggregate compare-and-swap token. New text editors use field
   * baselines so unrelated issue activity does not reject their edits. */
  expected_revision?: number;
  title?: string;
  /** Authoritative title the editor adopted before producing this update. */
  title_base?: string;
  description?: string;
  /** Authoritative description the editor had adopted before producing this
   * update. The server uses it to merge channel media that landed meanwhile. */
  description_base?: string;
  status?: IssueStatus;
  priority?: IssuePriority;
  assignee_type?: IssueAssigneeType | null;
  assignee_id?: string | null;
  position?: number;
  start_date?: string | null;
  due_date?: string | null;
  parent_issue_id?: string | null;
  project_id?: string | null;
  /** Ordered stage (>= 1); null clears it (unstaged). */
  stage?: number | null;
  /** Attachment IDs to bind to this issue alongside the description update.
   *  Used by the description editor to register newly uploaded files so they
   *  surface in `issueAttachments` and keep their preview Eye on refresh. */
  attachment_ids?: string[];
  /** Skip starting the agent run this write would trigger ("暂时不启动",
   *  MUL-3375). The assignee/status change still applies. Control field —
   *  strip from optimistic cache patches; never written onto the Issue. */
  suppress_run?: boolean;
}

/**
 * Server-owned drag intent. The client may use a provisional position for its
 * optimistic animation, but the canonical position is derived from these
 * workspace-scoped neighbors by `POST /api/issues/:id/move`.
 */
export interface MoveIssueRequest
  extends Pick<
    UpdateIssueRequest,
    | "status"
    | "assignee_type"
    | "assignee_id"
    | "parent_issue_id"
    | "project_id"
  > {
  before_id: string | null;
  after_id: string | null;
}

/** Inputs to `POST /api/issues/preview-trigger`. A nil prospective field means
 *  "leave unchanged"; `isCreate` previews a not-yet-persisted issue. */
export interface IssueTriggerPreviewParams {
  issueIds?: string[];
  isCreate?: boolean;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
  status?: IssueStatus;
}

/** One issue that WILL start a run under the prospective write. `agent_id` is
 *  the runnable agent (squad leader for squads). */
export interface IssueTriggerPreviewItem {
  issue_id: string;
  agent_id: string;
  source: string;
}

export interface IssueTriggerPreview {
  triggers: IssueTriggerPreviewItem[];
  total_count: number;
}

export interface ListIssuesParams {
  limit?: number;
  offset?: number;
  workspace_id?: string;
  /** Flat-table quick search. Matches issue title words or an exact issue number. */
  q?: string;
  status?: IssueStatus;
  /** Multi-value table facet. OR within the field. */
  statuses?: IssueStatus[];
  /**
   * Filter by status CATEGORY rather than by exact key, so one bucket holds a
   * category's canonical status plus every custom status that inherits it.
   * This is what keeps the board's fan-out fixed at 7 requests however many
   * custom statuses a workspace defines. (MUL-6243)
   */
  status_category?: IssueStatusCategory;
  /** Multi-value form of `status_category`. OR within the field. */
  status_categories?: IssueStatusCategory[];
  priority?: IssuePriority;
  /** Multi-value table facet. OR within the field. */
  priorities?: IssuePriority[];
  assignee_id?: string;
  assignee_ids?: string[];
  /**
   * Narrow to issues assigned to the given actor kinds (member / agent /
   * squad). Same semantics as `ListGroupedIssuesParams.assignee_types` —
   * powers the workspace Members/Agents tabs server-side.
   */
  assignee_types?: IssueAssigneeType[];
  creator_id?: string;
  project_id?: string;
  /** Actor-aware table facets. OR within each field. */
  assignee_filters?: IssueActorRef[];
  include_no_assignee?: boolean;
  creator_filters?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  /** Restrict the window to root issues instead of filtering loaded pages. */
  top_level_only?: boolean;
  /**
   * Hard restriction of the window to the given issue ids (the table's
   * agents-working facet sends the live running-issue set). An EMPTY array is
   * meaningful and yields an EMPTY window — omit the field entirely for "no
   * restriction".
   */
  ids?: string[];
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
  /** Custom-property filter: definition id → accepted values (option ids or
   *  "true"/"false" for checkbox; a plain string is exact equality, an
   *  operator object narrows it). OR within a definition, AND across. */
  properties?: Record<string, PropertyFilterValue[]>;
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
  sort_by?:
    | "position"
    | "status"
    | "priority"
    | "title"
    | "created_at"
    | "updated_at"
    | "last_activity"
    | "start_date"
    | "due_date"
    | `property:${string}`;
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
  /** Custom-property filter: definition id → accepted values (option ids or
   *  "true"/"false" for checkbox; a plain string is exact equality, an
   *  operator object narrows it). OR within a definition, AND across. */
  properties?: Record<string, PropertyFilterValue[]>;
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
  sort_by?:
    | "position"
    | "status"
    | "priority"
    | "title"
    | "created_at"
    | "updated_at"
    | "last_activity"
    | "start_date"
    | "due_date"
    | `property:${string}`;
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

// Server-authoritative Table query contract. Membership, grouping and counts
// are evaluated against the complete result set; the browser only owns view
// state such as collapsed groups/parents.
export type IssueTableScope =
  | { kind: "workspace"; assignee_types?: IssueAssigneeType[] }
  | { kind: "project"; project_id: string; assignee_types?: IssueAssigneeType[] }
  | { kind: "assignee"; actor: IssueActorRef }
  | { kind: "creator"; actor: IssueActorRef }
  | { kind: "my"; relation: "assigned" | "created" | "involved" | "any" };

export interface IssueTableFilters {
  statuses?: IssueStatus[];
  priorities?: IssuePriority[];
  assignees?: IssueActorRef[];
  include_no_assignee?: boolean;
  creators?: IssueActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  /** Same shape as `ListIssuesParams.properties`: bare strings are exact
   *  equality / "No value", operator objects narrow scalar matches. */
  properties?: Record<string, PropertyFilterValue[]>;
  date?: {
    field: "created_at" | "updated_at";
    start: string;
    end: string;
  };
  working_only?: boolean;
  /** Match the running-task issue projection returned by
   *  `/api/working-agents`. An explicit empty list matches nothing. */
  working_issue_ids?: string[];
  include_sub_issues?: boolean;
}

export type IssueTableSortField =
  | "position"
  | "status"
  | "priority"
  | "title"
  | "created_at"
  | "updated_at"
  | "last_activity"
  | "start_date"
  | "due_date"
  | `property:${string}`;

export interface IssueTableQuerySpec {
  scope: IssueTableScope;
  filters: IssueTableFilters;
  search?: string;
  sort: {
    field: IssueTableSortField;
    direction: "asc" | "desc";
  };
}

export type IssueTableGroupSpec =
  | { kind: "none" }
  | { kind: "status" }
  /**
   * Group by the CATEGORY a status behaves as, not by the status key.
   *
   * Board columns, list sections and swimlane cells are categories, so a custom
   * status folds into the column it behaves as instead of getting one of its
   * own — which is what keeps the surface's fan-out pinned at 7 no matter how
   * many statuses a workspace defines. The descriptor still reports
   * `value.kind === "status"` because a category's value IS its canonical
   * status key; the group KEY is what distinguishes the two contracts.
   * (MUL-6243)
   */
  | { kind: "status_category" }
  | { kind: "assignee" }
  | { kind: "project" }
  | { kind: "parent" }
  | {
      kind: "compound";
      primary: "assignee" | "project" | "parent";
      /** `status_category` folds custom statuses into their category's cell. */
      secondary: "status" | "status_category";
      /** Optional visible secondary buckets. When present, the server pages
       * only primary groups that contain at least one matching card and
       * returns `total` for that complete visible result set. */
      secondary_values?: IssueStatus[] | IssueStatusCategory[];
    }
  | { kind: "property"; property_id: string; include_empty?: boolean };

/** Response-side actor reference. Kept open for forward compatibility: an
 * installed desktop client may receive a new actor kind from a newer server. */
export interface IssueTableActorRef {
  type: string;
  id: string;
}

export interface IssueTableParentRef {
  id: string;
  number: number;
  identifier: string;
  title: string;
  status: string;
}

export type IssueTableGroupValue =
  | { kind: "status"; status: string }
  | { kind: "assignee"; actor: IssueTableActorRef | null }
  | { kind: "project"; project_id: string | null }
  | {
      kind: "parent";
      parent_id: string | null;
      parent: IssueTableParentRef | null;
      value_state: "value" | "unavailable" | "unset";
    }
  | {
      kind: "property";
      property_id: string;
      value?: string | boolean | null;
      value_state: "value" | "unavailable" | "unset";
    };

export interface IssueTableGroupDescriptor {
  key: string;
  value: IssueTableGroupValue;
  count: number;
  /** Present for compound groups. These opaque keys can be passed straight
   * back to `/table/rows`; clients must not reconstruct them. */
  secondary_groups?: IssueTableGroupDescriptor[];
}

export interface IssueTablePageRequest {
  limit?: number;
  cursor?: string | null;
}

export interface IssueTableGroupsRequest {
  query: IssueTableQuerySpec;
  group: Exclude<IssueTableGroupSpec, { kind: "none" }>;
  page?: IssueTablePageRequest;
}

export interface IssueTableGroupsResponse {
  query_fingerprint: string;
  total: number;
  groups: IssueTableGroupDescriptor[];
  next_cursor: string | null;
}

export interface IssueTableRowsRequest {
  query: IssueTableQuerySpec;
  group: IssueTableGroupSpec;
  group_key: string | null;
  hierarchy: { enabled: boolean };
  parent_id: string | null;
  page?: IssueTablePageRequest;
}

export interface IssueTableRow {
  issue: Issue;
  direct_child_count: number;
}

export interface IssueTableRowsResponse {
  query_fingerprint: string;
  group_key: string | null;
  parent_id: string | null;
  total: number;
  rows: IssueTableRow[];
  branch_total: number;
  next_cursor: string | null;
}

export type IssueTableFacetSpec =
  | { kind: "status" }
  | { kind: "priority" }
  | { kind: "assignee" }
  | { kind: "creator" }
  | { kind: "project" }
  | { kind: "label" }
  | { kind: "property"; property_id: string }
  /** Agents running issue work inside this surface. `key` is the agent id,
   *  `count` its running-task count. Evaluated against the surface's own scope
   *  and filters, so the header chip counts the same rows the list shows. */
  | { kind: "working_agents" };

export interface IssueTableFacetsRequest {
  query: IssueTableQuerySpec;
  facets: IssueTableFacetSpec[];
  /** Existing callers default to true. Count-only UIs can skip the extra scan. */
  include_total?: boolean;
}

export interface IssueTableFacetValue {
  key: string;
  count: number;
}

export interface IssueTableFacet {
  kind: IssueTableFacetSpec["kind"];
  property_id?: string;
  values: IssueTableFacetValue[];
}

export interface IssueTableFacetsResponse {
  query_fingerprint: string;
  total: number;
  facets: IssueTableFacet[];
}

/** One agent running issue work inside a single issue surface. Projected from
 *  the `working_agents` facet, so the count is already narrowed by that
 *  surface's scope and every active filter. Name/avatar are resolved from the
 *  workspace agent directory, not carried here. */
export interface WorkingAgentSummary {
  id: string;
  running_task_count: number;
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
  /** Bucketed by status CATEGORY — see PAGINATED_CATEGORIES. (MUL-6243) */
  byStatus: Partial<Record<IssueStatusCategory, IssueStatusBucket>>;
}

export interface SearchIssueResult extends Issue {
  match_source: "title" | "description" | "comment";
  matched_snippet?: string;
  matched_description_snippet?: string;
  matched_comment_snippet?: string;
}

export interface SearchIssuesResponse {
  issues: SearchIssueResult[];
}

export interface SearchProjectResult extends Project {
  match_source: "title" | "description";
  matched_snippet?: string;
}

export interface SearchProjectsResponse {
  projects: SearchProjectResult[];
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

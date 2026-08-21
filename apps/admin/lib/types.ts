// Shared shapes for the admin dashboard. These mirror the prototype's mock
// `workspaces` array field-for-field (see multica-admin-dashboard.html)
// wherever a real column/derivation exists; fields with no backing data
// (see queries.ts/derive.ts comments) are typed optional and rendered as
// explicit empty states rather than invented.

export type WorkspaceStatus = "active" | "idle" | "error";
export type HealthScore = "good" | "warning" | "critical";

export interface WorkspaceListItem {
  id: string;
  name: string;
  slug: string;
  owner: string | null;
  model: string | null;
  llmKey: string | null;
  team: string | null;
  keySpend: number | null;
  status: WorkspaceStatus;
  openIssues: number;
  lastActivity: string | null; // ISO timestamp; formatted client-side
}

export interface ActivityEvent {
  type: "success" | "error" | "default";
  text: string;
  at: string; // ISO timestamp
}

export interface WorkspaceMetadata {
  id: string;
  slug: string;
  createdAt: string;
  owner: string | null;
  model: string | null;
  root: string | null;
  repoCount: number;
}

export interface IssueMetrics {
  openIssues: number;
  closedLast7d: number;
  avgResolutionHours: number | null;
  /** Issues whose effective status (see issue_effective_status in
   * queries.ts) is neither "todo" nor "backlog" — i.e. actively worked or
   * done. Denominator for the cost-per-ticket figure in LiteLlmSection. */
  activeIssueCount: number;
  dailyOpenCounts: Array<{ date: string; count: number }>;
  /** Open-issue counts grouped by label, per plan §2.2E ("severity breakdown
   * by label"). The schema has no dedicated severity field — issue_label is
   * a free-form name/color pair, so this groups by whatever labels exist
   * rather than inventing a severity taxonomy. */
  labelBreakdown: Array<{ name: string; color: string; count: number }>;
}

export interface DerivedInsights {
  successRate: number | null;
  health: HealthScore;
}

export interface LiteLlmSection {
  linked: boolean;
  keyAlias: string | null;
  teamAlias: string | null;
  keySpend: number | null;
  /** keySpend / issues.activeIssueCount. Null when there's no linked key or
   * no active tickets to divide by — never a fabricated 0. */
  costPerTicket: number | null;
}

export interface WorkspaceMember {
  id: string;
  name: string;
  email: string;
  role: "owner" | "admin" | "member";
}

export interface WorkspaceDetail {
  metadata: WorkspaceMetadata;
  status: WorkspaceStatus;
  activity: ActivityEvent[];
  issues: IssueMetrics;
  litellm: LiteLlmSection;
  members: WorkspaceMember[];
  insights: DerivedInsights;
}

export type SortColumn =
  | "status"
  | "name"
  | "owner"
  | "model"
  | "llmKey"
  | "team"
  | "keySpend"
  | "issues"
  | "activity";

/** Columns the SQL layer can actually order by (see SORT_COLUMN_SQL in
 * lib/queries.ts). `llmKey`/`team` are excluded on purpose: they're not DB
 * columns, they're display-only strings resolved via a LiteLLM lookup
 * merged into each page's rows *after* the paginated SQL query runs
 * (lib/litellm-join.ts), and there's no meaningful order for them anyway.
 * The UI must not offer a sort control for these, or it would show an
 * "active sort" arrow next to a column that's silently still ordered by
 * something else underneath (queries.ts's real fallback behavior).
 *
 * `keySpend` IS sortable despite the same "not a DB column" issue, because
 * unlike llmKey/team it has a real numeric order users want (cost list). It
 * takes a different path than SORT_COLUMN_SQL: the route handler
 * (app/api/workspaces/route.ts) fetches every matching workspace unpaged,
 * joins in LiteLLM spend, sorts in memory, then paginates — see that file's
 * `sort === "keySpend"` branch. */
export const SORTABLE_COLUMNS: SortColumn[] = [
  "status",
  "name",
  "owner",
  "model",
  "issues",
  "activity",
  "keySpend",
];

export type SortDirection = "asc" | "desc";

export interface ListWorkspacesParams {
  search: string;
  status: WorkspaceStatus | "all";
  sort: SortColumn;
  direction: SortDirection;
  page: number;
  pageSize: number;
  /** Plan §3.2: "Date range picker for 'Last Activity' filtering". Both are
   * inclusive "YYYY-MM-DD" date-only strings; either/both may be omitted. */
  activityFrom?: string;
  activityTo?: string;
}

export interface ListWorkspacesResult {
  items: WorkspaceListItem[];
  total: number;
  page: number;
  pageSize: number;
}

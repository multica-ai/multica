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
  /** Latest workspace update, activity-log event, or runtime heartbeat. */
  lastActive?: string | null;
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

/** A live (non-expired) `workspace_invitation` row — see
 * server/migrations/041_workspace_invitation.up.sql. Only 'admin'/'member'
 * are invitable roles; the Go backend rejects 'owner' invites outright. */
export interface PendingInvitation {
  email: string;
  role: "admin" | "member";
  createdAt: string;
  expiresAt: string;
}

/**
 * Whether the invite feature can actually write right now, computed
 * up front (LBYL, not "attempt the POST and interpret the 403") from data
 * this app already reads: is the bot account admin uses to call the Go API
 * (AGENTFARM_BOT_EMAIL) itself an owner/admin member of this workspace? See
 * app/api/workspaces/[id]/invitations/route.ts for the write path this
 * gates, and lib/queries.ts's getWorkspaceMembers for the source data.
 *
 * `reason` distinguishes *why* `eligible` is false — a deployment
 * misconfiguration ("pat-missing", BOT_PAT isn't set at all) reads very
 * differently to an operator than a per-workspace condition
 * ("not-workspace-admin", the bot account just needs to be added/promoted).
 */
export interface InviteEligibility {
  eligible: boolean;
  botEmail: string;
  reason: "pat-missing" | "not-workspace-admin" | null;
}

export interface WorkspaceDetail {
  metadata: WorkspaceMetadata;
  status: WorkspaceStatus;
  activity: ActivityEvent[];
  issues: IssueMetrics;
  litellm: LiteLlmSection;
  members: WorkspaceMember[];
  pendingInvitations: PendingInvitation[];
  inviteEligibility: InviteEligibility;
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

// ---------------------------------------------------------------------------
// Analytics page — global (cross-workspace) time-series.
// ---------------------------------------------------------------------------

/** Granularities offered in the UI. `168` = weekly, for the widest windows. */
export const GRANULARITY_HOURS = [1, 3, 6, 12, 24, 168] as const;
export type GranularityHours = (typeof GRANULARITY_HOURS)[number];

export interface AnalyticsParams {
  /** ISO timestamp, inclusive start of the window. */
  from: string;
  /** ISO timestamp, exclusive end of the window. */
  to: string;
  granularityHours: GranularityHours;
}

export interface AutopilotRunCounts {
  completed: number;
  failed: number;
  skipped: number;
  /** issue_created / running — still in flight when queried. The
   * 'pending' status was removed by 043_fix_orphaned_autopilot_runs; the
   * live CHECK constraint only allows issue_created/running/completed/
   * failed/skipped. */
  other: number;
}

/** Same 7 classes the per-workspace Errors tab uses (@multica/core/dashboard's
 * FAILURE_CLASSES) — kept as a plain string-keyed record here so this module
 * doesn't have to depend on that package's type for a shape this generic. */
export type ErrorClassCounts = Record<
  "auth" | "rate_limit" | "timeout" | "provider" | "runtime" | "agent" | "other",
  number
>;

export interface AnalyticsBucket {
  /** ISO timestamp — start of this bucket. */
  bucketStart: string;
  workspacesCreated: number;
  issuesCreated: number;
  autopilotRuns: AutopilotRunCounts;
  errors: ErrorClassCounts;
}

export interface AnalyticsResult {
  from: string;
  to: string;
  granularityHours: GranularityHours;
  buckets: AnalyticsBucket[];
  /**
   * Sum of `spend` across every LiteLLM key (lib/litellm.ts), i.e. lifetime
   * cost — not scoped to `from`/`to`. LiteLLM's /key/list only reports
   * cumulative spend per key, not a date-ranged figure, so this is
   * deliberately NOT a per-bucket time series (see the "cost" KPI card
   * rather than a chart). Null when LiteLLM isn't configured
   * (litellmConfigured() in lib/litellm.ts) — never fabricated as 0.
   */
  totalLiteLlmSpendUsd: number | null;
}

/** A clickable segment in one of the two drill-down analytics charts. */
export type AnalyticsBreakdownKind = "errors" | "autopilotRuns";

export interface AnalyticsWorkspaceBreakdownParams {
  /** Exact start/end of the clicked chart bucket. */
  from: string;
  to: string;
  kind: AnalyticsBreakdownKind;
  /** An error class for `errors`, or a run outcome for `autopilotRuns`. */
  segment: string;
}

/** One workspace's contribution to a selected analytics chart segment. */
export interface AnalyticsWorkspaceBreakdownItem {
  workspaceId: string;
  workspaceName: string;
  count: number;
}

export interface AnalyticsWorkspaceBreakdownResult {
  items: AnalyticsWorkspaceBreakdownItem[];
}

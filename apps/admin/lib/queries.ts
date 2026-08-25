import "server-only";
import { query } from "./db";
import type {
  ActivityEvent,
  AnalyticsBreakdownKind,
  AnalyticsParams,
  AutopilotRunCounts,
  IssueMetrics,
  ListWorkspacesParams,
  ListWorkspacesResult,
  PendingInvitation,
  WorkspaceListItem,
  WorkspaceMember,
  WorkspaceMetadata,
  WorkspaceStatus,
} from "./types";

// All queries here are SELECT-only against agentfarm's shared schema (see
// lib/db.ts). Column provenance, one by one, since several prototype fields
// (multica-admin-dashboard.html's mock `workspaces` array) have no 1:1 DB
// column and are either derived or explicitly unavailable:
//
//   workspace.slug/name/context/repos/created_at/updated_at  -- real columns
//   owner        -- member.role = 'owner' joined to "user"
//   model        -- agent.model (050_agent_model.up.sql), most-recent agent
//   status       -- derived: agent.status='error' > agent_runtime online/
//                   recently-seen > idle (see deriveStatus in derive.ts;
//                   duplicated here as SQL so it can drive filter/sort)
//   openIssues   -- issue.status not in ('done','cancelled')
//   lastActivity -- MAX(activity_log.created_at) for the workspace
//   root         -- NOT STORED anywhere in the DB. The prototype invents a
//                   filesystem path; real workspaces don't have one on the
//                   server (it's daemon/local-machine state). Rendered as an
//                   explicit "Not reported" empty state, never fabricated.
//   repoCount    -- workspace.repos.length (JSONB array of RepoData; see
//                   server/internal/handler/agent.go's RepoData struct) — a
//                   workspace can have more than one connected repo, so only
//                   the count is shown, not a single URL
//   llmKey/team  -- NOT a DB column; resolved via a LiteLLM lookup merged in
//                   the route handler (see app/api/workspaces/[id]/route.ts
//                   and the join-strategy note in the design plan)

// Maps each entry in SORT_COLUMN_SQL's SortColumn arguments to its SQL
// expression. Only ever called with a column from SORTABLE_COLUMNS
// (lib/types.ts) — the API route validates against that list before this
// runs, so every real caller has a real entry here. See that list's doc
// comment for why llmKey/team are excluded rather than mapped to a fallback.
const SORT_COLUMN_SQL: Record<string, string> = {
  status: "derived_status",
  name: "w.name",
  owner: "owner_name",
  model: "model",
  issues: "open_issues",
  activity: "last_activity",
};

// Hard backstop for listWorkspaces' unpaged mode (used to sort by keySpend,
// which is resolved post-query — see SORTABLE_COLUMNS' doc comment in
// lib/types.ts), mirroring MAX_KEY_PAGES/MAX_TEAM_PAGES in lib/litellm.ts.
const MAX_UNPAGED_ROWS = 5000;

const STATUS_CASE = `
  CASE
    WHEN EXISTS (
      SELECT 1 FROM agent a2 WHERE a2.workspace_id = w.id AND a2.status = 'error'
    ) THEN 'error'
    WHEN EXISTS (
      SELECT 1 FROM agent_runtime ar2
      WHERE ar2.workspace_id = w.id
        AND (ar2.status = 'online' OR ar2.last_seen_at > now() - interval '1 hour')
    ) THEN 'active'
    ELSE 'idle'
  END
`;

export async function listWorkspaces(
  params: ListWorkspacesParams,
  opts: { unpaged?: boolean } = {},
): Promise<ListWorkspacesResult> {
  const { search, status, sort, direction, page, pageSize, activityFrom, activityTo } = params;

  const whereClauses: string[] = [];
  const values: unknown[] = [];

  if (search.trim()) {
    values.push(`%${search.trim()}%`);
    const idx = values.length;
    whereClauses.push(
      `(w.name ILIKE $${idx} OR owner.name ILIKE $${idx} OR model.model ILIKE $${idx})`,
    );
  }
  if (status !== "all") {
    values.push(status);
    whereClauses.push(`(${STATUS_CASE}) = $${values.length}`);
  }
  // Plan §3.2: date-range filter on "Last Activity" — the same
  // GREATEST(w.updated_at, activity.last_at) expression used in the SELECT
  // below, filtered against inclusive date-only bounds. Applied as a HAVING-
  // style clause via the same lateral join, so it can't be expressed in the
  // WHERE list above (those only see columns on w/owner/model at this point);
  // pushed into the query below via placeholder markers instead.
  if (activityFrom) {
    values.push(activityFrom);
    whereClauses.push(`GREATEST(w.updated_at, activity.last_at) >= $${values.length}::date`);
  }
  if (activityTo) {
    values.push(activityTo);
    // Inclusive of the whole end day: < (date + 1 day).
    whereClauses.push(`GREATEST(w.updated_at, activity.last_at) < ($${values.length}::date + interval '1 day')`);
  }
  const where = whereClauses.length ? `WHERE ${whereClauses.join(" AND ")}` : "";

  const sortColumn = SORT_COLUMN_SQL[sort] ?? "last_activity";
  const sortDir = direction === "asc" ? "ASC" : "DESC";

  // Unpaged mode (sort by keySpend, see SORTABLE_COLUMNS' doc comment):
  // fetch every matching row up to a hard backstop instead of the requested
  // page, so the caller can join in LiteLLM spend and sort in memory before
  // paginating.
  values.push(opts.unpaged ? MAX_UNPAGED_ROWS : pageSize, opts.unpaged ? 0 : (page - 1) * pageSize);
  const limitIdx = values.length - 1;
  const offsetIdx = values.length;

  const rows = await query<{
    id: string;
    name: string;
    slug: string;
    owner_name: string | null;
    model: string | null;
    open_issues: string;
    last_activity: string | null;
    derived_status: WorkspaceStatus;
    total: string;
  }>(
    `
    SELECT
      w.id, w.name, w.slug,
      owner.name AS owner_name,
      model.model,
      COALESCE(issues.open_count, 0) AS open_issues,
      GREATEST(w.updated_at, activity.last_at) AS last_activity,
      ${STATUS_CASE} AS derived_status,
      count(*) OVER () AS total
    FROM workspace w
    LEFT JOIN LATERAL (
      SELECT u.name FROM member m JOIN "user" u ON u.id = m.user_id
      WHERE m.workspace_id = w.id AND m.role = 'owner' LIMIT 1
    ) owner ON true
    LEFT JOIN LATERAL (
      SELECT a.model FROM agent a
      WHERE a.workspace_id = w.id AND a.model IS NOT NULL
      ORDER BY a.created_at DESC LIMIT 1
    ) model ON true
    LEFT JOIN LATERAL (
      SELECT count(*) AS open_count FROM issue i
      WHERE i.workspace_id = w.id AND i.status NOT IN ('done', 'cancelled')
    ) issues ON true
    LEFT JOIN LATERAL (
      SELECT max(al.created_at) AS last_at FROM activity_log al
      WHERE al.workspace_id = w.id
    ) activity ON true
    ${where}
    ORDER BY ${sortColumn} ${sortDir} NULLS LAST
    LIMIT $${limitIdx} OFFSET $${offsetIdx}
    `,
    values,
  );

  const total = rows[0] ? Number(rows[0].total) : 0;
  const items: WorkspaceListItem[] = rows.map((r) => ({
    id: r.id,
    name: r.name,
    slug: r.slug,
    owner: r.owner_name,
    model: r.model,
    llmKey: null, // resolved by the caller via lib/litellm.ts, per join strategy
    team: null,
    keySpend: null,
    status: r.derived_status,
    openIssues: Number(r.open_issues),
    lastActivity: r.last_activity,
  }));

  return { items, total, page, pageSize };
}

export async function getWorkspaceMetadata(id: string): Promise<WorkspaceMetadata | null> {
  const rows = await query<{
    id: string;
    slug: string;
    created_at: string;
    owner_name: string | null;
    model: string | null;
    repos: unknown;
  }>(
    `
    SELECT
      w.id, w.slug, w.created_at,
      owner.name AS owner_name,
      model.model,
      w.repos
    FROM workspace w
    LEFT JOIN LATERAL (
      SELECT u.name FROM member m JOIN "user" u ON u.id = m.user_id
      WHERE m.workspace_id = w.id AND m.role = 'owner' LIMIT 1
    ) owner ON true
    LEFT JOIN LATERAL (
      SELECT a.model FROM agent a
      WHERE a.workspace_id = w.id AND a.model IS NOT NULL
      ORDER BY a.created_at DESC LIMIT 1
    ) model ON true
    WHERE w.id = $1
    `,
    [id],
  );
  const row = rows[0];
  if (!row) return null;

  const repoCount = Array.isArray(row.repos) ? row.repos.length : 0;

  return {
    id: row.id,
    slug: row.slug,
    createdAt: row.created_at,
    owner: row.owner_name,
    model: row.model,
    root: null, // not stored server-side — see provenance note above
    repoCount,
  };
}

export async function getWorkspaceStatus(id: string): Promise<WorkspaceStatus> {
  const rows = await query<{ status: WorkspaceStatus }>(
    `SELECT (${STATUS_CASE}) AS status FROM workspace w WHERE w.id = $1`,
    [id],
  );
  return rows[0]?.status ?? "idle";
}

/**
 * Recent activity_log rows for a workspace, most-recent first. `type` is
 * derived from the free-text `action` column via simple keyword matching
 * (documented, not fabricated) rather than a stored enum — activity_log has
 * no severity/type column today. Default limit is 50: the panel (per plan
 * §2.2B) shows only the first 10 with a "View all" expansion, so this fetches
 * enough real rows up front to expand into without a second round trip.
 */
export async function getRecentActivity(id: string, limit = 50): Promise<ActivityEvent[]> {
  const rows = await query<{ action: string; created_at: string }>(
    `
    SELECT action, created_at FROM activity_log
    WHERE workspace_id = $1
    ORDER BY created_at DESC
    LIMIT $2
    `,
    [id, limit],
  );
  return rows.map((r) => {
    const a = r.action.toLowerCase();
    let type: ActivityEvent["type"] = "default";
    if (/fail|error|timeout/.test(a)) type = "error";
    else if (/complet|resolv|success|deploy/.test(a)) type = "success";
    return { type, text: r.action, at: r.created_at };
  });
}

/**
 * Issue metrics for the detail panel. `dailyOpenCounts` is a real 14-day
 * trend of issues created per day (grouped from the `issue` table) — this
 * replaces the prototype's hardcoded/random bar-height array, per
 * DESIGN.md's "no invented metrics" anti-pattern.
 */
export async function getIssueMetrics(id: string): Promise<IssueMetrics> {
  const [counts] = await query<{
    open_issues: string;
    closed_7d: string;
    avg_resolution_hours: string | null;
    active_issue_count: string;
  }>(
    // issue_effective_status resolves custom per-workspace statuses to their
    // canonical category (see 340_issue_effective_status_fn.up.sql) before
    // every comparison below — a raw status string match would misclassify
    // a workspace's custom statuses (e.g. a custom "done"-category status
    // would be counted as still open).
    `
    SELECT
      count(*) FILTER (
        WHERE issue_effective_status(workspace_id, status) NOT IN ('done', 'cancelled')
      ) AS open_issues,
      count(*) FILTER (
        WHERE issue_effective_status(workspace_id, status) = 'done'
          AND updated_at > now() - interval '7 days'
      ) AS closed_7d,
      avg(EXTRACT(EPOCH FROM (updated_at - created_at)) / 3600.0) FILTER (
        WHERE issue_effective_status(workspace_id, status) = 'done'
          AND updated_at > now() - interval '30 days'
      ) AS avg_resolution_hours,
      count(*) FILTER (
        WHERE issue_effective_status(workspace_id, status) NOT IN ('todo', 'backlog')
      ) AS active_issue_count
    FROM issue
    WHERE workspace_id = $1
    `,
    [id],
  );

  const trend = await query<{ day: string; count: string }>(
    `
    SELECT date_trunc('day', created_at)::date::text AS day, count(*) AS count
    FROM issue
    WHERE workspace_id = $1 AND created_at > now() - interval '14 days'
    GROUP BY 1
    ORDER BY 1
    `,
    [id],
  );

  // Plan §2.2E: open-issue count "with severity breakdown by label". No
  // severity column exists (see IssueMetrics comment in lib/types.ts) — this
  // groups currently-open issues by their real issue_label rows instead.
  const labels = await query<{ name: string; color: string; count: string }>(
    `
    SELECT il.name, il.color, count(*) AS count
    FROM issue i
    JOIN issue_to_label itl ON itl.issue_id = i.id
    JOIN issue_label il ON il.id = itl.label_id
    WHERE i.workspace_id = $1
      AND issue_effective_status(i.workspace_id, i.status) NOT IN ('done', 'cancelled')
    GROUP BY il.name, il.color
    ORDER BY count DESC
    `,
    [id],
  );

  return {
    openIssues: Number(counts?.open_issues ?? 0),
    closedLast7d: Number(counts?.closed_7d ?? 0),
    avgResolutionHours: counts?.avg_resolution_hours
      ? Math.round(Number(counts.avg_resolution_hours) * 10) / 10
      : null,
    activeIssueCount: Number(counts?.active_issue_count ?? 0),
    dailyOpenCounts: trend.map((t) => ({ date: t.day, count: Number(t.count) })),
    labelBreakdown: labels.map((l) => ({ name: l.name, color: l.color, count: Number(l.count) })),
  };
}

/** Real agentfarm membership for a workspace (member + user join), ordered
 * by join date. This is the actual member roster — distinct from LiteLLM's
 * key/team data, which carries no member/username information at all. */
export async function getWorkspaceMembers(id: string): Promise<WorkspaceMember[]> {
  const rows = await query<{ id: string; name: string; email: string; role: string }>(
    `
    SELECT u.id, u.name, u.email, m.role
    FROM member m
    JOIN "user" u ON u.id = m.user_id
    WHERE m.workspace_id = $1
    ORDER BY m.created_at ASC
    `,
    [id],
  );
  return rows.map((r) => ({
    id: r.id,
    name: r.name,
    email: r.email,
    role: r.role as WorkspaceMember["role"],
  }));
}

/**
 * Live (pending, not-yet-expired) invitations for a workspace — used both to
 * render the "pending" state in MembersSection and, more importantly, as an
 * LBYL pre-check: the invite dialog and route handler read this before
 * attempting a write, so "already invited" is caught up front instead of via
 * the Go API's 409. `status = 'pending' AND expires_at > now()` mirrors the
 * partial unique index (idx_invitation_unique_pending) the Go backend itself
 * enforces — see server/migrations/041_workspace_invitation.up.sql.
 */
export async function getPendingInvitations(id: string): Promise<PendingInvitation[]> {
  const rows = await query<{
    invitee_email: string;
    role: string;
    created_at: string;
    expires_at: string;
  }>(
    `
    SELECT invitee_email, role, created_at, expires_at
    FROM workspace_invitation
    WHERE workspace_id = $1 AND status = 'pending' AND expires_at > now()
    ORDER BY created_at DESC
    `,
    [id],
  );
  return rows.map((r) => ({
    email: r.invitee_email,
    role: r.role as PendingInvitation["role"],
    createdAt: r.created_at,
    expiresAt: r.expires_at,
  }));
}

/** Completed vs. failed agent_task_queue rows in the last 30 days, for
 * deriveSuccessRate() in lib/derive.ts. */
export async function getTaskOutcomeCounts(
  workspaceId: string,
): Promise<{ completed: number; failed: number }> {
  const rows = await query<{ status: string; n: string }>(
    `
    SELECT atq.status, count(*) AS n
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    WHERE a.workspace_id = $1
      AND atq.status IN ('completed', 'failed')
      AND atq.created_at > now() - interval '30 days'
    GROUP BY atq.status
    `,
    [workspaceId],
  );
  let completed = 0;
  let failed = 0;
  for (const r of rows) {
    if (r.status === "completed") completed = Number(r.n);
    else if (r.status === "failed") failed = Number(r.n);
  }
  return { completed, failed };
}

/**
 * Global (cross-workspace) time series for the Analytics page. Errors keep
 * raw `failure_reason` strings — folding those into the 7 display classes
 * (auth/rate_limit/timeout/provider/runtime/agent/other) happens in the
 * route handler via `failureClassOf` from @multica/core/dashboard, not here,
 * so this module has no dependency beyond the DB itself.
 */
export interface AnalyticsRawBucket {
  bucketStart: string;
  workspacesCreated: number;
  issuesCreated: number;
  autopilotRuns: AutopilotRunCounts;
  errorsByReason: Record<string, number>;
}

/**
 * Unfolded workspace rows for one selected analytics bucket. Error rows keep
 * their raw reason here so the route can apply the shared failure taxonomy
 * before selecting the requested segment.
 */
export interface AnalyticsWorkspaceBreakdownRawRow {
  workspaceId: string;
  workspaceName: string;
  segment: string;
  count: number;
}

export async function getAnalyticsWorkspaceBreakdown(
  params: Pick<AnalyticsParams, "from" | "to"> & { kind: AnalyticsBreakdownKind },
): Promise<AnalyticsWorkspaceBreakdownRawRow[]> {
  const { from, to, kind } = params;

  if (kind === "autopilotRuns") {
    return query<{ workspace_id: string; workspace_name: string; status: string; n: string }>(
      `
      SELECT ap.workspace_id, w.name AS workspace_name, ar.status, count(*) AS n
      FROM autopilot_run ar
      JOIN autopilot ap ON ap.id = ar.autopilot_id
      JOIN workspace w ON w.id = ap.workspace_id
      WHERE ar.triggered_at >= $1::timestamptz AND ar.triggered_at < $2::timestamptz
      GROUP BY 1, 2, 3
      `,
      [from, to],
    ).then((rows) => rows.map((row) => ({
      workspaceId: row.workspace_id,
      workspaceName: row.workspace_name,
      segment: row.status,
      count: Number(row.n),
    })));
  }

  return query<{ workspace_id: string; workspace_name: string; failure_reason: string; n: string }>(
    `
    SELECT a.workspace_id,
           w.name AS workspace_name,
           COALESCE(NULLIF(atq.failure_reason, ''), 'unclassified') AS failure_reason,
           count(*) AS n
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    JOIN workspace w ON w.id = a.workspace_id
    WHERE atq.status = 'failed'
      AND atq.completed_at >= $1::timestamptz AND atq.completed_at < $2::timestamptz
    GROUP BY 1, 2, 3
    `,
    [from, to],
  ).then((rows) => rows.map((row) => ({
    workspaceId: row.workspace_id,
    workspaceName: row.workspace_name,
    segment: row.failure_reason,
    count: Number(row.n),
  })));
}

// Every bucketing expression below is anchored at `from` ($1) rather than a
// calendar boundary, so a granularity that doesn't evenly divide 24h (e.g.
// 3h against a window starting mid-day) still buckets identically across
// every metric query and the generate_series spine — they all evaluate this
// exact expression against the exact same $1/$3. Output as an ISO-8601
// string (not left as timestamptz) so every query's bucket column is a
// stable, directly-comparable map key regardless of the pg driver's
// timestamptz-to-JS-Date parsing.
function bucketStartIsoExpr(column: string): string {
  return `to_char(
      ($1::timestamptz + floor(extract(epoch from (${column} - $1::timestamptz)) / ($3::int * 3600))
        * ($3::int * interval '1 hour')) AT TIME ZONE 'UTC',
      'YYYY-MM-DD"T"HH24:MI:SS"Z"'
    )`;
}

export async function getAnalyticsTimeSeries(params: AnalyticsParams): Promise<AnalyticsRawBucket[]> {
  const { from, to, granularityHours } = params;
  const values = [from, to, granularityHours];

  const [spine, workspaceRows, issueRows, autopilotRows, errorRows] = await Promise.all([
    // Zero-fills empty buckets rather than letting them gap. generate_series
    // is end-inclusive, so a window whose length is an exact multiple of the
    // granularity would otherwise emit a trailing zero-width bucket at `to`
    // — excluded via `gs < to`, same bound every metric query uses.
    query<{ bucket_start: string }>(
      `
      SELECT to_char(gs AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS bucket_start
      FROM generate_series($1::timestamptz, $2::timestamptz, ($3 || ' hours')::interval) AS gs
      WHERE gs < $2::timestamptz
      ORDER BY 1
      `,
      values,
    ),
    query<{ bucket_start: string; n: string }>(
      `
      SELECT ${bucketStartIsoExpr("created_at")} AS bucket_start, count(*) AS n
      FROM workspace
      WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz
      GROUP BY 1
      `,
      values,
    ),
    query<{ bucket_start: string; n: string }>(
      `
      SELECT ${bucketStartIsoExpr("created_at")} AS bucket_start, count(*) AS n
      FROM issue
      WHERE created_at >= $1::timestamptz AND created_at < $2::timestamptz
      GROUP BY 1
      `,
      values,
    ),
    query<{ bucket_start: string; status: string; n: string }>(
      `
      SELECT ${bucketStartIsoExpr("triggered_at")} AS bucket_start, status, count(*) AS n
      FROM autopilot_run
      WHERE triggered_at >= $1::timestamptz AND triggered_at < $2::timestamptz
      GROUP BY 1, 2
      `,
      values,
    ),
    // Mirrors ListDashboardFailuresDaily (server/pkg/db/queries/task_usage.sql)
    // with the workspace_id filter dropped: terminal failed tasks, bucketed
    // on completed_at (not started_at — a task can fail before starting).
    // Deliberately NOT unioned with autopilot_run.status='failed': an
    // autopilot run that fails because its task failed already has a row
    // here (taskFailureReasonForAutopilotRun in server/internal/service/
    // autopilot.go copies the task's own failure_reason onto the run), so
    // adding autopilot_run's failures too would double-count. Autopilot
    // failures that never reached a task surface in the autopilot-runs
    // chart's own "failed" segment instead.
    query<{ bucket_start: string; failure_reason: string; n: string }>(
      `
      SELECT ${bucketStartIsoExpr("completed_at")} AS bucket_start,
             COALESCE(NULLIF(failure_reason, ''), 'unclassified') AS failure_reason,
             count(*) AS n
      FROM agent_task_queue
      WHERE status = 'failed'
        AND completed_at >= $1::timestamptz AND completed_at < $2::timestamptz
      GROUP BY 1, 2
      `,
      values,
    ),
  ]);

  const buckets = new Map<string, AnalyticsRawBucket>();
  const bucketFor = (bucketStart: string): AnalyticsRawBucket => {
    let b = buckets.get(bucketStart);
    if (!b) {
      b = {
        bucketStart,
        workspacesCreated: 0,
        issuesCreated: 0,
        autopilotRuns: { completed: 0, failed: 0, skipped: 0, other: 0 },
        errorsByReason: {},
      };
      buckets.set(bucketStart, b);
    }
    return b;
  };

  for (const row of spine) bucketFor(row.bucket_start);
  for (const row of workspaceRows) bucketFor(row.bucket_start).workspacesCreated = Number(row.n);
  for (const row of issueRows) bucketFor(row.bucket_start).issuesCreated = Number(row.n);
  for (const row of autopilotRows) {
    const b = bucketFor(row.bucket_start);
    const n = Number(row.n);
    // Live CHECK constraint (043_fix_orphaned_autopilot_runs +
    // 079_autopilot_run_skipped_status) allows exactly these five statuses;
    // issue_created/running are still in flight when queried, so they fold
    // into `other` rather than getting their own chart segments.
    if (row.status === "completed") b.autopilotRuns.completed = n;
    else if (row.status === "failed") b.autopilotRuns.failed = n;
    else if (row.status === "skipped") b.autopilotRuns.skipped = n;
    else b.autopilotRuns.other += n;
  }
  for (const row of errorRows) {
    const b = bucketFor(row.bucket_start);
    b.errorsByReason[row.failure_reason] = (b.errorsByReason[row.failure_reason] ?? 0) + Number(row.n);
  }

  return Array.from(buckets.values()).sort((a, b) => a.bucketStart.localeCompare(b.bucketStart));
}

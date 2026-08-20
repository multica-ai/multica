import "server-only";
import { query } from "./db";
import type {
  ActivityEvent,
  IssueMetrics,
  ListWorkspacesParams,
  ListWorkspacesResult,
  WorkspaceListItem,
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
//   gitRemote    -- workspace.repos[0].url (JSONB array of RepoData; see
//                   server/internal/handler/agent.go's RepoData struct)
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

  let gitRemote: string | null = null;
  if (Array.isArray(row.repos) && row.repos.length > 0) {
    const first = row.repos[0] as { url?: unknown };
    if (typeof first?.url === "string") gitRemote = first.url;
  }

  return {
    id: row.id,
    slug: row.slug,
    createdAt: row.created_at,
    owner: row.owner_name,
    model: row.model,
    root: null, // not stored server-side — see provenance note above
    gitRemote,
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
  }>(
    `
    SELECT
      count(*) FILTER (WHERE status NOT IN ('done', 'cancelled')) AS open_issues,
      count(*) FILTER (WHERE status = 'done' AND updated_at > now() - interval '7 days') AS closed_7d,
      avg(EXTRACT(EPOCH FROM (updated_at - created_at)) / 3600.0)
        FILTER (WHERE status = 'done' AND updated_at > now() - interval '30 days') AS avg_resolution_hours
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
    WHERE i.workspace_id = $1 AND i.status NOT IN ('done', 'cancelled')
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
    dailyOpenCounts: trend.map((t) => ({ date: t.day, count: Number(t.count) })),
    labelBreakdown: labels.map((l) => ({ name: l.name, color: l.color, count: Number(l.count) })),
  };
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

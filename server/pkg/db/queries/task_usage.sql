-- name: UpsertTaskUsage :exec
-- Bumps `updated_at` on INSERT and on conflict so the hourly-rollup worker
-- detects the row as dirty and re-aggregates its bucket.
-- Without the conflict-side bump, a correction to historical token counts
-- would never propagate to the rollup.
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (task_id, provider, model)
DO UPDATE SET
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    cache_write_tokens = EXCLUDED.cache_write_tokens,
    updated_at = now();

-- name: UpsertLocalCLIUsage :exec
INSERT INTO local_cli_usage (
    run_id,
    workspace_id,
    issue_id,
    owner_id,
    cli_name,
    provider,
    model,
    input_tokens,
    output_tokens,
    cache_read_tokens,
    cache_write_tokens,
    updated_at
)
SELECT
    lcr.id,
    lcr.workspace_id,
    lcr.issue_id,
    lcr.owner_id,
    lcr.cli_name,
    sqlc.arg('provider'),
    sqlc.arg('model'),
    sqlc.arg('input_tokens'),
    sqlc.arg('output_tokens'),
    sqlc.arg('cache_read_tokens'),
    sqlc.arg('cache_write_tokens'),
    now()
FROM local_cli_run lcr
WHERE lcr.id = sqlc.arg('run_id')
ON CONFLICT (run_id, provider, model)
DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    issue_id = EXCLUDED.issue_id,
    owner_id = EXCLUDED.owner_id,
    cli_name = EXCLUDED.cli_name,
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    cache_write_tokens = EXCLUDED.cache_write_tokens,
    updated_at = now();

-- name: GetTaskUsage :many
SELECT * FROM task_usage
WHERE task_id = $1
ORDER BY model;

-- name: GetIssueUsageSummary :one
WITH usage_rows AS (
    SELECT
        'task'::text AS source,
        tu.task_id AS run_id,
        tu.input_tokens,
        tu.output_tokens,
        tu.cache_read_tokens,
        tu.cache_write_tokens
    FROM task_usage tu
    JOIN agent_task_queue atq ON atq.id = tu.task_id
    WHERE atq.issue_id = $1

    UNION ALL

    SELECT
        'local'::text AS source,
        lcu.run_id,
        lcu.input_tokens,
        lcu.output_tokens,
        lcu.cache_read_tokens,
        lcu.cache_write_tokens
    FROM local_cli_usage lcu
    JOIN local_cli_run lcr ON lcr.id = lcu.run_id
    WHERE lcr.issue_id = $1
)
SELECT
    COALESCE(SUM(input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT source || ':' || run_id::text)::int AS task_count
FROM usage_rows;

-- name: ListDashboardUsageDaily :many
-- Daily per-(date, model) token aggregates for the workspace, served
-- from the UTC-bucketed `task_usage_hourly` table and
-- sliced to calendar days under the caller-supplied @tz. Optionally
-- scoped to a single project via sqlc.narg('project_id'). Powers the
-- workspace dashboard's daily cost chart.
-- The viewer's tz is applied here at query time, so a viewer in
-- Asia/Shanghai gets their "today" cut at +08 and one in
-- America/Los_Angeles gets theirs at -08 against the same UTC rows.
--
-- @since is already the viewer's local start-of-day-(N) as a UTC
-- instant (computed by parseSinceParamInTZ). It must NOT be re-truncated
-- with DATE_TRUNC here — DATE_TRUNC operates in the session tz and would
-- snap the cutoff back to UTC midnight, dragging in an extra partial
-- local day for any non-UTC viewer.
SELECT
    DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) AS date,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text), model
ORDER BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) DESC, model;

-- name: ListDashboardUsageByAgent :many
-- Per-(agent, model) token aggregates from `task_usage_hourly`. No
-- date grouping in the result, so this query takes no `@tz` — the
-- @since cutoff is a raw timestamptz the Go layer has already computed
-- in the viewer's tz. Model dimension is preserved so the client can
-- compute cost from its per-model pricing table; the client folds rows
-- by agent for the "by agent" list on the dashboard.
--
-- task_count is summed across hourly buckets — one task that spans
-- multiple hours lands in multiple buckets, so this over-counts by
-- hour the same way the daily version over-counted by day. The
-- frontend prefers `ListDashboardAgentRunTime` for the user-facing
-- "tasks" column, so this stays informational only.
SELECT
    agent_id,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY agent_id, model
ORDER BY agent_id, model;

-- name: ListDashboardRunTimeDaily :many
-- Daily per-date run time + task counts for the workspace, optionally
-- scoped to a single project. Powers the workspace dashboard's "Time"
-- and "Tasks" metrics on the same toggle as Tokens / Cost. Bucketed by
-- completed_at (terminal time) sliced into calendar days under the
-- caller-supplied @tz — same Viewing-tz treatment as ListDashboardUsageDaily
-- so the Time / Tasks tabs cut their day boundary identically to the
-- Cost / Tokens tabs (a viewer east of UTC would otherwise see the four
-- tabs disagree on a "1d" window). Only terminal tasks (completed or
-- failed) with both started_at and completed_at populated contribute.
--
-- @since is already the viewer's local start-of-day-(N) (parseSinceParamInTZ)
-- — passed straight through, NOT re-truncated; see ListDashboardUsageDaily.
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text)
ORDER BY DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) DESC;

-- name: ListDashboardAgentRunTime :many
-- Per-agent total task run time and task count for the workspace, optionally
-- scoped to a single project. Counts only terminal runs (completed or failed)
-- with both started_at and completed_at populated — queued/running tasks have
-- no finite duration. Anchored on completed_at so the window matches the
-- token cost window (which is anchored on tu.created_at, ~= completion time).
--
-- No date bucketing, so no @tz — but @since is the viewer's local
-- start-of-day-(N) so the "last N days" window lines up with the per-agent
-- cost card; passed straight through without re-truncation.
SELECT
    atq.agent_id,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id
ORDER BY total_seconds DESC;

-- name: ListDashboardLocalUsageDaily :many
SELECT
    DATE(lcu.updated_at) AS date,
    lcu.model,
    SUM(lcu.input_tokens)::bigint AS input_tokens,
    SUM(lcu.output_tokens)::bigint AS output_tokens,
    SUM(lcu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(lcu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT lcu.run_id)::int AS task_count
FROM local_cli_usage lcu
LEFT JOIN issue i ON i.id = lcu.issue_id
WHERE lcu.workspace_id = $1
  AND lcu.updated_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY DATE(lcu.updated_at), lcu.model
ORDER BY DATE(lcu.updated_at) DESC, lcu.model;

-- name: ListDashboardLocalUsageByRunner :many
SELECT
    lcu.owner_id,
    (COALESCE(NULLIF(u.name, ''), u.email) || '-local-' || lcu.cli_name)::text AS runner_name,
    lcu.cli_name,
    lcu.provider,
    lcu.model,
    SUM(lcu.input_tokens)::bigint AS input_tokens,
    SUM(lcu.output_tokens)::bigint AS output_tokens,
    SUM(lcu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(lcu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT lcu.run_id)::int AS task_count
FROM local_cli_usage lcu
JOIN "user" u ON u.id = lcu.owner_id
LEFT JOIN issue i ON i.id = lcu.issue_id
WHERE lcu.workspace_id = $1
  AND lcu.updated_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY lcu.owner_id, runner_name, lcu.cli_name, lcu.provider, lcu.model
ORDER BY runner_name, lcu.model;

-- name: ListDashboardLocalRunTimeByRunner :many
SELECT
    lcr.owner_id,
    (COALESCE(NULLIF(u.name, ''), u.email) || '-local-' || lcr.cli_name)::text AS runner_name,
    lcr.cli_name,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (lcr.completed_at - lcr.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE lcr.status = 'failed')::int AS failed_count
FROM local_cli_run lcr
JOIN "user" u ON u.id = lcr.owner_id
LEFT JOIN issue i ON i.id = lcr.issue_id
WHERE lcr.workspace_id = $1
  AND lcr.status IN ('completed', 'failed', 'cancelled')
  AND lcr.started_at IS NOT NULL
  AND lcr.completed_at IS NOT NULL
  AND lcr.completed_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY lcr.owner_id, runner_name, lcr.cli_name
ORDER BY runner_name;

-- name: ListDashboardLocalRunTimeDaily :many
SELECT
    DATE(lcr.completed_at) AS date,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (lcr.completed_at - lcr.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE lcr.status = 'failed')::int AS failed_count
FROM local_cli_run lcr
LEFT JOIN issue i ON i.id = lcr.issue_id
WHERE lcr.workspace_id = $1
  AND lcr.status IN ('completed', 'failed', 'cancelled')
  AND lcr.started_at IS NOT NULL
  AND lcr.completed_at IS NOT NULL
  AND lcr.completed_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY DATE(lcr.completed_at)
ORDER BY DATE(lcr.completed_at) DESC;

-- name: UpsertTaskUsage :exec
-- Bumps `updated_at` on INSERT and on conflict so the daily-rollup worker
-- (migration 073) detects the row as dirty and re-aggregates its bucket.
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

-- name: GetTaskUsage :many
SELECT * FROM task_usage
WHERE task_id = $1
ORDER BY model;

-- name: GetWorkspaceUsageByDay :many
-- Bucket by tu.created_at (usage report time, ~= task completion time), not
-- atq.created_at (task enqueue time), so tasks that queue one day and execute
-- the next are attributed to the day tokens were actually produced. The since
-- cutoff is truncated to start-of-day so `days=N` yields full calendar days.
SELECT
    DATE(tu.created_at) AS date,
    tu.model,
    SUM(tu.input_tokens)::bigint AS total_input_tokens,
    SUM(tu.output_tokens)::bigint AS total_output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS total_cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
GROUP BY DATE(tu.created_at), tu.model
ORDER BY DATE(tu.created_at) DESC, tu.model;

-- name: GetWorkspaceUsageSummary :many
-- Filter by tu.created_at (usage report time), aligned to start-of-day, so
-- `days=N` is interpreted as N full calendar days like the other usage queries.
SELECT
    tu.model,
    SUM(tu.input_tokens)::bigint AS total_input_tokens,
    SUM(tu.output_tokens)::bigint AS total_output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS total_cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN agent a ON a.id = atq.agent_id
WHERE a.workspace_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
GROUP BY tu.model
ORDER BY (SUM(tu.input_tokens) + SUM(tu.output_tokens)) DESC;

-- name: GetIssueUsageSummary :one
SELECT
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id = $1;

-- name: GetIssueUsageByModel :many
-- Per-model token totals for tasks belonging to this issue. Used by the
-- handler to compute cost via pkg/pricing — the same authoritative table
-- used for budget enforcement, so the sidebar number matches what the
-- workspace was actually charged.
SELECT
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id = $1
GROUP BY tu.model;

-- name: GetIssueSubtreeUsageByModel :many
-- Per-model token totals across the issue and all descendants (recursive).
-- The subtree CTE walks parent_issue_id at every level so deeply nested
-- sub-issues roll up into their root. Cost is summed per model in Go and
-- returned alongside the "this issue only" total.
WITH RECURSIVE subtree AS (
    SELECT issue.id FROM issue WHERE issue.id = $1
    UNION ALL
    SELECT i.id
    FROM issue i
    JOIN subtree s ON i.parent_issue_id = s.id
)
SELECT
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id IN (SELECT id FROM subtree)
GROUP BY tu.model;

-- name: GetChatSessionUsageByModel :many
-- Per-model token totals for tasks belonging to this chat session. Used by
-- the chat session header to expose session price + token breakdown.
SELECT
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.chat_session_id = $1
GROUP BY tu.model;

-- name: ListDashboardUsageDaily :many
SELECT
    DATE(tu.created_at) AS date,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY DATE(tu.created_at), tu.model
ORDER BY DATE(tu.created_at) DESC, tu.model;

-- name: ListDashboardUsageByAgent :many
SELECT
    atq.agent_id,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id, tu.model
ORDER BY atq.agent_id, tu.model;

-- name: ListDashboardUsageDailyRollup :many
SELECT
    bucket_date AS date,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_dashboard_daily
WHERE workspace_id = $1
  AND bucket_date >= DATE_TRUNC('day', @since::timestamptz)::date
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY bucket_date, model
ORDER BY bucket_date DESC, model;

-- name: ListDashboardUsageByAgentRollup :many
SELECT
    agent_id,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_dashboard_daily
WHERE workspace_id = $1
  AND bucket_date >= DATE_TRUNC('day', @since::timestamptz)::date
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY agent_id, model
ORDER BY agent_id, model;

-- name: ListDashboardAgentRunTime :many
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
  AND atq.completed_at >= DATE_TRUNC('day', @since::timestamptz)
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id
ORDER BY total_seconds DESC;

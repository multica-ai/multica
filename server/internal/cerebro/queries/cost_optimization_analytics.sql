-- name: AnalyticsCostOptimizationBySaving :many
-- Per-saving rollup for today / 7d / 30d windows (FIR-2765 analytics tab).
SELECT
    saving_key,
    metric,
    COUNT(*) FILTER (WHERE created_at >= date_trunc('day', now() AT TIME ZONE 'UTC')) AS runs_today,
    COALESCE(SUM(baseline_value - effective_value) FILTER (WHERE created_at >= date_trunc('day', now() AT TIME ZONE 'UTC')), 0)::BIGINT AS saved_units_today,
    COALESCE(SUM(saved_cents) FILTER (WHERE created_at >= date_trunc('day', now() AT TIME ZONE 'UTC')), 0)::BIGINT AS saved_cents_today,
    COUNT(*) FILTER (WHERE created_at >= now() - interval '7 days') AS runs_7d,
    COALESCE(SUM(baseline_value - effective_value) FILTER (WHERE created_at >= now() - interval '7 days'), 0)::BIGINT AS saved_units_7d,
    COALESCE(SUM(saved_cents) FILTER (WHERE created_at >= now() - interval '7 days'), 0)::BIGINT AS saved_cents_7d,
    COUNT(*) FILTER (WHERE created_at >= now() - interval '30 days') AS runs_30d,
    COALESCE(SUM(baseline_value - effective_value) FILTER (WHERE created_at >= now() - interval '30 days'), 0)::BIGINT AS saved_units_30d,
    COALESCE(SUM(saved_cents) FILTER (WHERE created_at >= now() - interval '30 days'), 0)::BIGINT AS saved_cents_30d
FROM cerebro_cost_optimization_measurement
WHERE workspace_id = sqlc.arg(workspace_id)
  AND created_at >= now() - interval '30 days'
GROUP BY saving_key, metric
ORDER BY saving_key;

-- name: AnalyticsCostOptimizationTopIssues :many
-- Top issues by saved units for one saving in a time window.
SELECT
    i.id AS issue_id,
    i.number AS issue_number,
    i.title AS issue_title,
    COUNT(*)::BIGINT AS run_count,
    COALESCE(SUM(m.baseline_value - m.effective_value), 0)::BIGINT AS saved_units,
    COALESCE(SUM(m.saved_cents), 0)::BIGINT AS saved_cents
FROM cerebro_cost_optimization_measurement m
JOIN agent_task_queue atq ON atq.id = m.task_id
JOIN issue i ON i.id = atq.issue_id
WHERE m.workspace_id = sqlc.arg(workspace_id)
  AND m.saving_key = sqlc.arg(saving_key)
  AND m.created_at >= sqlc.arg(since)
GROUP BY i.id, i.number, i.title
ORDER BY saved_units DESC, run_count DESC
LIMIT sqlc.arg(row_limit);

-- name: AnalyticsCostOptimizationIssueRuns :many
-- Per-run measurements for one issue + saving.
SELECT
    m.task_id,
    m.created_at,
    m.mode,
    m.applied,
    m.held_out,
    m.metric,
    m.baseline_value,
    m.effective_value,
    (m.baseline_value - m.effective_value)::BIGINT AS saved_units,
    m.saved_cents,
    m.detail_json
FROM cerebro_cost_optimization_measurement m
JOIN agent_task_queue atq ON atq.id = m.task_id
WHERE m.workspace_id = sqlc.arg(workspace_id)
  AND m.saving_key = sqlc.arg(saving_key)
  AND atq.issue_id = sqlc.arg(issue_id)
  AND m.created_at >= sqlc.arg(since)
ORDER BY m.created_at DESC
LIMIT sqlc.arg(row_limit);

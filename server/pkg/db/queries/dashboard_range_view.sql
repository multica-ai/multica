-- name: RecordDashboardRangeView :exec
-- One dashboard read. outcome is within (inside the limit, or no limit),
-- observed (past the limit, admitted under a configured observe), degraded
-- (past the limit, admitted because a stale enforce was downgraded) or
-- truncated (past the limit, cut to it under enforce).
--
-- The row is selected from its workspace under FOR KEY SHARE, which conflicts
-- with the FOR UPDATE workspace deletion takes first. A record that runs after
-- the deletion committed finds no workspace and inserts nothing; one that runs
-- first makes the deletion wait and is then removed by it.
INSERT INTO dashboard_range_view (
    workspace_id, day, requested_days, returned_days,
    view_count, observed_over_limit_count, degraded_over_limit_count, truncated_count,
    last_limit_days, last_action, last_reason,
    first_viewed_at, last_viewed_at
)
SELECT
    w.id, (now() AT TIME ZONE 'UTC')::date, @requested_days, @returned_days,
    1,
    CASE WHEN @outcome::text = 'observed' THEN 1 ELSE 0 END,
    CASE WHEN @outcome::text = 'degraded' THEN 1 ELSE 0 END,
    CASE WHEN @outcome::text = 'truncated' THEN 1 ELSE 0 END,
    sqlc.narg('limit_days'), @action, @reason,
    now(), now()
FROM workspace w
WHERE w.id = @workspace_id
FOR KEY SHARE OF w
ON CONFLICT (workspace_id, day, requested_days, returned_days) DO UPDATE
SET view_count = dashboard_range_view.view_count + 1,
    observed_over_limit_count = dashboard_range_view.observed_over_limit_count + EXCLUDED.observed_over_limit_count,
    degraded_over_limit_count = dashboard_range_view.degraded_over_limit_count + EXCLUDED.degraded_over_limit_count,
    truncated_count = dashboard_range_view.truncated_count + EXCLUDED.truncated_count,
    last_limit_days = EXCLUDED.last_limit_days,
    last_action = EXCLUDED.last_action,
    last_reason = EXCLUDED.last_reason,
    last_viewed_at = now(),
    updated_at = now();

-- name: ListWorkspaceDashboardRangeViews :many
-- Every recorded day of one workspace, newest first.
SELECT * FROM dashboard_range_view
WHERE workspace_id = @workspace_id
ORDER BY day DESC, requested_days, returned_days;

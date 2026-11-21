-- name: UpsertRuntimeRateLimit :exec
-- One snapshot per runtime (MUL-9427). Concurrent runs on the same runtime
-- each report their own view; the newest observation wins, so a late report
-- from an older run cannot roll the snapshot back.
INSERT INTO runtime_rate_limit (runtime_id, workspace_id, snapshot, observed_at)
VALUES (@runtime_id, @workspace_id, @snapshot, @observed_at)
ON CONFLICT (runtime_id) DO UPDATE
SET snapshot = EXCLUDED.snapshot,
    workspace_id = EXCLUDED.workspace_id,
    observed_at = EXCLUDED.observed_at,
    updated_at = now()
WHERE runtime_rate_limit.observed_at <= EXCLUDED.observed_at;

-- name: GetRuntimeRateLimit :one
SELECT runtime_id, workspace_id, snapshot, observed_at, updated_at
FROM runtime_rate_limit
WHERE runtime_id = $1;

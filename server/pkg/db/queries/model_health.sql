-- name: GetModelHealth :one
SELECT * FROM model_health WHERE workspace_id IS NOT DISTINCT FROM sqlc.narg('workspace_id')::uuid AND concrete = sqlc.arg('concrete');

-- name: UpsertModelHealthUnhealthy :one
INSERT INTO model_health (workspace_id, concrete, status, reason, consecutive_failures, last_failure_reason, last_failure_at, updated_at)
VALUES (sqlc.narg('workspace_id')::uuid, sqlc.arg('concrete'), 'unhealthy', sqlc.narg('reason')::text, 1, sqlc.narg('reason')::text, now(), now())
ON CONFLICT (workspace_id, concrete) DO UPDATE SET status = 'unhealthy', reason = EXCLUDED.reason, consecutive_failures = model_health.consecutive_failures + 1, last_failure_reason = EXCLUDED.reason, last_failure_at = now(), updated_at = now()
RETURNING *;

-- name: UpsertModelHealthPricingUnhealthy :one
-- Pricing breach: mark the model unhealthy in a way that does NOT auto-recover
-- via the model_health TTL. isModelHealthyWithQueries treats an 'unhealthy' row
-- as healthy again once time.Since(last_failure_at) > modelHealthTTL (10m). By
-- pushing last_failure_at far into the future, the breach stays sticky until the
-- price recovers and MarkModelHealthy is called.
INSERT INTO model_health (workspace_id, concrete, status, reason, consecutive_failures, last_failure_reason, last_failure_at, updated_at)
VALUES (sqlc.narg('workspace_id')::uuid, sqlc.arg('concrete'), 'unhealthy', 'pricing', 1, 'pricing', now() + interval '365 days', now())
ON CONFLICT (workspace_id, concrete) DO UPDATE SET status = 'unhealthy', reason = 'pricing', consecutive_failures = model_health.consecutive_failures + 1, last_failure_reason = 'pricing', last_failure_at = now() + interval '365 days', updated_at = now()
RETURNING *;

-- name: MarkModelHealthy :exec
INSERT INTO model_health (workspace_id, concrete, status, reason, consecutive_failures, last_success_at, updated_at)
VALUES (sqlc.narg('workspace_id')::uuid, sqlc.arg('concrete'), 'healthy', NULL, 0, now(), now())
ON CONFLICT (workspace_id, concrete) DO UPDATE SET status = 'healthy', reason = NULL, consecutive_failures = 0, last_success_at = now(), updated_at = now();

-- name: ListModelHealth :many
SELECT * FROM model_health WHERE workspace_id IS NOT DISTINCT FROM sqlc.narg('workspace_id')::uuid;

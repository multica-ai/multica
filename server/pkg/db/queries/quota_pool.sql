-- name: GetProviderQuotaPool :one
SELECT * FROM provider_quota_pool WHERE id = $1;

-- name: GetProviderQuotaPoolForUpdate :one
SELECT * FROM provider_quota_pool WHERE id = $1 FOR UPDATE;

-- name: ListProviderQuotaPoolsByOwner :many
SELECT * FROM provider_quota_pool WHERE owner_id = $1 ORDER BY name, id;

-- name: GetProviderQuotaPoolForAgent :one
SELECT p.* FROM provider_quota_pool p
JOIN provider_quota_pool_agent m ON m.pool_id = p.id
WHERE m.agent_id = $1;

-- name: GetProviderQuotaPoolForAgentForShare :one
-- StartTask uses this in the same transaction as the task transition. Holding
-- a shared row lock serializes task start against a concurrent pool hold.
SELECT p.* FROM provider_quota_pool p
JOIN provider_quota_pool_agent m ON m.pool_id = p.id
WHERE m.agent_id = $1
FOR SHARE OF p;

-- name: GetProviderQuotaPoolForAgentForUpdate :one
-- Failure finalization locks the same pool row that task start reads FOR SHARE.
SELECT p.* FROM provider_quota_pool p
JOIN provider_quota_pool_agent m ON m.pool_id = p.id
WHERE m.agent_id = $1
FOR UPDATE OF p;

-- name: CreateProviderQuotaPool :one
INSERT INTO provider_quota_pool (id, owner_id, name, provider_hint, timezone)
VALUES (@id, @owner_id, @name, @provider_hint, @timezone)
RETURNING *;

-- name: AssignAgentProviderQuotaPool :exec
INSERT INTO provider_quota_pool_agent (agent_id, pool_id)
VALUES (@agent_id, @pool_id)
ON CONFLICT (agent_id) DO UPDATE SET pool_id = EXCLUDED.pool_id;

-- name: RemoveAgentProviderQuotaPool :exec
DELETE FROM provider_quota_pool_agent WHERE agent_id = $1;

-- name: ListProviderQuotaPoolAgents :many
SELECT agent_id FROM provider_quota_pool_agent WHERE pool_id = $1 ORDER BY agent_id;

-- name: ListProviderQuotaPoolRuntimeIDs :many
SELECT DISTINCT a.runtime_id FROM provider_quota_pool_agent m
JOIN agent a ON a.id = m.agent_id
WHERE m.pool_id = $1 AND a.runtime_id IS NOT NULL AND a.archived_at IS NULL;

-- name: SetProviderQuotaPoolState :one
UPDATE provider_quota_pool
SET state = @state,
    reset_at = sqlc.narg(reset_at),
    reset_date = sqlc.narg(reset_date),
    source_task_id = sqlc.narg(source_task_id),
    observed_at = sqlc.narg(observed_at),
    probe_started_at = sqlc.narg(probe_started_at),
    probe_task_id = sqlc.narg(probe_task_id),
    probe_attempts = @probe_attempts,
    revision = revision + 1,
    updated_at = now()
WHERE id = @id AND revision = @expected_revision
RETURNING *;

-- name: CreateProviderQuotaPoolEvent :exec
INSERT INTO provider_quota_pool_event
    (id, pool_id, actor_id, source_task_id, event_type, reason, old_state, new_state)
VALUES (@id, @pool_id, sqlc.narg(actor_id), sqlc.narg(source_task_id),
        @event_type, @reason, @old_state, @new_state);

-- name: ListProviderQuotaPoolEvents :many
SELECT * FROM provider_quota_pool_event
WHERE pool_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- name: ListDueProviderQuotaPools :many
SELECT * FROM provider_quota_pool
WHERE (state IN ('held_exact', 'probe_backoff') AND reset_at <= now())
   OR (state = 'held_date' AND reset_date <= (now() AT TIME ZONE timezone)::date)
ORDER BY COALESCE(reset_at, reset_date::timestamptz), id
LIMIT $1;

-- name: ListOrphanedProviderQuotaProbes :many
SELECT p.* FROM provider_quota_pool p
LEFT JOIN agent_task_queue t ON t.id = p.probe_task_id
WHERE p.state = 'probing'
  AND (t.id IS NULL OR t.status IN ('completed', 'failed', 'cancelled'))
ORDER BY p.probe_started_at, p.id
LIMIT $1;

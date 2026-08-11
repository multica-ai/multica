-- =====================
-- Durable compensation / saga queries. Owner: ALL-16.
-- blocked and dead_letter are NEVER auto-retried; only manual unlock.
-- =====================

-- name: InsertCompensationIfAbsent :one
-- Idempotent insert: a duplicate idempotency_key is a no-op that returns the
-- existing row. Used before any remote side effect so a crash after the
-- remote call can be reconciled on restart.
INSERT INTO memoryhub_compensation (
    workspace_id, binding_id, op, idempotency_key, remote_ref
) VALUES (
    $1, sqlc.narg(binding_id), $2, $3, $4
)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: ClaimDueCompensations :many
-- Worker scan: FOR UPDATE SKIP LOCKED plus lease. Only pending/retry_wait.
UPDATE memoryhub_compensation
SET state = 'running',
    attempt = attempt + 1,
    lease_owner = $1,
    lease_expires_at = now() + $2::interval,
    updated_at = now()
WHERE id IN (
    SELECT id FROM memoryhub_compensation
    WHERE state IN ('pending', 'retry_wait')
      AND (next_attempt_at IS NULL OR next_attempt_at <= now())
    ORDER BY next_attempt_at ASC NULLS FIRST, created_at ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkCompensated :one
UPDATE memoryhub_compensation
SET state = 'compensated',
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1 AND state = 'running'
RETURNING *;

-- name: MarkRetryWait :one
-- Exponential backoff 1s -> 2 -> 4 -> 8 -> 16 -> 32, capped at max_attempt.
UPDATE memoryhub_compensation
SET state = 'retry_wait',
    next_attempt_at = now() + make_interval(secs => LEAST(2 ^ (attempt - 1), 32)),
    lease_owner = NULL,
    lease_expires_at = NULL,
    last_error = $2,
    updated_at = now()
WHERE id = $1 AND state = 'running' AND attempt < max_attempt
RETURNING *;

-- name: MarkBlocked :one
-- Terminal for automatic retry; only manual unlock may move it.
UPDATE memoryhub_compensation
SET state = 'blocked',
    lease_owner = NULL,
    lease_expires_at = NULL,
    last_error = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkDeadLetter :one
UPDATE memoryhub_compensation
SET state = 'dead_letter',
    lease_owner = NULL,
    lease_expires_at = NULL,
    last_error = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ReleaseExpiredCompensationLeases :exec
UPDATE memoryhub_compensation
SET lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE lease_owner IS NOT NULL
  AND lease_expires_at < now();

-- name: CreateDaemonClaimAttempt :one
INSERT INTO daemon_claim_attempt (
    id, daemon_id, principal_key, request_fingerprint,
    runtime_ids, max_tasks, status, expires_at
) VALUES (
    $1, $2, $3, $4,
    $5, $6, 'processing', now() + make_interval(secs => @expires_in_seconds::double precision)
)
ON CONFLICT (id) DO NOTHING
RETURNING *;

-- name: GetDaemonClaimAttemptForUpdate :one
SELECT * FROM daemon_claim_attempt
WHERE id = $1
FOR UPDATE;

-- name: ListDaemonClaimAttemptTasks :many
SELECT * FROM agent_task_queue
WHERE claim_attempt_id = $1
  AND claim_attempt_ordinal IS NOT NULL
  AND status = 'dispatched'
  AND started_at IS NULL
ORDER BY claim_attempt_ordinal;

-- name: MarkDaemonClaimAttemptReady :one
UPDATE daemon_claim_attempt
SET status = 'ready',
    ready_at = now(),
    updated_at = now()
WHERE id = $1
  AND status = 'processing'
RETURNING *;

-- name: AcknowledgeDaemonClaimAttempt :one
UPDATE daemon_claim_attempt
SET status = 'acknowledged',
    acknowledged_at = COALESCE(acknowledged_at, now()),
    updated_at = now()
WHERE id = $1
  AND daemon_id = $2
  AND principal_key = $3
  AND status IN ('ready', 'acknowledged')
RETURNING *;

-- name: AcknowledgeDaemonClaimAttemptForTask :execrows
UPDATE daemon_claim_attempt AS attempt
SET status = 'acknowledged',
    acknowledged_at = COALESCE(attempt.acknowledged_at, now()),
    updated_at = now()
FROM agent_task_queue AS task
WHERE task.id = $1
  AND task.claim_attempt_id = attempt.id
  AND attempt.status = 'ready';

-- name: ExpireDaemonClaimAttempts :execrows
UPDATE daemon_claim_attempt
SET status = 'expired',
    updated_at = now()
WHERE status IN ('processing', 'ready')
  AND expires_at <= now();

-- name: DeleteOldDaemonClaimAttempts :execrows
DELETE FROM daemon_claim_attempt
WHERE (status = 'acknowledged' AND updated_at < now() - interval '10 minutes')
   OR (status = 'expired' AND updated_at < now() - interval '24 hours');

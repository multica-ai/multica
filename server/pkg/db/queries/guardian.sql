-- =====================
-- Guardian persistent state queries. Owner: ALL-16.
-- Guardian state transitions are CAS + lease; no raw queue writes from here.
-- =====================

-- name: UpsertGuardianState :one
INSERT INTO guardian_state (
    execution_id, workspace_id, state, fingerprint
) VALUES (
    $1, $2, 'pending', $3
)
ON CONFLICT (execution_id) DO NOTHING
RETURNING *;

-- name: GetGuardianStateByExecution :one
SELECT * FROM guardian_state WHERE execution_id = $1;

-- name: ClaimDueGuardianStates :many
-- Scheduler scan: FOR UPDATE SKIP LOCKED plus lease on due pending /
-- retry_wait / handoff_pending rows.
UPDATE guardian_state
SET lease_owner = $1,
    lease_expires_at = now() + $2::interval,
    updated_at = now()
WHERE id IN (
    SELECT id FROM guardian_state
    WHERE state IN ('pending', 'retry_wait', 'handoff_pending')
      AND (next_wakeup IS NULL OR next_wakeup <= now())
    ORDER BY next_wakeup ASC NULLS FIRST, created_at ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: UpdateGuardianStateCAS :one
UPDATE guardian_state
SET state = $2,
    version = version + 1,
    lease_owner = NULL,
    lease_expires_at = NULL,
    next_wakeup = sqlc.narg(next_wakeup),
    evidence_ref = sqlc.narg(evidence_ref),
    failure_code = sqlc.narg(failure_code),
    updated_at = now()
WHERE id = $1 AND state = $3 AND version = $4
RETURNING *;

-- name: RecordGuardianHandoffCAS :one
-- handoff: record candidate score, target agent/runtime, and handoff_of.
UPDATE guardian_state
SET state = 'handoff_pending',
    version = version + 1,
    handoff_of = sqlc.narg(handoff_of)::uuid,
    candidate_agent_id = sqlc.narg(candidate_agent_id)::uuid,
    candidate_runtime_id = sqlc.narg(candidate_runtime_id)::uuid,
    score = sqlc.narg(score),
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = @id AND state = @old_state AND version = @version
RETURNING *;

-- name: BlockGuardianStateCAS :one
UPDATE guardian_state
SET state = 'blocked',
    version = version + 1,
    lease_owner = NULL,
    lease_expires_at = NULL,
    next_wakeup = NULL,
    failure_code = sqlc.narg(failure_code),
    updated_at = now()
WHERE id = @id AND state = @old_state AND version = @version
RETURNING *;

-- name: ReleaseExpiredGuardianLeases :exec
UPDATE guardian_state
SET lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE lease_owner IS NOT NULL
  AND lease_expires_at < now();

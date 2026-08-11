-- =====================
-- ExecutionLedger queries. Owner: ALL-16.
-- Queue and ledger writes happen in ONE transaction through
-- TaskService.runInTx; events/broadcasts/wakeups are post-commit.
-- =====================

-- name: InsertExecutionLedger :one
INSERT INTO execution_ledger (
    execution_id, attempt, task_id, task_version, workspace_id, project_id,
    scope_kind, issue_id, run_id, agent_id, runtime_id, model,
    state, origin, idempotency_key, retry_of, rerun_of, delegated_from,
    handoff_of, review_policy, reviewer_agent_id
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg(project_id),
    $6, sqlc.narg(issue_id), $7, $8, $9, $10,
    'queued', $11, $12, sqlc.narg(retry_of), sqlc.narg(rerun_of), sqlc.narg(delegated_from),
    sqlc.narg(handoff_of), $13, sqlc.narg(reviewer_agent_id)
)
RETURNING *;

-- name: GetExecutionLedgerByExecutionID :one
SELECT * FROM execution_ledger WHERE execution_id = $1;

-- name: GetExecutionLedgerByTaskID :one
SELECT * FROM execution_ledger WHERE task_id = $1;

-- name: GetExecutionLedgerByIDempotencyKey :one
SELECT * FROM execution_ledger WHERE idempotency_key = $1;

-- name: ClaimExecutionLedgerCAS :one
-- Claim the ledger row: queued -> claimed with lease + optimistic version.
-- Zero rows => the double-claim rejection path.
UPDATE execution_ledger
SET state = 'claimed',
    lease_owner = $2,
    lease_expires_at = now() + $3::interval,
    version = version + 1,
    started_at = COALESCE(started_at, now())
WHERE execution_id = $1
  AND state = 'queued'
  AND version = $4
RETURNING *;

-- name: StartExecutionLedgerRunningCAS :one
UPDATE execution_ledger
SET state = 'running',
    lease_owner = NULL,
    lease_expires_at = NULL,
    version = version + 1,
    started_at = COALESCE(started_at, now())
WHERE execution_id = $1
  AND state = 'claimed'
  AND version = $2
RETURNING *;

-- name: CompleteExecutionLedgerCAS :one
UPDATE execution_ledger
SET state = 'completed',
    version = version + 1,
    finished_at = now(),
    stop_reason = sqlc.narg(stop_reason),
    lease_owner = NULL,
    lease_expires_at = NULL
WHERE execution_id = $1
  AND state IN ('claimed', 'running')
  AND version = $2
RETURNING *;

-- name: FailExecutionLedgerCAS :one
UPDATE execution_ledger
SET state = 'failed',
    version = version + 1,
    finished_at = now(),
    stop_reason = $2,
    lease_owner = NULL,
    lease_expires_at = NULL
WHERE execution_id = $1
  AND state IN ('claimed', 'running')
  AND version = $3
RETURNING *;

-- name: CancelExecutionLedgerCAS :one
UPDATE execution_ledger
SET state = 'cancelled',
    version = version + 1,
    finished_at = now(),
    stop_reason = sqlc.narg(stop_reason),
    lease_owner = NULL,
    lease_expires_at = NULL
WHERE execution_id = $1
  AND state IN ('queued', 'claimed', 'running')
  AND version = $2
RETURNING *;

-- name: MarkExecutionLedgerDeadLetter :one
UPDATE execution_ledger
SET state = 'dead_letter',
    version = version + 1,
    finished_at = now(),
    stop_reason = $2,
    lease_owner = NULL,
    lease_expires_at = NULL
WHERE execution_id = $1
  AND version = $3
RETURNING *;

-- name: AppendExecutionLedgerMergedLineage :one
-- P3 comment merge: preserve execution_id/run_id, append deduplicated
-- comment id to lineage.merged_from, bump task_version.
UPDATE execution_ledger
SET task_version = task_version + 1,
    updated_at = now()
WHERE execution_id = $1
  AND NOT ($2::uuid = ANY(merged_from))
RETURNING *;

-- name: ListExecutionLedgerByWorkspaceRun :many
SELECT * FROM execution_ledger
WHERE workspace_id = $1 AND run_id = $2
ORDER BY attempt ASC;

-- name: ReleaseExpiredExecutionLedgerLeases :exec
UPDATE execution_ledger
SET lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE lease_owner IS NOT NULL
  AND lease_expires_at < now();

-- name: GetWorkspaceClaimIntakeControl :one
SELECT *
FROM workspace_claim_intake_control
WHERE workspace_id = @workspace_id;

-- name: LockWorkspaceClaimIntakeControlsForClaim :many
-- Claims hold a shared row lock through the transaction that creates or refreshes
-- ownership. Pause/resume takes FOR UPDATE on the same row, making acknowledgement
-- and post-fence ownership exhaustive orderings.
SELECT *
FROM workspace_claim_intake_control
WHERE workspace_id = ANY(@workspace_ids::uuid[])
ORDER BY workspace_id
FOR KEY SHARE;

-- name: LockWorkspaceClaimIntakeControlForMutation :one
SELECT *
FROM workspace_claim_intake_control
WHERE workspace_id = @workspace_id
FOR UPDATE;

-- name: LockWorkspaceClaimIntakeControlForLedger :one
-- Hold a shared row lock while producing the operator ledger so its top-level
-- authoritative control metadata cannot change between the control read and the
-- task snapshot. Claim transactions may continue because they take the same
-- shared lock; pause/resume mutations serialize behind this short read.
SELECT *
FROM workspace_claim_intake_control
WHERE workspace_id = @workspace_id
FOR KEY SHARE;

-- name: ApplyWorkspaceClaimIntakeControlMutation :one
UPDATE workspace_claim_intake_control
SET state = @state,
    generation = @generation,
    updated_by_type = @updated_by_type,
    updated_by_id = @updated_by_id,
    auth_source = @auth_source,
    actor_display = @actor_display,
    reason = @reason,
    authoritative_action_id = @authoritative_action_id,
    effective_at = @effective_at,
    updated_at = @effective_at
WHERE workspace_id = @workspace_id
RETURNING *;

-- name: InsertWorkspaceClaimIntakeAction :one
INSERT INTO workspace_claim_intake_action (
    id,
    workspace_id,
    action,
    idempotency_key,
    expected_generation,
    requested_at,
    effective_at,
    actor_type,
    actor_id,
    auth_source,
    actor_display,
    reason,
    previous_state,
    resulting_state,
    generation,
    result,
    error_class,
    response_status,
    response_body
) VALUES (
    @id,
    @workspace_id,
    @action,
    @idempotency_key,
    sqlc.narg('expected_generation'),
    @requested_at,
    @effective_at,
    @actor_type,
    @actor_id,
    @auth_source,
    @actor_display,
    @reason,
    @previous_state,
    @resulting_state,
    @generation,
    @result,
    sqlc.narg('error_class'),
    @response_status,
    @response_body
)
RETURNING *;

-- name: ListWorkspaceIDsForRuntimes :many
SELECT id AS runtime_id, workspace_id
FROM agent_runtime
WHERE id = ANY(@runtime_ids::uuid[])
ORDER BY workspace_id, id;

-- name: ListWorkspaceClaimIntakeActions :many
SELECT *
FROM workspace_claim_intake_action
WHERE workspace_id = @workspace_id
ORDER BY created_at DESC, id DESC
LIMIT @result_limit::int OFFSET @result_offset::int;

-- name: ListWorkspaceClaimIntakeLedger :many
SELECT
    task.id AS task_id,
    task.status AS task_status,
    task.agent_id,
    task.runtime_id,
    task.claim_consumer_id,
    task.dispatched_at,
    task.prepare_lease_expires_at,
    CASE
        WHEN task.status = 'dispatched'
         AND COALESCE(task.prepare_lease_expires_at, task.dispatched_at + interval '3 minutes') < now()
        THEN true
        ELSE false
    END AS stale_reclaimable,
    task.claim_intake_generation,
    task.claim_intake_action_id,
    CASE
        WHEN task.claim_intake_generation IS NULL THEN 'unclassified'
        WHEN task.claim_intake_generation < @current_generation::bigint THEN 'pre_fence'
        WHEN task.claim_intake_generation = @current_generation::bigint THEN 'current_generation'
        ELSE 'post_fence_anomaly'
    END AS fence_classification
FROM agent_task_queue task
JOIN agent task_agent
  ON task_agent.id = task.agent_id
WHERE task_agent.workspace_id = @workspace_id
  AND task.status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
ORDER BY task.created_at ASC, task.id ASC
LIMIT @result_limit::int OFFSET @result_offset::int;

-- name: CountWorkspaceClaimIntakeLedgerByStatus :many
SELECT
    task.status AS task_status,
    count(*)::bigint AS task_count
FROM agent_task_queue task
JOIN agent task_agent
  ON task_agent.id = task.agent_id
WHERE task_agent.workspace_id = @workspace_id
  AND task.status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
GROUP BY task.status;

-- name: GetWorkspaceClaimIntakeActionByIdempotencyKey :one
SELECT *
FROM workspace_claim_intake_action
WHERE workspace_id = @workspace_id
  AND idempotency_key = @idempotency_key;

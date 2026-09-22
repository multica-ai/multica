-- Runtime-scoped provider circuit breaker (SE-37711 / SE-37664). One row per
-- (runtime_id, provider); every statement here serializes on that single row.

-- name: GetRuntimeProviderCircuit :one
-- Selector read. No row means the circuit has never opened, i.e. closed.
SELECT * FROM runtime_provider_circuit
WHERE runtime_id = $1 AND provider = $2;

-- name: OpenRuntimeProviderCircuit :many
-- Terminal-failure write. Opens the circuit, or escalates an already-open one,
-- but only when this failure is newer than the failure that opened the current
-- generation — so a duplicate/late terminal callback for an older task is a
-- no-op (returns zero rows) instead of resetting the timer or bumping the
-- generation. Each accepted failure bumps generation and clears any probe.
INSERT INTO runtime_provider_circuit (
    workspace_id, runtime_id, provider, state, generation, reason,
    opened_at, reset_at, failure_completed_at, failure_task_id, reset_source
) VALUES ($1, $2, $3, 'open', 1, $4, now(), $5, $6, $7, $8)
ON CONFLICT (runtime_id, provider) DO UPDATE SET
    state = 'open',
    generation = runtime_provider_circuit.generation + 1,
    reason = EXCLUDED.reason,
    opened_at = now(),
    reset_at = EXCLUDED.reset_at,
    failure_completed_at = EXCLUDED.failure_completed_at,
    failure_task_id = EXCLUDED.failure_task_id,
    reset_source = EXCLUDED.reset_source,
    success_completed_at = NULL,
    success_task_id = NULL,
    probe_task_id = NULL,
    probe_expires_at = NULL,
    updated_at = now()
WHERE runtime_provider_circuit.failure_completed_at IS NULL
   OR EXCLUDED.failure_completed_at > runtime_provider_circuit.failure_completed_at
   OR (EXCLUDED.failure_completed_at = runtime_provider_circuit.failure_completed_at
       AND EXCLUDED.failure_task_id > runtime_provider_circuit.failure_task_id)
RETURNING *;

-- name: CloseRuntimeProviderCircuitOnSuccess :many
-- Terminal-success write. Closes an open/half-open circuit, but only when this
-- success is newer than the failure that opened the current generation: an old
-- success that completed before the current failure must not close a fresher
-- open circuit. Returns the closed row, or zero rows when the success is stale
-- or the circuit was already closed.
UPDATE runtime_provider_circuit
SET state = 'closed',
    reason = NULL,
    reset_at = NULL,
    probe_task_id = NULL,
    probe_expires_at = NULL,
    success_completed_at = $3,
    success_task_id = $4,
    updated_at = now()
WHERE runtime_id = $1 AND provider = $2
  AND state <> 'closed'
  AND (failure_completed_at IS NULL
       OR $3 > failure_completed_at
       OR ($3 = failure_completed_at AND $4 > failure_task_id))
RETURNING *;

-- name: AcquireRuntimeProviderHalfOpenProbe :many
-- Exactly-one half-open probe. Once reset_at has passed, the first caller to
-- run this wins the probe lease (probe_task_id / probe_expires_at); a second
-- concurrent caller serializes on the row and then fails the WHERE (probe still
-- held and unexpired), returning zero rows. An expired probe lease is
-- reclaimable, so a crashed probe holder does not wedge the circuit open.
UPDATE runtime_provider_circuit
SET state = 'half_open',
    probe_task_id = $3,
    probe_expires_at = $4,
    updated_at = now()
WHERE runtime_id = $1 AND provider = $2
  AND state IN ('open', 'half_open')
  AND reset_at IS NOT NULL AND reset_at <= now()
  AND (probe_task_id IS NULL OR probe_expires_at IS NULL OR probe_expires_at < now())
RETURNING *;

-- name: DeleteRuntimeProviderCircuitsByRuntime :exec
-- Runtime teardown removes the runtime's circuit rows in the same transaction.
DELETE FROM runtime_provider_circuit WHERE runtime_id = $1;

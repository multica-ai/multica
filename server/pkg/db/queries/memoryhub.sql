-- =====================
-- MemoryHub bindings, workspace config, secrets, docket, claim gate, and
-- evidence review CAS. Owner: ALL-16.
-- V6-3: NO credential-handle / credential-grant SQL representation. Handle
-- and grant live only in the in-process registry in
-- server/internal/service/memoryhub_secret_broker.go; nothing here persists
-- them, and no query in this file may touch a handle or grant table/column.
-- =====================

-- =====================
-- Binding lifecycle
-- =====================

-- name: InsertMemoryHubBinding :one
INSERT INTO memoryhub_binding (
    workspace_id, scope_kind, scope_id, subject_type, subject_id,
    status, version, idempotency_key,
    remote_team_id, remote_agent_id, remote_task_id, remote_name
) VALUES (
    $1, $2, $3, $4, $5,
    'unbound', 1, $6,
    sqlc.narg(remote_team_id), sqlc.narg(remote_agent_id), sqlc.narg(remote_task_id), sqlc.narg(remote_name)
)
RETURNING *;

-- name: GetMemoryHubBindingByID :one
SELECT * FROM memoryhub_binding WHERE id = $1;

-- name: GetMemoryHubBindingByIDempotencyKey :one
SELECT * FROM memoryhub_binding WHERE idempotency_key = $1;

-- name: ListMemoryHubBindingsByWorkspace :many
SELECT * FROM memoryhub_binding
WHERE workspace_id = $1
ORDER BY created_at DESC, id DESC;

-- name: ListMemoryHubBindingsByScope :many
-- Normalized scope filter for the bindings list endpoint. Scope filters are
-- mutually exclusive: a workspace scope requires scope_id NULL, a project
-- scope requires it present. subject_type-only is a valid type filter.
SELECT * FROM memoryhub_binding
WHERE workspace_id = $1
  AND (@scope_kind::text IS NULL OR scope_kind = @scope_kind)
  AND (@scope_id::uuid IS NULL OR scope_id = @scope_id)
  AND (@subject_type::text IS NULL OR subject_type = @subject_type)
  AND (@subject_id::uuid IS NULL OR subject_id = @subject_id)
ORDER BY created_at DESC, id DESC;

-- name: UpdateMemoryHubBindingStateCAS :one
-- Optimistic state transition: zero rows => 409 binding_transition_conflict.
UPDATE memoryhub_binding
SET status = $2,
    version = version + 1,
    updated_at = now()
WHERE id = $1 AND status = $3 AND version = $4
RETURNING *;

-- name: UpdateMemoryHubBindingRemoteStateCAS :one
-- State transition plus remote-ref/evidence update in one CAS write.
UPDATE memoryhub_binding
SET status = $2,
    version = version + 1,
    remote_team_id = COALESCE(sqlc.narg(remote_team_id), remote_team_id),
    remote_agent_id = COALESCE(sqlc.narg(remote_agent_id), remote_agent_id),
    remote_task_id = COALESCE(sqlc.narg(remote_task_id), remote_task_id),
    remote_name = COALESCE(sqlc.narg(remote_name), remote_name),
    evidence_ref = COALESCE(sqlc.narg(evidence_ref), evidence_ref),
    next_wakeup = sqlc.narg(next_wakeup),
    updated_at = now()
WHERE id = $1 AND status = $3 AND version = $4
RETURNING *;

-- name: DeleteMemoryHubBinding :exec
DELETE FROM memoryhub_binding WHERE id = $1;

-- =====================
-- Workspace config (refs only; never a plaintext key)
-- =====================

-- name: GetMemoryHubWorkspaceConfig :one
SELECT * FROM memoryhub_workspace_config WHERE workspace_id = $1;

-- name: UpsertMemoryHubWorkspaceConfig :one
INSERT INTO memoryhub_workspace_config (workspace_id, credential_ref, user_key_hash, service_id)
VALUES ($1, sqlc.narg(credential_ref), sqlc.narg(user_key_hash), sqlc.narg(service_id))
ON CONFLICT (workspace_id) DO UPDATE SET
    credential_ref = COALESCE(EXCLUDED.credential_ref, memoryhub_workspace_config.credential_ref),
    user_key_hash  = COALESCE(EXCLUDED.user_key_hash, memoryhub_workspace_config.user_key_hash),
    service_id     = COALESCE(EXCLUDED.service_id, memoryhub_workspace_config.service_id),
    updated_at     = now()
RETURNING *;

-- =====================
-- Secret broker (encrypted envelope + state/CAS/lease/rotation)
-- =====================

-- name: InsertSecret :one
INSERT INTO memoryhub_secret (
    workspace_id, credential_ref, kind, envelope_version, key_id,
    nonce, ciphertext, aad, user_key_hash
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetSecretForClaim :one
-- Claim-time lookup: only a non-revoked envelope may be decrypted.
SELECT * FROM memoryhub_secret
WHERE credential_ref = $1 AND revoked_at IS NULL;

-- name: ClaimSecretsForReencryption :many
-- Rotation worker claim: pick active/rotating rows whose lease is expired
-- (or absent) and take a lease. CAS on state_version prevents a stale worker
-- from overwriting a newer version.
UPDATE memoryhub_secret
SET lease_owner = $1,
    lease_expires_at = now() + $2::interval
WHERE id IN (
    SELECT id FROM memoryhub_secret
    WHERE state IN ('active', 'rotating')
      AND (lease_expires_at IS NULL OR lease_expires_at < now())
    ORDER BY updated_at ASC
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: UpdateSecretStateCAS :one
-- Generic active/rotating CAS transition used by rotation entry and
-- blocked_migration on decrypt failure.
UPDATE memoryhub_secret
SET state = $2,
    state_version = state_version + 1,
    lease_owner = NULL,
    lease_expires_at = NULL,
    last_error_code = sqlc.narg(last_error_code),
    last_error_at = CASE WHEN sqlc.narg(last_error_code)::text IS NULL THEN last_error_at ELSE now() END,
    updated_at = now()
WHERE id = $1 AND state = $3 AND state_version = $4
RETURNING *;

-- name: CompleteSecretRotationCAS :one
-- Reencrypt completed: write new nonce/ciphertext/key_id/envelope_version,
-- rotate key id chain, then CAS back to active.
UPDATE memoryhub_secret
SET envelope_version = envelope_version + 1,
    key_id = $2,
    nonce = $3,
    ciphertext = $4,
    rotation_from_key_id = key_id,
    state = 'active',
    state_version = state_version + 1,
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1 AND state = 'rotating' AND state_version = $5
RETURNING *;

-- name: RevokeSecretCAS :one
UPDATE memoryhub_secret
SET state = 'revoked',
    state_version = state_version + 1,
    revoked_at = now(),
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
  AND state IN ('active', 'rotating')
  AND state_version = $2
RETURNING *;

-- name: ReplaceBlockedSecret :one
-- The only recovery path from blocked_migration: owner-authorized config
-- replacement writes a fresh envelope under CAS.
UPDATE memoryhub_secret
SET envelope_version = $2,
    key_id = $3,
    nonce = $4,
    ciphertext = $5,
    aad = $6,
    user_key_hash = $7,
    rotation_from_key_id = NULL,
    state = 'active',
    state_version = state_version + 1,
    last_error_code = NULL,
    last_error_at = NULL,
    lease_owner = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1 AND state = 'blocked_migration'
RETURNING *;

-- name: ListExpiredSecretLeases :many
SELECT * FROM memoryhub_secret
WHERE lease_owner IS NOT NULL AND lease_expires_at < now();

-- =====================
-- Memory Docket (durable rows)
-- =====================

-- name: UpsertMemoryDocket :one
INSERT INTO memoryhub_memory_docket (
    workspace_id, scope_kind, scope_id, subject_type, subject_id,
    policy, revision, generated_at, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg(expires_at)
)
ON CONFLICT (workspace_id, scope_id, subject_type, subject_id, revision)
    WHERE scope_kind = 'project' AND scope_id IS NOT NULL
DO UPDATE SET
    policy = EXCLUDED.policy,
    generated_at = EXCLUDED.generated_at,
    expires_at = EXCLUDED.expires_at,
    updated_at = now()
RETURNING *;

-- name: GetMemoryDocketBySubject :one
SELECT * FROM memoryhub_memory_docket
WHERE workspace_id = $1
  AND scope_kind = $2
  AND scope_id IS NOT DISTINCT FROM $3
  AND subject_type = $4
  AND subject_id = $5
ORDER BY revision DESC
LIMIT 1;

-- name: ListMemoryItemsByDocket :many
SELECT * FROM memoryhub_memory_item
WHERE docket_id = $1
ORDER BY priority DESC, created_at ASC;

-- name: InsertMemoryItem :one
INSERT INTO memoryhub_memory_item (
    docket_id, state, kind, summary, source_ref,
    evidence_ref, priority, dedupe_key, expires_at
) VALUES (
    $1, 'active', $2, $3, $4,
    sqlc.narg(evidence_ref), $5, $6, sqlc.narg(expires_at)
)
RETURNING *;

-- name: GetMemoryItemByID :one
SELECT * FROM memoryhub_memory_item WHERE id = $1;

-- name: WithdrawMemoryItemCAS :one
UPDATE memoryhub_memory_item
SET state = 'withdrawn',
    withdrawn_at = now(),
    updated_at = now()
WHERE id = $1 AND state = $2
RETURNING *;

-- name: ListActiveExpiredMemoryItems :many
SELECT * FROM memoryhub_memory_item
WHERE state = 'active' AND expires_at IS NOT NULL AND expires_at < now();

-- =====================
-- Claim gate (reservation + outcome, queue stays queued until commit)
-- =====================

-- name: ReserveQueuedTaskForMemoryGate :one
-- Reserve a queued row for gate preflight without changing status or
-- dispatched_at. Excludes rows with a live gate lease held by someone else.
UPDATE agent_task_queue
SET memory_gate_state = 'preparing',
    memory_gate_lease_id = $1,
    memory_gate_lease_expires_at = now() + $2::interval
WHERE id = $3
  AND status = 'queued'
  AND (
      memory_gate_lease_id IS NULL
      OR memory_gate_lease_id = $1
      OR memory_gate_lease_expires_at < now()
  )
RETURNING *;

-- name: SetMemoryGateOutcome :one
-- Persist a gate outcome without changing queue status (required-fail and
-- degraded outcomes both keep the row queued).
UPDATE agent_task_queue
SET memory_gate_state = $2,
    memory_gate_error_code = sqlc.narg(memory_gate_error_code),
    memory_gate_evidence_ref = sqlc.narg(memory_gate_evidence_ref),
    memory_gate_next_wakeup = sqlc.narg(memory_gate_next_wakeup),
    memory_gate_lease_id = NULL,
    memory_gate_lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CommitReservedTaskClaim :one
-- The ONLY query that moves a gate-approved queue row from queued to
-- dispatched. Runs in one transaction with the ledger claim update; all
-- steps succeed or none do.
UPDATE agent_task_queue
SET status = 'dispatched',
    dispatched_at = COALESCE(dispatched_at, now()),
    execution_id = sqlc.narg(execution_id),
    memoryhub_run_id = sqlc.narg(memoryhub_run_id),
    memory_gate_state = 'ready',
    memory_gate_error_code = NULL,
    memory_gate_evidence_ref = NULL,
    memory_gate_next_wakeup = NULL,
    memory_gate_lease_id = NULL,
    memory_gate_lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
  AND status = 'queued'
  AND memory_gate_lease_id = $2
RETURNING *;

-- name: ReleaseExpiredMemoryGateReservations :exec
UPDATE agent_task_queue
SET memory_gate_lease_id = NULL,
    memory_gate_lease_expires_at = NULL,
    updated_at = now()
WHERE memory_gate_lease_id IS NOT NULL
  AND memory_gate_lease_expires_at < now();

-- =====================
-- Evidence review lifecycle (V5-7 + V6-1/V6-2)
-- =====================

-- name: InsertExecutionEvidenceRecord :one
INSERT INTO execution_evidence_record (execution_id, workspace_id)
VALUES ($1, $2)
ON CONFLICT (execution_id) DO NOTHING
RETURNING *;

-- name: GetExecutionEvidenceRecord :one
SELECT * FROM execution_evidence_record WHERE execution_id = $1;

-- name: GetExecutionEvidenceRecordScoped :one
-- V6-1.3 scoped load: a record in another workspace is indistinguishable from
-- an absent one (404).
SELECT * FROM execution_evidence_record
WHERE execution_id = $1 AND workspace_id = $2;

-- name: UpdateEvidenceRecordRuntimeStateCAS :one
-- Set runtime evidence state (collecting|complete|failed) under CAS.
UPDATE execution_evidence_record
SET runtime_evidence_state = $2,
    updated_at = now()
WHERE execution_id = $1 AND runtime_evidence_state = $3
RETURNING *;

-- name: SetEvidenceRecordCompletionRefs :one
-- Atomic five-category completion write: output/message/usage/artifact/test
-- refs persisted together when the runtime gate passes.
UPDATE execution_evidence_record
SET output_ref = $2,
    message_refs = $3,
    usage_refs = $4,
    artifact_refs = $5,
    test_refs = $6,
    runtime_evidence_state = 'complete',
    updated_at = now()
WHERE execution_id = $1 AND runtime_evidence_state = 'collecting'
RETURNING *;

-- name: SetEvidenceRecordGateFailure :one
UPDATE execution_evidence_record
SET runtime_evidence_state = 'failed',
    updated_at = now()
WHERE execution_id = $1 AND runtime_evidence_state = 'collecting'
RETURNING *;

-- name: ClaimPendingReviewCAS :one
-- Scheduler lease claim: pending -> dispatching, increments review_attempt.
UPDATE execution_evidence_record
SET review_state = 'dispatching',
    review_attempt = review_attempt + 1,
    review_lease_owner = $2,
    review_lease_expires_at = now() + $3::interval,
    updated_at = now()
WHERE execution_id = $1
  AND review_state = 'pending'
  AND review_version = $4
RETURNING *;

-- name: ListReviewDueRecords :many
-- Migration 328 drives this: only pending|dispatching|retry_wait. blocked is
-- never scheduled automatically and never appears here.
SELECT * FROM execution_evidence_record
WHERE review_state IN ('pending', 'dispatching', 'retry_wait')
  AND review_next_wakeup <= now()
ORDER BY review_next_wakeup ASC
LIMIT $1;

-- name: MarkReviewQueuedCAS :one
-- dispatching -> queued: reviewer task + ledger committed; store task id.
UPDATE execution_evidence_record
SET review_state = 'queued',
    review_task_id = $2,
    review_lease_owner = NULL,
    review_lease_expires_at = NULL,
    updated_at = now()
WHERE execution_id = $1
  AND review_state = 'dispatching'
  AND review_version = $3
RETURNING *;

-- name: MarkReviewRecordedCAS :one
-- running -> recorded: independent reviewer refs persist atomically; wakeup
-- cleared; terminal for review.
UPDATE execution_evidence_record
SET review_state = 'recorded',
    review_output_ref = $2,
    review_next_wakeup = NULL,
    review_lease_owner = NULL,
    review_lease_expires_at = NULL,
    updated_at = now()
WHERE execution_id = $1
  AND review_state = 'running'
  AND review_version = $3
RETURNING *;

-- name: MarkReviewRetryWaitCAS :one
-- transient failure with attempts remaining: set a future wakeup.
UPDATE execution_evidence_record
SET review_state = 'retry_wait',
    review_next_wakeup = $2,
    review_failure_code = $3,
    review_lease_owner = NULL,
    review_lease_expires_at = NULL,
    updated_at = now()
WHERE execution_id = $1
  AND review_state IN ('dispatching', 'queued', 'running')
  AND review_version = $4
RETURNING *;

-- name: MarkReviewPendingRetryCAS :one
-- retry_wait -> pending when the wakeup is reached and attempts remain.
UPDATE execution_evidence_record
SET review_state = 'pending',
    review_task_id = NULL,
    review_output_ref = NULL,
    review_next_wakeup = now(),
    review_failure_code = NULL,
    updated_at = now()
WHERE execution_id = $1
  AND review_state = 'retry_wait'
  AND review_version = $2
  AND review_next_wakeup <= now()
RETURNING *;

-- name: BlockReviewCAS :one
-- pending|dispatching|queued|running|retry_wait -> blocked. Clears wakeup and
-- lease so blocked is never automatically scheduled.
UPDATE execution_evidence_record
SET review_state = 'blocked',
    review_failure_code = $2,
    review_next_wakeup = NULL,
    review_lease_owner = NULL,
    review_lease_expires_at = NULL,
    updated_at = now()
WHERE execution_id = $1
  AND review_state IN ('pending', 'dispatching', 'queued', 'running', 'retry_wait')
  AND review_version = $3
RETURNING *;

-- name: RepairBlockedReviewerCAS :one
-- V6-1/V6-2 owner repair: the ONLY transition out of blocked. Scoped
-- optimistic-CAS review update; zero rows => 409. Runs inside the repair
-- transaction that also inserts the memoryhub_review_repaired audit row and
-- publishes the scheduler wakeup only after commit. This is a persisted
-- evidence-review query, not a credential-handle query.
UPDATE execution_evidence_record
SET review_state = 'pending',
    review_version = review_version + 1,
    reviewer_agent_id = $3,
    review_task_id = NULL,
    review_output_ref = NULL,
    review_attempt = 0,
    review_next_wakeup = now(),
    review_failure_code = NULL,
    review_lease_owner = NULL,
    review_lease_expires_at = NULL,
    updated_at = now()
WHERE execution_id = $1
  AND workspace_id = $4
  AND review_state = 'blocked'
  AND review_version = $2
RETURNING *;

-- name: ReleaseExpiredReviewLeases :exec
UPDATE execution_evidence_record
SET review_lease_owner = NULL,
    review_lease_expires_at = NULL,
    updated_at = now()
WHERE review_lease_owner IS NOT NULL
  AND review_lease_expires_at < now()
  AND review_state IN ('dispatching', 'queued', 'running');

-- =====================
-- Evidence events + scores
-- =====================

-- name: InsertEvidenceEvent :one
INSERT INTO execution_evidence_event (
    schema_version, execution_id, run_id, workspace_id, project_id,
    agent_id, runtime_id, model, sequence, kind, payload_ref,
    payload_sha256, occurred_at, retention_until
) VALUES (
    1, $1, $2, $3, sqlc.narg(project_id),
    $4, $5, $6, $7, $8, $9,
    $10, $11, $12
)
RETURNING *;

-- name: ListEvidenceEventsByExecution :many
SELECT * FROM execution_evidence_event
WHERE execution_id = $1
ORDER BY sequence ASC, id ASC;

-- name: InsertEvidenceScore :one
INSERT INTO execution_evidence_score (
    execution_id, algorithm_version, input_digest,
    availability, isolation, security, recovery, performance, observability,
    overall, eligible, input_snapshot, computed_at, evidence_refs
) VALUES (
    $1, $2, $3,
    $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14
)
RETURNING *;

-- name: GetEvidenceScore :one
SELECT * FROM execution_evidence_score
WHERE execution_id = $1 AND algorithm_version = $2;

-- name: PruneExpiredEvidence :exec
DELETE FROM execution_evidence_event
WHERE retention_until < now()
  AND NOT EXISTS (
      SELECT 1 FROM execution_ledger el
      WHERE el.execution_id = execution_evidence_event.execution_id
        AND el.state IN ('queued', 'claimed', 'running')
  );

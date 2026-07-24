-- Event Hooks MVP (MUL-4332) — hook / revision / execution persistence access.
-- All associations are application-validated; there are no foreign keys. Every
-- write is workspace-scoped for tenant isolation. The hook and its immutable
-- revisions are created together in one transaction with app-generated ids
-- (hook.active_revision_id and hook_revision.id are chosen up front) because
-- the two rows reference each other and there is no FK to order them.

-- name: CreateHook :one
INSERT INTO hook (
    id, workspace_id, name, enabled, active_revision_id,
    scope_type, scope_id, origin,
    creator_actor_type, creator_actor_id, authorization_principal_user_id
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10, $11
)
RETURNING *;

-- name: GetHookInWorkspace :one
SELECT * FROM hook
WHERE id = $1 AND workspace_id = $2;

-- name: GetHookForUpdate :one
-- Row-locking load used by PATCH so concurrent edits to the same hook serialize:
-- the lock holder allocates the next revision and repoints the active pointer
-- before the next waiter reads MAX(revision), so idx_hook_revision_unique can
-- never be violated by a MAX+1 race (MUL-4332 PR2 review point 4).
SELECT * FROM hook
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: ListHooksByWorkspace :many
SELECT * FROM hook
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: ListHooksByScope :many
SELECT * FROM hook
WHERE workspace_id = $1 AND scope_type = $2 AND scope_id = $3 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: SetHookActiveRevision :one
-- Switch the active revision pointer and update the display name (PATCH).
-- Revisions themselves are never mutated; a config change appends a new revision
-- and repoints here. Scope is immutable after creation and is not touched.
UPDATE hook SET
    active_revision_id = $3,
    name = $4,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND archived_at IS NULL
RETURNING *;

-- name: SetHookEnabled :one
UPDATE hook SET
    enabled = $3,
    disabled_reason = sqlc.narg('disabled_reason'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND archived_at IS NULL
RETURNING *;

-- name: ArchiveHook :one
-- Soft archive (DELETE). Existing revisions / executions / effects are retained
-- for audit; the hook is also disabled so nothing can match it.
UPDATE hook SET
    archived_at = now(),
    enabled = false,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND archived_at IS NULL
RETURNING *;

-- name: CreateHookRevision :one
INSERT INTO hook_revision (
    id, hook_id, revision, event_type, match, conditions, fire_mode, actions,
    created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10
)
RETURNING *;

-- name: GetHookRevision :one
SELECT * FROM hook_revision
WHERE id = $1;

-- name: GetHookRevisionByNumber :one
-- A specific revision of a hook, for `explain --revision N` (read-only debug).
SELECT * FROM hook_revision
WHERE hook_id = $1 AND revision = $2;

-- name: GetMaxHookRevision :one
-- Highest revision number for a hook, 0 when none exist yet. Used to compute the
-- next revision on PATCH.
SELECT COALESCE(MAX(revision), 0)::int AS max_revision
FROM hook_revision
WHERE hook_id = $1;

-- name: ListHookExecutionsByHook :many
-- Execution trace for the debug/explain endpoints. Newest first, bounded.
SELECT * FROM hook_execution
WHERE hook_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListActiveHookRevisionsForEvent :many
-- Materialize the complete candidate set for a domain event in ONE statement:
-- every enabled, non-archived hook in the workspace whose ACTIVE revision listens
-- to this event type, together with that revision's full configuration.
--
-- Being a SINGLE statement is the point (MUL-4332 PR3 review round: matcher point
-- 1). Under READ COMMITTED every statement gets its own snapshot, so reading the
-- candidate ids and then re-reading each hook's active_revision_id one by one could
-- mix revisions from different instants within the same transaction. Returning
-- (hook, revision) pairs from one statement pins the whole set at one instant, and
-- the matcher calls this inside the transaction that claims the event, so the pin
-- is taken at claim time. The matcher must NOT re-read active_revision_id
-- afterwards — the pinned revision_id here is authoritative for this event.
--
-- Issue scope is lifecycle ownership only and does NOT restrict the event subject —
-- that is the job of `when` — so scope is not a filter here.
SELECT
    h.id           AS hook_id,
    r.id           AS revision_id,
    r.match        AS match,
    r.conditions   AS conditions,
    r.fire_mode    AS fire_mode
FROM hook h
JOIN hook_revision r ON r.id = h.active_revision_id
WHERE h.workspace_id = $1
  AND h.enabled = true
  AND h.archived_at IS NULL
  AND r.event_type = $2
ORDER BY h.created_at ASC, h.id ASC;

-- name: LockHookForDecision :one
-- Serialize one hook's rising-edge latch read-modify-write against other matchers.
-- This is a LOCK ONLY: the hook's active_revision_id is deliberately not returned,
-- because the revision for this event was already pinned at claim time by
-- ListActiveHookRevisionsForEvent and re-reading it here would reintroduce the
-- drift that pin exists to prevent.
SELECT id FROM hook
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateHookExecution :one
-- Persist one matcher decision (queued to fire, or skipped with a reason) with
-- the evaluator's structured snapshots and the pinned revision. Idempotent per
-- (hook_id, event_id) via idx_hook_execution_hook_event, so re-processing a
-- re-leased event never double-creates or double-advances the latch.
INSERT INTO hook_execution (
    id, workspace_id, hook_id, hook_revision_id, event_id, correlation_id,
    status, skip_reason, match_snapshot, condition_snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (hook_id, event_id) DO NOTHING
RETURNING *;

-- name: CreateHookExecutionFailure :one
-- Terminal isolation record for a candidate whose STORED CONFIG cannot be
-- evaluated (a malformed revision fails identically on every retry). Writing it
-- lets the matcher finish the remaining candidates and finalize the event, so one
-- bad rule can never starve the healthy rules on the same event (MUL-4332 PR3
-- review round: matcher point 4). It carries no snapshots — evaluation never
-- produced one. Same (hook_id, event_id) idempotency as the decision path.
INSERT INTO hook_execution (
    id, workspace_id, hook_id, hook_revision_id, event_id, correlation_id,
    status, error_code, error, completed_at
) VALUES ($1, $2, $3, $4, $5, $6, 'failed', $7, $8, now())
ON CONFLICT (hook_id, event_id) DO NOTHING
RETURNING *;

-- name: ClaimOneHookExecution :one
-- The executor's claim (MUL-4332 PR3 §7.2): lease one queued execution whose retry
-- backoff has elapsed, or reclaim one abandoned by a crashed worker once its lease
-- expires. Oldest first. The lease deadline comes from the DATABASE clock, the same
-- clock every ownership predicate compares it against, so app/DB skew cannot grant an
-- already-expired or over-long lease. FOR UPDATE SKIP LOCKED lets several executors
-- share the queue. Rides idx_hook_execution_lease.
UPDATE hook_execution
SET status = 'running',
    lease_token = @lease_token,
    lease_expires_at = clock_timestamp() + make_interval(secs => @lease_ttl_seconds::float8),
    attempts = attempts + 1,
    started_at = COALESCE(started_at, now())
WHERE id = (
    SELECT id FROM hook_execution
    WHERE (status = 'queued' AND (next_attempt_at IS NULL OR next_attempt_at <= now()))
       OR (status = 'running' AND lease_expires_at < now())
    ORDER BY created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetOwnedHookExecution :one
-- Row-lock a claimed execution and assert lease OWNERSHIP before any action write.
-- Identical predicate to every terminal write below, evaluated against database clock
-- time, so a worker whose lease was reclaimed — or whose own lease elapsed — is not
-- the owner and must write nothing (§7.3: a lost lease may never write terminal state).
SELECT * FROM hook_execution
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp()
FOR UPDATE;

-- name: AdvanceHookExecutionAction :execrows
-- Move the action cursor past a completed action, under the ownership predicate. It
-- runs in the SAME transaction as that action's target write and effect, so an action
-- can never commit without its cursor advancing.
UPDATE hook_execution
SET current_action_index = @next_action_index
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: MarkHookExecutionSucceeded :execrows
UPDATE hook_execution
SET status = 'succeeded', completed_at = now(), lease_token = NULL, lease_expires_at = NULL
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: MarkHookExecutionSkipped :execrows
-- A terminal, non-retryable outcome (§7.3): permission, unavailable target, departed
-- principal. Distinct from `failed`, which is an exhausted infrastructure retry.
UPDATE hook_execution
SET status = 'skipped', skip_reason = @skip_reason, completed_at = now(),
    lease_token = NULL, lease_expires_at = NULL
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: MarkHookExecutionFailed :execrows
UPDATE hook_execution
SET status = 'failed', error_code = @error_code, error = @error, completed_at = now(),
    lease_token = NULL, lease_expires_at = NULL
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: RescheduleHookExecution :execrows
-- Release the lease and re-queue for a later attempt after an infrastructure failure.
-- current_action_index is untouched, so the retry resumes at the action that failed
-- and every action already committed stays committed (§7.2 partial execution).
UPDATE hook_execution
SET status = 'queued',
    next_attempt_at = now() + make_interval(secs => @backoff_seconds::int),
    lease_token = NULL, lease_expires_at = NULL
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: GetHookActionEffect :one
SELECT * FROM hook_action_effect WHERE effect_key = $1;

-- name: CreateHookActionEffect :one
-- Claim one action's idempotency anchor. ON CONFLICT DO NOTHING means a concurrent or
-- replayed attempt gets no row back and must read the existing effect instead of
-- re-running the action.
INSERT INTO hook_action_effect (
    id, effect_key, execution_id, action_index, action_type, status, resolved_input, attempts
) VALUES ($1, $2, $3, $4, $5, 'running', $6, 1)
ON CONFLICT (effect_key) DO NOTHING
RETURNING *;

-- name: MarkHookActionEffectSucceeded :execrows
UPDATE hook_action_effect
SET status = 'succeeded', output_type = @output_type, output_id = @output_id, completed_at = now()
WHERE effect_key = $1;

-- name: HeartbeatHookExecution :execrows
-- Extend the lease of an execution this worker still owns (§7.2). A long action must
-- not lose its lease simply because it is slow; the heartbeat keeps a live worker's
-- claim valid while an ABANDONED claim still expires on schedule.
UPDATE hook_execution
SET lease_expires_at = clock_timestamp() + make_interval(secs => @lease_ttl_seconds::float8)
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: DeferExpiredHookExecution :execrows
-- Back off an execution whose lease elapsed while THIS worker was running it. The
-- CAS is on lease_token only — deliberately without the not-expired condition, since
-- the lease is expired by definition here — so it fires only while the row still
-- carries our token. If another worker has already reclaimed it the token differs,
-- nothing is written, and the new owner is left alone. Without this the row keeps its
-- original ordering position and the next claim selects it again immediately,
-- starving everything behind it.
UPDATE hook_execution
SET status = 'queued',
    next_attempt_at = now() + make_interval(secs => @backoff_seconds::int),
    lease_token = NULL,
    lease_expires_at = NULL
WHERE id = $1
  AND status = 'running'
  AND lease_token = $2;

-- name: UpsertTerminalHookActionEffect :exec
-- Record the durable audit row for an action that ended terminally — skipped or
-- failed — carrying its resolved input and the reason. The success path writes its
-- effect inside the action transaction, which rolls back on failure, so without this
-- a skipped/failed action would leave no trace at all and a partial execution would
-- show only the actions that succeeded. Keyed on effect_key so a retry updates the
-- same row rather than creating a second one for the same (execution, action index).
INSERT INTO hook_action_effect (
    id, effect_key, execution_id, action_index, action_type,
    status, resolved_input, attempts, error_code, error, completed_at
) VALUES ($1, $2, $3, $4, $5, @status, $6, 1, @error_code, @error, now())
ON CONFLICT (effect_key) DO UPDATE SET
    status = EXCLUDED.status,
    resolved_input = EXCLUDED.resolved_input,
    attempts = hook_action_effect.attempts + 1,
    error_code = EXCLUDED.error_code,
    error = EXCLUDED.error,
    completed_at = now();

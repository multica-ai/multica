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

-- name: ListActiveHookIDsForEvent :many
-- Candidate hooks for a domain event: enabled, non-archived hooks in the
-- workspace whose ACTIVE revision listens to this event type. Issue scope is
-- lifecycle ownership only and does NOT restrict the event subject — that is the
-- job of `when` — so scope is not a filter here. The matcher re-reads each hook
-- under a row lock to pin the revision authoritatively.
SELECT h.id
FROM hook h
JOIN hook_revision r ON r.id = h.active_revision_id
WHERE h.workspace_id = $1
  AND h.enabled = true
  AND h.archived_at IS NULL
  AND r.event_type = $2
ORDER BY h.created_at ASC, h.id ASC;

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

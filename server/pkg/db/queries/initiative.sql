-- =====================
-- Initiative CRUD
-- =====================

-- name: CreateInitiative :one
INSERT INTO initiative (
    workspace_id, title, idea, constraints,
    autonomy_level, budget_limit_tokens, max_parallel_tasks, max_attempts,
    stall_timeout_seconds, external_wait_timeout_seconds, created_by
) VALUES (
    $1, $2, $3, $4,
    sqlc.narg('autonomy_level'), sqlc.narg('budget_limit_tokens'),
    sqlc.narg('max_parallel_tasks'), sqlc.narg('max_attempts'),
    sqlc.narg('stall_timeout_seconds'), sqlc.narg('external_wait_timeout_seconds'), $5
) RETURNING *;

-- name: GetInitiativeInWorkspace :one
SELECT * FROM initiative
WHERE id = $1 AND workspace_id = $2;

-- name: ListInitiativesByWorkspace :many
SELECT * FROM initiative
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY updated_at DESC
LIMIT $2;

-- name: ListActiveInitiativeIDs :many
-- Reconciler tick enumeration. Keep the status list in sync with
-- idx_initiative_active (migration 219) and the "active" set in
-- server/internal/service/initiative_transitions.go.
SELECT id FROM initiative
WHERE status IN ('planning', 'plan_review', 'executing', 'integrating', 'verifying', 'needs_human');

-- name: UpdateInitiativeMeta :one
UPDATE initiative SET
    title = COALESCE(sqlc.narg('title'), title),
    idea = COALESCE(sqlc.narg('idea'), idea),
    constraints = COALESCE(sqlc.narg('constraints'), constraints),
    autonomy_level = COALESCE(sqlc.narg('autonomy_level'), autonomy_level),
    budget_limit_tokens = COALESCE(sqlc.narg('budget_limit_tokens'), budget_limit_tokens),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: TransitionInitiativeStatus :one
-- CAS transition: succeeds only when the row is still in one of the expected
-- from-statuses; pgx.ErrNoRows signals a lost race (callers map it to 409 or
-- retry on the next reconcile pass). The pause/needs_human bookkeeping columns
-- are SET (not COALESCEd) so leaving those states clears them.
UPDATE initiative SET
    status = $2,
    pause_prev_status = sqlc.narg('pause_prev_status'),
    pause_reason = sqlc.narg('pause_reason'),
    needs_human_reason = sqlc.narg('needs_human_reason'),
    updated_at = now()
WHERE id = $1 AND status = ANY(sqlc.arg('from_statuses')::text[])
RETURNING *;

-- name: ApproveInitiativePlan :one
-- Atomic human plan-approval gate: CAS out of plan_review + approval stamp.
UPDATE initiative SET
    status = 'executing',
    approved_by = $2,
    approved_at = now(),
    updated_at = now()
WHERE id = $1 AND status = 'plan_review'
RETURNING *;

-- =====================
-- Initiative tasks
--
-- The task queries below are reconciler-facing scaffolding: they ship with
-- the schema so their CAS/idempotency semantics are reviewed alongside the
-- tables, and gain their consumers in the reconciler PRs of this feature
-- series.
-- =====================

-- name: CreateInitiativeTask :one
INSERT INTO initiative_task (
    initiative_id, workspace_id, plan_version, task_key, title, description,
    role, depends_on, acceptance_criteria, required_capabilities,
    max_attempts, assignee_hint
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    sqlc.narg('max_attempts'), $11
) RETURNING *;

-- name: ListInitiativeTasks :many
SELECT * FROM initiative_task
WHERE initiative_id = $1 AND plan_version = $2
ORDER BY created_at, id;

-- name: ListInitiativeTasksAllVersions :many
-- Cleanup paths (cancel) need every linked issue regardless of plan version.
SELECT * FROM initiative_task
WHERE initiative_id = $1
ORDER BY created_at, id;

-- name: GetInitiativeTaskInWorkspace :one
SELECT * FROM initiative_task
WHERE id = $1 AND workspace_id = $2;

-- name: GetInitiativeTaskByIssue :one
SELECT * FROM initiative_task
WHERE issue_id = $1;

-- name: TransitionInitiativeTaskState :one
-- CAS state transition; see TransitionInitiativeStatus for semantics.
UPDATE initiative_task SET
    state = $2,
    state_reason = sqlc.narg('state_reason'),
    updated_at = now()
WHERE id = $1 AND state = ANY(sqlc.arg('from_states')::text[])
RETURNING *;

-- name: SetInitiativeTaskIssue :one
-- Stamps the dispatched issue exactly once; the IS NULL guard makes concurrent
-- dispatch passes idempotent.
UPDATE initiative_task SET
    issue_id = $2,
    updated_at = now()
WHERE id = $1 AND issue_id IS NULL
RETURNING *;

-- name: BumpInitiativeTaskAttempt :one
UPDATE initiative_task SET
    attempt = attempt + 1,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateInitiativeTaskActivity :exec
UPDATE initiative_task SET
    last_activity_at = $2,
    stall_strikes = $3,
    updated_at = now()
WHERE id = $1;

-- name: CountActiveInitiativeTasks :one
-- Parallelism accounting for dispatch. Blocked tasks are parked and do not
-- consume a slot; independent DAG branches keep flowing past them.
SELECT COUNT(*) FROM initiative_task
WHERE initiative_id = $1 AND plan_version = $2
  AND state IN ('dispatched', 'in_progress', 'review', 'verifying');

-- =====================
-- Initiative events (append-only)
-- =====================

-- name: CreateInitiativeEvent :one
INSERT INTO initiative_event (
    workspace_id, initiative_id, task_id, actor_type, actor_id, event_type, payload
) VALUES (
    $1, $2, sqlc.narg('task_id'), $3, sqlc.narg('actor_id'), $4, $5
) RETURNING *;

-- name: ListInitiativeEvents :many
-- Keyset pagination newest-first: pass the (created_at, id) of the last row of
-- the previous page, or NULLs for the first page.
SELECT * FROM initiative_event
WHERE initiative_id = $1
  AND (
    sqlc.narg('before_created_at')::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $2;

-- =====================
-- Initiative blockers
-- =====================

-- name: ListInitiativeBlockers :many
SELECT * FROM initiative_blocker
WHERE initiative_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC;

-- name: GetInitiativeBlockerInWorkspace :one
SELECT * FROM initiative_blocker
WHERE id = $1 AND workspace_id = $2;

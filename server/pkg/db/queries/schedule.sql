-- name: CreateScheduledTask :one
INSERT INTO scheduled_task (
    workspace_id,
    created_by,
    name,
    title_template,
    description,
    assignee_type,
    assignee_id,
    priority,
    cron_expression,
    timezone,
    enabled,
    next_run_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetScheduledTask :one
SELECT * FROM scheduled_task
WHERE id = $1 AND archived_at IS NULL;

-- name: GetScheduledTaskForWorkspace :one
SELECT * FROM scheduled_task
WHERE id = $1 AND workspace_id = $2 AND archived_at IS NULL;

-- name: ListScheduledTasks :many
SELECT * FROM scheduled_task
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateScheduledTask :one
UPDATE scheduled_task
SET
    name            = COALESCE(sqlc.narg('name'), name),
    title_template  = COALESCE(sqlc.narg('title_template'), title_template),
    description     = COALESCE(sqlc.narg('description'), description),
    assignee_type   = COALESCE(sqlc.narg('assignee_type'), assignee_type),
    assignee_id     = COALESCE(sqlc.narg('assignee_id'), assignee_id),
    priority        = COALESCE(sqlc.narg('priority'), priority),
    cron_expression = COALESCE(sqlc.narg('cron_expression'), cron_expression),
    timezone        = COALESCE(sqlc.narg('timezone'), timezone),
    enabled         = COALESCE(sqlc.narg('enabled'), enabled),
    next_run_at     = COALESCE(sqlc.narg('next_run_at'), next_run_at),
    updated_at      = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING *;

-- name: ArchiveScheduledTask :exec
UPDATE scheduled_task
SET archived_at = now(), updated_at = now()
WHERE id = $1 AND archived_at IS NULL;

-- name: ClaimDueScheduledTasks :many
-- Atomically select and lock scheduled_task rows that are enabled, due to
-- fire, and not already archived. The scheduler goroutine calls this in a
-- transaction and must update next_run_at (via UpdateScheduledTaskRun) for
-- each claimed row before committing.
SELECT * FROM scheduled_task
WHERE enabled = TRUE
  AND archived_at IS NULL
  AND next_run_at <= now()
ORDER BY next_run_at ASC
LIMIT 100
FOR UPDATE SKIP LOCKED;

-- name: UpdateScheduledTaskRun :exec
-- Marks a scheduled task as having just run. Callers set next_run_at to the
-- next computed fire time (or to a far-future sentinel for broken schedules).
UPDATE scheduled_task
SET
    last_run_at       = $2,
    last_run_issue_id = $3,
    last_run_error    = $4,
    next_run_at       = $5,
    updated_at        = now()
WHERE id = $1;

-- name: ListSchedules :many
SELECT * FROM schedule
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: ListSchedulesForWorkflow :many
SELECT * FROM schedule
WHERE workspace_id = $1 AND workflow_id = $2
ORDER BY created_at DESC;

-- name: GetSchedule :one
SELECT * FROM schedule
WHERE id = $1;

-- name: GetScheduleInWorkspace :one
SELECT * FROM schedule
WHERE id = $1 AND workspace_id = $2;

-- name: CreateSchedule :one
INSERT INTO schedule (
    workspace_id, workflow_id, name, description,
    schedule_type, cron_expr, once_at, timezone,
    weekdays, time_of_day, next_run_at, enabled, created_by
) VALUES ($1, $2, $3, $4, $5, sqlc.narg(cron_expr), sqlc.narg(once_at), $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: UpdateSchedule :one
UPDATE schedule SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    workflow_id = COALESCE(sqlc.narg('workflow_id'), workflow_id),
    schedule_type = COALESCE(sqlc.narg('schedule_type'), schedule_type),
    cron_expr = COALESCE(sqlc.narg('cron_expr'), cron_expr),
    once_at = COALESCE(sqlc.narg('once_at'), once_at),
    timezone = COALESCE(sqlc.narg('timezone'), timezone),
    weekdays = COALESCE(sqlc.narg('weekdays'), weekdays),
    time_of_day = COALESCE(sqlc.narg('time_of_day'), time_of_day),
    next_run_at = COALESCE(sqlc.narg('next_run_at'), next_run_at),
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSchedule :exec
DELETE FROM schedule WHERE id = $1;

-- name: ToggleSchedule :one
UPDATE schedule SET enabled = NOT enabled, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetDueSchedules :many
SELECT * FROM schedule
WHERE enabled = true AND next_run_at <= now()
ORDER BY next_run_at ASC
LIMIT 100;

-- name: AdvanceScheduleNextRun :one
UPDATE schedule SET
    last_run_at = now(),
    next_run_at = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DisableSchedule :one
UPDATE schedule SET enabled = false, updated_at = now()
WHERE id = $1
RETURNING *;

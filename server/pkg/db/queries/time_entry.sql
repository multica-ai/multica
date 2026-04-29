-- name: CreateTimeEntry :one
INSERT INTO time_entry (
    workspace_id, issue_id, user_id, duration_minutes,
    activity_name, redmine_activity_id, comment, spent_on,
    sync_status, timer_started_at, timer_stopped_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetTimeEntry :one
SELECT * FROM time_entry
WHERE id = $1 AND workspace_id = $2;

-- name: ListTimeEntriesByIssue :many
SELECT * FROM time_entry
WHERE workspace_id = $1 AND issue_id = $2
ORDER BY spent_on DESC, created_at DESC;

-- name: ListTimeEntriesByUser :many
SELECT * FROM time_entry
WHERE workspace_id = $1 AND user_id = $2
  AND spent_on >= $3 AND spent_on <= $4
ORDER BY spent_on DESC, created_at DESC;

-- name: DeleteTimeEntry :exec
DELETE FROM time_entry
WHERE id = $1 AND workspace_id = $2;

-- name: UpdateTimeEntrySyncStatus :exec
UPDATE time_entry
SET external_time_entry_id = $3,
    sync_status            = $4,
    updated_at             = now()
WHERE id = $1 AND workspace_id = $2;

-- name: GetTotalTimeByIssue :one
SELECT COALESCE(SUM(duration_minutes), 0)::int AS total_minutes
FROM time_entry
WHERE workspace_id = $1 AND issue_id = $2;

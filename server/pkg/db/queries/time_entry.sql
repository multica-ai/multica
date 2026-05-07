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

-- name: UpdateTimeEntry :one
UPDATE time_entry
SET duration_minutes    = $3,
    activity_name       = $4,
    redmine_activity_id = $5,
    comment             = $6,
    spent_on            = $7,
    updated_at          = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ListFailedTimeEntries :many
SELECT * FROM time_entry
WHERE workspace_id = $1 AND sync_status IN ('failed', 'not_linked')
ORDER BY created_at DESC;

-- name: ListTimeEntriesByUserDateRange :many
SELECT te.*, i.number AS issue_number, i.title AS issue_title
FROM time_entry te
JOIN issue i ON i.id = te.issue_id
WHERE te.workspace_id = $1 AND te.user_id = $2
  AND te.spent_on >= $3 AND te.spent_on <= $4
ORDER BY te.spent_on DESC, te.created_at DESC;

-- name: GetDailyTimeByUser :many
SELECT spent_on, COALESCE(SUM(duration_minutes), 0)::int AS total_minutes
FROM time_entry
WHERE workspace_id = $1 AND user_id = $2
  AND spent_on >= $3 AND spent_on <= $4
GROUP BY spent_on
ORDER BY spent_on;

-- name: GetTimeByActivity :many
SELECT COALESCE(activity_name, 'Unspecified') AS activity,
       COALESCE(SUM(duration_minutes), 0)::int AS total_minutes
FROM time_entry
WHERE workspace_id = $1 AND user_id = $2
  AND spent_on >= $3 AND spent_on <= $4
GROUP BY activity_name
ORDER BY total_minutes DESC;

-- name: GetTimeByIssue :many
SELECT te.issue_id, i.number AS issue_number, i.title AS issue_title,
       COALESCE(SUM(te.duration_minutes), 0)::int AS total_minutes
FROM time_entry te
JOIN issue i ON i.id = te.issue_id
WHERE te.workspace_id = $1 AND te.user_id = $2
  AND te.spent_on >= $3 AND te.spent_on <= $4
GROUP BY te.issue_id, i.number, i.title
ORDER BY total_minutes DESC;

-- name: GetUserTimeOnDate :one
SELECT COALESCE(SUM(duration_minutes), 0)::int AS total_minutes
FROM time_entry
WHERE workspace_id = $1 AND user_id = $2 AND spent_on = $3;

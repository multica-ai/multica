-- name: CreateTimeEntry :one
INSERT INTO time_entry (workspace_id, user_id, issue_id, description, start_time, stop_time, duration_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetTimeEntryByID :one
SELECT * FROM time_entry
WHERE id = $1 AND workspace_id = $2;

-- name: ListTimeEntriesByUser :many
SELECT * FROM time_entry
WHERE workspace_id = $1 AND user_id = $2
ORDER BY start_time DESC
LIMIT $3 OFFSET $4;

-- name: ListTimeEntriesByIssue :many
SELECT * FROM time_entry
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY start_time DESC;

-- name: UpdateTimeEntry :one
UPDATE time_entry
SET description      = COALESCE($3, description),
    issue_id         = $4,
    start_time       = COALESCE($5, start_time),
    stop_time        = COALESCE($6, stop_time),
    duration_seconds = COALESCE($7, duration_seconds),
    updated_at       = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: StopTimeEntry :one
UPDATE time_entry
SET stop_time = $3,
    duration_seconds = $4,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteTimeEntry :exec
DELETE FROM time_entry
WHERE id = $1 AND workspace_id = $2;

-- name: SetRunningTimer :exec
INSERT INTO running_timer (user_id, time_entry_id, started_at)
VALUES ($1, $2, now())
ON CONFLICT (user_id) DO UPDATE
    SET time_entry_id = EXCLUDED.time_entry_id,
        started_at = EXCLUDED.started_at;

-- name: GetRunningTimerByUser :one
SELECT te.*
FROM running_timer rt
JOIN time_entry te ON te.id = rt.time_entry_id
WHERE rt.user_id = $1 AND te.workspace_id = $2;

-- name: ClearRunningTimerByUser :exec
DELETE FROM running_timer WHERE user_id = $1;

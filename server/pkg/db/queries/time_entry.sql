-- name: CreateTimeEntry :one
INSERT INTO time_entry (workspace_id, user_id, issue_id, description, start_time, stop_time, duration_seconds, type)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetTimeEntryByID :one
SELECT * FROM time_entry
WHERE id = $1 AND workspace_id = $2;

-- name: ListTimeEntriesByUser :many
SELECT * FROM time_entry
WHERE workspace_id = $1 AND user_id = $2
ORDER BY start_time DESC
LIMIT $3 OFFSET $4;

-- name: ListTimeEntriesByUserRange :many
-- Filters by start_time falling within [since, until) — ideal for day/week/month views.
SELECT * FROM time_entry
WHERE workspace_id = $1
  AND user_id = $2
  AND start_time >= $3
  AND start_time < $4
ORDER BY start_time DESC;

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

-- name: SumTimeEntriesByUserInWorkspace :many
-- Returns total stopped duration (seconds) grouped by user for all members in a workspace
-- within [since, until). Running entries (stop_time IS NULL) are excluded.
SELECT user_id, SUM(duration_seconds)::BIGINT AS total_seconds
FROM time_entry
WHERE workspace_id = $1
  AND start_time >= $2
  AND start_time < $3
  AND stop_time IS NOT NULL
GROUP BY user_id
ORDER BY total_seconds DESC;

-- name: SumTimeEntriesByProjectInWorkspace :many
-- Returns total stopped duration per project for a workspace within [since, until).
-- Entries not linked to any issue, or whose issues have no project, are grouped under NULL project_id.
SELECT i.project_id, SUM(te.duration_seconds)::BIGINT AS total_seconds
FROM time_entry te
LEFT JOIN issue i ON i.id = te.issue_id
WHERE te.workspace_id = $1
  AND te.start_time >= $2
  AND te.start_time < $3
  AND te.stop_time IS NOT NULL
GROUP BY i.project_id
ORDER BY total_seconds DESC;

-- name: SumTimeEntriesForProject :one
-- Returns the total stopped duration for all time entries linked to issues under a project.
SELECT COALESCE(SUM(te.duration_seconds), 0)::BIGINT AS total_seconds
FROM time_entry te
JOIN issue i ON i.id = te.issue_id
WHERE i.project_id = $1
  AND te.workspace_id = $2
  AND te.stop_time IS NOT NULL;

-- name: ListOverlappingStoppedTimeEntries :many
SELECT *
FROM time_entry
WHERE workspace_id = @workspace_id
  AND user_id = @user_id
  AND stop_time IS NOT NULL
  AND (@exclude_id::uuid IS NULL OR id <> @exclude_id)
  AND (
    tstzrange(start_time, stop_time, '[)') &&
    tstzrange(@range_start::timestamptz, @range_stop::timestamptz, '[)')
  )
ORDER BY start_time DESC;

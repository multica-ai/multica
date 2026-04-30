-- name: CreateWorkCalendar :one
INSERT INTO work_calendar (workspace_id, name, year, days, monthly_hours, source)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorkCalendar :one
SELECT * FROM work_calendar
WHERE id = $1 AND workspace_id = $2;

-- name: ListWorkCalendars :many
SELECT * FROM work_calendar
WHERE workspace_id = $1
ORDER BY year DESC, name ASC;

-- name: ListWorkCalendarsByYear :many
SELECT * FROM work_calendar
WHERE workspace_id = $1 AND year = $2
ORDER BY name ASC;

-- name: UpdateWorkCalendar :one
UPDATE work_calendar
SET name          = $3,
    year          = $4,
    days          = $5,
    monthly_hours = $6,
    updated_at    = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWorkCalendar :exec
DELETE FROM work_calendar
WHERE id = $1 AND workspace_id = $2;

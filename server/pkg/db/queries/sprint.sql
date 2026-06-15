-- name: CreateSprint :one
INSERT INTO sprint (workspace_id, project_id, name, goal, start_date, end_date, state)
VALUES ($1, $2, $3, $4, $5, $6, 'planning')
RETURNING *;

-- name: GetSprint :one
SELECT * FROM sprint WHERE id = $1;

-- name: GetSprintByWorkspace :one
SELECT * FROM sprint WHERE id = $1 AND workspace_id = $2;

-- name: ListSprints :many
SELECT * FROM sprint
WHERE workspace_id = $1 AND project_id = $2
ORDER BY created_at ASC;

-- name: UpdateSprint :one
UPDATE sprint
SET
    name       = COALESCE(sqlc.narg('name')::text, name),
    goal       = COALESCE(sqlc.narg('goal')::text, goal),
    start_date = COALESCE(sqlc.narg('start_date')::timestamptz, start_date),
    end_date   = COALESCE(sqlc.narg('end_date')::timestamptz, end_date),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: StartSprint :one
UPDATE sprint
SET state = 'active', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'planning'
RETURNING *;

-- name: CompleteSprint :one
UPDATE sprint
SET state = 'completed', updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'active'
RETURNING *;

-- name: GetActiveSprintForProject :one
SELECT * FROM sprint
WHERE project_id = $1 AND state = 'active'
LIMIT 1;

-- name: AddTicketToSprint :exec
UPDATE issue
SET sprint_id = $1, updated_at = now()
WHERE id = $2 AND workspace_id = $3;

-- name: RemoveTicketFromSprint :exec
UPDATE issue
SET sprint_id = NULL, updated_at = now()
WHERE id = $1 AND sprint_id = $2 AND workspace_id = $3;

-- name: CarryIncompleteToBacklog :exec
UPDATE issue
SET sprint_id = NULL, updated_at = now()
WHERE sprint_id = $1
  AND workspace_id = $2
  AND status NOT IN ('done', 'cancelled');

-- name: CarryIncompleteToSprint :exec
UPDATE issue
SET sprint_id = $2, updated_at = now()
WHERE sprint_id = $1
  AND workspace_id = $3
  AND status NOT IN ('done', 'cancelled');

-- name: ListBacklogIssues :many
SELECT id, workspace_id, title, description, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, start_date, due_date, created_at,
       updated_at, number, project_id, metadata, estimate, sprint_id
FROM issue
WHERE workspace_id = $1
  AND project_id = $2
  AND sprint_id IS NULL
ORDER BY position ASC, created_at ASC;

-- name: ListSprintIssues :many
SELECT id, workspace_id, title, description, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, start_date, due_date, created_at,
       updated_at, number, project_id, metadata, estimate, sprint_id
FROM issue
WHERE workspace_id = $1
  AND sprint_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetSprintVelocity :one
SELECT COALESCE(SUM(estimate), 0)::bigint AS velocity
FROM issue
WHERE sprint_id = $1
  AND workspace_id = $2
  AND status IN ('done', 'cancelled');

-- name: ListCompletedSprintVelocities :many
SELECT s.id, s.name, s.start_date, s.end_date,
       COALESCE(SUM(i.estimate) FILTER (WHERE i.status IN ('done', 'cancelled')), 0)::bigint AS completed_points,
       COALESCE(SUM(i.estimate), 0)::bigint AS total_points
FROM sprint s
LEFT JOIN issue i ON i.sprint_id = s.id AND i.workspace_id = s.workspace_id
WHERE s.project_id = $1
  AND s.workspace_id = $2
  AND s.state = 'completed'
GROUP BY s.id, s.name, s.start_date, s.end_date
ORDER BY s.end_date ASC;

-- name: GetSprintBurndown :many
-- Returns daily snapshot: per-day sum of estimates of issues that were
-- NOT yet done as of that day (approximated via updated_at of status change).
-- Simpler approach: return all issues with their estimate + status for client-side burndown
SELECT id, status, estimate, updated_at
FROM issue
WHERE sprint_id = $1
  AND workspace_id = $2;

-- name: ListProjects :many
-- include_archived defaults FALSE — archived projects are hidden from
-- the default list. Pass true to include them (used by an admin "Show
-- archived" toggle on the projects page).
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL)
ORDER BY created_at DESC;

-- name: GetProject :one
SELECT * FROM project
WHERE id = $1;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    icon = sqlc.narg('icon'),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM project WHERE id = $1;

-- name: ArchiveProject :one
-- Soft-delete: stamps archived_at + archived_by. Idempotent — re-archiving
-- a row that's already archived just refreshes the timestamp.
UPDATE project
SET archived_at = now(),
    archived_by = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RestoreProject :one
-- Reverses ArchiveProject. archived_by is cleared so the next archive
-- record reflects who actually re-archived.
UPDATE project
SET archived_at = NULL,
    archived_by = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done_count
FROM issue
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;

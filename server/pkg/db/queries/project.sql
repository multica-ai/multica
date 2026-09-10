-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
ORDER BY created_at DESC;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: LockProjectForChatSessionCreate :one
-- Conflicts with project deletion so a chat session cannot commit a soft
-- project reference after the delete transaction has swept existing sessions.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockProjectForDelete :one
-- Serializes project deletion with chat-session creation. The handler locks,
-- clears every soft chat reference, and deletes the project in one transaction.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority, start_date, due_date
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
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
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM project WHERE id = $1 AND workspace_id = $2;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE
           CASE WHEN sqlc.arg('lifecycle_enabled')::bool THEN
               CASE WHEN EXISTS (
                   SELECT 1 FROM issue_lifecycle_status AS coherent
                   WHERE coherent.id = i.lifecycle_status_id
                     AND coherent.lifecycle_id = i.lifecycle_id
                     AND coherent.workspace_id = i.workspace_id
                     AND coherent.legacy_status_key = i.status
               ) THEN EXISTS (
                   SELECT 1 FROM issue_lifecycle_status AS terminal
                   WHERE terminal.id = i.lifecycle_status_id
                     AND terminal.outcome IN ('completed', 'cancelled')
               ) ELSE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]) END
           ELSE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]) END
       )::bigint AS done_count
FROM issue AS i
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;

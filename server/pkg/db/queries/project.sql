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
    lead_type, lead_id, priority, start_date, due_date, issue_prefix, workflow_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = CASE WHEN sqlc.arg('description_set')::boolean THEN sqlc.narg('description') ELSE description END,
    icon = CASE WHEN sqlc.arg('icon_set')::boolean THEN sqlc.narg('icon') ELSE icon END,
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = CASE WHEN sqlc.arg('lead_type_set')::boolean THEN sqlc.narg('lead_type') ELSE lead_type END,
    lead_id = CASE WHEN sqlc.arg('lead_id_set')::boolean THEN sqlc.narg('lead_id') ELSE lead_id END,
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    start_date = CASE WHEN sqlc.arg('start_date_set')::boolean THEN sqlc.narg('start_date') ELSE start_date END,
    due_date = CASE WHEN sqlc.arg('due_date_set')::boolean THEN sqlc.narg('due_date') ELSE due_date END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM project WHERE id = $1 AND workspace_id = $2;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetIssueProjectLead :one
-- SHAR-155 routing rule 2: the project a notification's issue belongs to and
-- its lead, so a sender-routed inbox push can fall back to the lead member's
-- wecom binding. A project with no lead (lead_type/lead_id NULL) or an
-- agent lead returns a row with NULL lead fields — the caller reads
-- lead_type='member' to decide whether to use it at all. issue.project_id is
-- SET NULL on project delete, so a missing row is the "no project" case.
SELECT p.lead_type, p.lead_id
FROM issue i
JOIN project p ON p.id = i.project_id
WHERE i.id = sqlc.arg('issue_id');

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;

-- name: IncrementProjectIssueCounter :one
-- Serializes project-scoped issue creation on the project row, mirroring
-- IncrementIssueCounter for the workspace scope.
UPDATE project SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- name: ListProjectIssuePrefixes :many
-- Every project-generated prefix in a workspace, used to resolve identifiers
-- and to keep prefixes unique across the workspace.
SELECT id, issue_prefix FROM project
WHERE workspace_id = $1 AND issue_prefix <> '';

-- name: GetProjectByIssuePrefix :one
SELECT * FROM project
WHERE workspace_id = $1 AND issue_prefix <> '' AND issue_prefix = UPPER(sqlc.arg('issue_prefix')::text);

-- name: SetProjectIssueCounter :exec
UPDATE project SET issue_counter = $2 WHERE id = $1;

-- name: LockProjectForIssuePrefix :one
-- Serializes prefix changes and issue renumbering against concurrent creates
-- in the same project.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

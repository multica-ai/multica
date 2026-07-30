-- name: GetLatestIssueDescriptionVersion :one
-- The newest version row for an issue — the candidate "open" editing session
-- the next description write may coalesce into. Read once per description
-- write, so it rides idx_issue_description_version_issue.
SELECT * FROM issue_description_version
WHERE issue_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetIssueDescriptionVersion :one
-- Workspace-scoped so a version id from another workspace cannot be read by id.
SELECT * FROM issue_description_version
WHERE id = $1 AND workspace_id = $2;

-- name: ListIssueDescriptionVersions :many
-- Newest first for the history panel. DESC + LIMIT deliberately keeps the
-- NEWEST rows when an issue exceeds the cap; the timeline's activity query
-- makes the opposite (ASC) choice and silently drops recent entries, which is
-- tracked separately.
SELECT * FROM issue_description_version
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC
LIMIT $3;

-- name: CreateIssueDescriptionVersion :one
INSERT INTO issue_description_version (
    workspace_id, issue_id, parent_version_id, actor_type, actor_id,
    source_task_id, content, added_lines, removed_lines, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    COALESCE(sqlc.narg('created_at')::timestamptz, now()),
    COALESCE(sqlc.narg('created_at')::timestamptz, now())
)
RETURNING *;

-- name: UpdateIssueDescriptionVersionContent :one
-- Rewrites the open session row in place. The line counts are recomputed
-- against parent_version_id by the caller, never accumulated, so a session that
-- adds then deletes the same line settles back to zero.
UPDATE issue_description_version
SET content = $2, added_lines = $3, removed_lines = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteIssueDescriptionVersionsForIssue :exec
-- Explicit cleanup: the repo forbids ON DELETE CASCADE, so DeleteIssue calls
-- this alongside the issue row removal.
DELETE FROM issue_description_version WHERE issue_id = $1;

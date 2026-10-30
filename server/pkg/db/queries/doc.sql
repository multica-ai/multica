-- Knowledge base (MUL-8370). See migration 610 for the model.

-- name: LockDocTree :exec
-- Serializes folder/doc moves within one workspace so the cycle check and the
-- move it guards see the same tree.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('workspace_id')::uuid::text || ':doc_tree', 0));

-- name: ListDocFolders :many
SELECT * FROM doc_folder
WHERE workspace_id = $1
ORDER BY lower(title), created_at;

-- name: GetDocFolder :one
SELECT * FROM doc_folder
WHERE id = $1 AND workspace_id = $2;

-- name: CreateDocFolder :one
INSERT INTO doc_folder (workspace_id, parent_id, title, created_by_type, created_by_id)
VALUES (@workspace_id, sqlc.narg('parent_id'), @title, @created_by_type, @created_by_id)
RETURNING *;

-- name: RenameDocFolder :one
UPDATE doc_folder SET title = @title, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: MoveDocFolder :one
UPDATE doc_folder SET parent_id = sqlc.narg('parent_id'), updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: CountDocFolderChildren :one
SELECT (
    (SELECT count(*) FROM doc_folder f WHERE f.workspace_id = @workspace_id AND f.parent_id = @folder_id::uuid)
    + (SELECT count(*) FROM doc d WHERE d.workspace_id = @workspace_id AND d.folder_id = @folder_id::uuid)
)::int AS total;

-- name: DeleteDocFolder :exec
DELETE FROM doc_folder WHERE id = $1 AND workspace_id = $2;

-- name: ListDocs :many
-- Tree listing: metadata only, plus whether the doc changed since the caller
-- last opened it. A doc never opened counts from when the caller joined the
-- workspace, so a new member does not inherit the whole history as unread.
-- The caller's own edits never make a doc unread.
SELECT
    d.id, d.workspace_id, d.folder_id, d.format, d.title, d.revision, d.version_count,
    d.created_by_type, d.created_by_id, d.updated_by_type, d.updated_by_id,
    d.created_at, d.updated_at,
    EXISTS (
        SELECT 1 FROM doc_version v
        WHERE v.doc_id = d.id
          AND v.updated_at > COALESCE(r.seen_at, sqlc.arg('member_since')::timestamptz)
          AND NOT (v.author_type = 'member' AND v.author_id = sqlc.arg('user_id')::uuid)
    ) AS unread
FROM doc d
LEFT JOIN doc_read r ON r.doc_id = d.id AND r.user_id = sqlc.arg('user_id')::uuid
WHERE d.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY lower(d.title), d.created_at;

-- name: GetDoc :one
SELECT * FROM doc
WHERE id = $1 AND workspace_id = $2;

-- name: GetDocForUpdate :one
SELECT * FROM doc
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateDoc :one
INSERT INTO doc (
    workspace_id, folder_id, format, title, content,
    created_by_type, created_by_id, updated_by_type, updated_by_id
) VALUES (
    @workspace_id, sqlc.narg('folder_id'), @format, @title, @content,
    @author_type, @author_id, @author_type, @author_id
)
RETURNING *;

-- name: UpdateDocContent :one
UPDATE doc SET
    title = @title,
    content = @content,
    revision = revision + 1,
    version_count = @version_count,
    updated_by_type = @updated_by_type,
    updated_by_id = @updated_by_id,
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: MoveDoc :one
-- A move is not an edit: revision and updated_at stay put so an open editor
-- does not see a conflict and the doc does not jump in "recently edited".
UPDATE doc SET folder_id = sqlc.narg('folder_id')
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: DeleteDoc :exec
WITH v AS (
    DELETE FROM doc_version WHERE doc_version.doc_id = @id AND doc_version.workspace_id = @workspace_id
), r AS (
    DELETE FROM doc_read WHERE doc_read.doc_id = @id AND doc_read.workspace_id = @workspace_id
), p AS (
    DELETE FROM project_doc WHERE project_doc.doc_id = @id AND project_doc.workspace_id = @workspace_id
)
DELETE FROM doc WHERE doc.id = @id AND doc.workspace_id = @workspace_id;

-- name: GetDocVersion :one
SELECT * FROM doc_version
WHERE doc_id = $1 AND number = $2;

-- name: ListDocVersions :many
SELECT
    doc_id, workspace_id, number, title, author_type, author_id, message, action,
    action_ref, task_id, issue_id, lines_added, lines_removed, created_at, updated_at
FROM doc_version
WHERE doc_id = $1
ORDER BY number DESC
LIMIT $2;

-- name: InsertDocVersion :one
INSERT INTO doc_version (
    doc_id, workspace_id, number, title, content, author_type, author_id,
    message, action, action_ref, task_id, issue_id, lines_added, lines_removed
) VALUES (
    @doc_id, @workspace_id, @number, @title, @content, @author_type, @author_id,
    @message, @action, sqlc.narg('action_ref'), sqlc.narg('task_id'), sqlc.narg('issue_id'),
    @lines_added, @lines_removed
)
RETURNING *;

-- name: CoalesceDocVersion :one
-- Folds a follow-up edit into the newest version of the same editing session.
UPDATE doc_version SET
    title = @title,
    content = @content,
    lines_added = @lines_added,
    lines_removed = @lines_removed,
    updated_at = now()
WHERE doc_id = @doc_id AND number = @number
RETURNING *;

-- name: ListDocActivity :many
-- Recent versions across the workspace for the "recently updated" feed. The
-- client groups them per doc per day; content is never included.
SELECT
    v.doc_id, v.number, v.author_type, v.author_id, v.message, v.action, v.action_ref,
    v.task_id, v.issue_id, v.lines_added, v.lines_removed, v.created_at, v.updated_at,
    d.title AS doc_title, d.folder_id AS doc_folder_id, d.format AS doc_format,
    (
        v.updated_at > COALESCE(r.seen_at, sqlc.arg('member_since')::timestamptz)
        AND NOT (v.author_type = 'member' AND v.author_id = sqlc.arg('user_id')::uuid)
    )::boolean AS unread
FROM doc_version v
JOIN doc d ON d.id = v.doc_id
LEFT JOIN doc_read r ON r.doc_id = v.doc_id AND r.user_id = sqlc.arg('user_id')::uuid
WHERE v.workspace_id = sqlc.arg('workspace_id')::uuid
  AND v.updated_at >= sqlc.arg('since')::timestamptz
ORDER BY v.updated_at DESC
LIMIT sqlc.arg('max_rows');

-- name: GetDocRead :one
SELECT * FROM doc_read
WHERE doc_id = $1 AND user_id = $2;

-- name: MarkDocRead :one
INSERT INTO doc_read (doc_id, user_id, workspace_id, seen_at)
VALUES (@doc_id, @user_id, @workspace_id, now())
ON CONFLICT (doc_id, user_id) DO UPDATE SET seen_at = now()
RETURNING *;

-- name: ListProjectDocs :many
SELECT
    d.id, d.workspace_id, d.folder_id, d.format, d.title, d.revision, d.version_count,
    d.created_by_type, d.created_by_id, d.updated_by_type, d.updated_by_id,
    d.created_at, d.updated_at, pd.created_at AS linked_at
FROM project_doc pd
JOIN doc d ON d.id = pd.doc_id AND d.workspace_id = pd.workspace_id
WHERE pd.project_id = $1 AND pd.workspace_id = $2
ORDER BY pd.created_at;

-- name: ListDocProjectIDs :many
SELECT project_id FROM project_doc
WHERE doc_id = $1 AND workspace_id = $2
ORDER BY created_at;

-- name: LinkProjectDoc :execrows
INSERT INTO project_doc (project_id, doc_id, workspace_id, created_by_type, created_by_id)
VALUES (@project_id, @doc_id, @workspace_id, @created_by_type, @created_by_id)
ON CONFLICT (project_id, doc_id) DO NOTHING;

-- name: UnlinkProjectDoc :execrows
DELETE FROM project_doc
WHERE project_id = $1 AND doc_id = $2 AND workspace_id = $3;

-- name: DeleteProjectDocsByProject :exec
DELETE FROM project_doc WHERE project_id = $1 AND workspace_id = $2;

-- name: DeleteWorkspaceKnowledgeBase :exec
-- No foreign keys by project rule, so workspace teardown sweeps every
-- knowledge-base table here.
WITH v AS (
    DELETE FROM doc_version WHERE doc_version.workspace_id = @workspace_id
), r AS (
    DELETE FROM doc_read WHERE doc_read.workspace_id = @workspace_id
), p AS (
    DELETE FROM project_doc WHERE project_doc.workspace_id = @workspace_id
), f AS (
    DELETE FROM doc_folder WHERE doc_folder.workspace_id = @workspace_id
)
DELETE FROM doc WHERE doc.workspace_id = @workspace_id;

-- Workspace documents and folders.

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
SELECT id, workspace_id, folder_id, title, revision, version_count,
created_by_type, created_by_id, updated_by_type, updated_by_id, created_at, updated_at
FROM doc WHERE workspace_id = $1 ORDER BY lower(title), created_at;

-- name: GetDoc :one
SELECT * FROM doc
WHERE id = $1 AND workspace_id = $2;

-- name: GetDocForUpdate :one
SELECT * FROM doc
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateDoc :one
INSERT INTO doc (
    workspace_id, folder_id, title, content,
    created_by_type, created_by_id, updated_by_type, updated_by_id
) VALUES (
    @workspace_id, sqlc.narg('folder_id'), @title, @content,
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
WITH v AS (DELETE FROM doc_version WHERE doc_version.doc_id = @id AND doc_version.workspace_id = @workspace_id)
DELETE FROM doc WHERE doc.id = @id AND doc.workspace_id = @workspace_id;

-- name: GetDocVersion :one
SELECT * FROM doc_version
WHERE doc_id = $1 AND number = $2;

-- name: ListDocVersions :many
SELECT
    doc_id, workspace_id, number, title, author_type, author_id, message, action,
    action_ref, task_id, issue_id, created_at, updated_at
FROM doc_version
WHERE doc_id = $1
ORDER BY number DESC
LIMIT $2;

-- name: InsertDocVersion :one
INSERT INTO doc_version (
    doc_id, workspace_id, number, title, content, author_type, author_id,
    message, action, action_ref, task_id, issue_id
) VALUES (
    @doc_id, @workspace_id, @number, @title, @content, @author_type, @author_id,
    @message, @action, sqlc.narg('action_ref'), sqlc.narg('task_id'), sqlc.narg('issue_id')
)
RETURNING *;

-- name: DeleteWorkspaceDocs :exec
WITH v AS (DELETE FROM doc_version WHERE doc_version.workspace_id = @workspace_id),
f AS (DELETE FROM doc_folder WHERE doc_folder.workspace_id = @workspace_id)
DELETE FROM doc WHERE doc.workspace_id = @workspace_id;

-- Workspace Document CRUD

-- name: CreateWorkspaceDocument :one
INSERT INTO workspace_document (workspace_id, path, title, description, content, tags, pinned, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetWorkspaceDocumentByPath :one
SELECT * FROM workspace_document
WHERE workspace_id = $1 AND path = $2 AND archived_at IS NULL;

-- name: GetWorkspaceDocumentByID :one
SELECT * FROM workspace_document WHERE id = $1;

-- name: GetWorkspaceDocumentByIDForUpdate :one
-- Row lock acquired up-front in every mutation transaction so concurrent
-- writers can't both read the same MAX(revision_number) and collide on
-- the UNIQUE(document_id, revision_number) constraint.
SELECT * FROM workspace_document WHERE id = $1 FOR UPDATE;

-- name: ListWorkspaceDocuments :many
SELECT * FROM workspace_document
WHERE workspace_id = $1
  AND archived_at IS NULL
  AND ($2::text IS NULL OR path LIKE $2 || '%')
  AND ($3::text[] IS NULL OR tags && $3::text[])
ORDER BY path;

-- name: ListPinnedWorkspaceDocuments :many
SELECT * FROM workspace_document
WHERE workspace_id = $1 AND pinned = true AND archived_at IS NULL
ORDER BY path;

-- name: ListWorkspaceDocumentIndex :many
SELECT id, path, description, pinned FROM workspace_document
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY path;

-- name: UpdateWorkspaceDocumentContent :exec
UPDATE workspace_document
SET content = $2, title = $3, description = $4, tags = $5,
    current_revision_id = $6, updated_at = now()
WHERE id = $1;

-- name: SearchWorkspaceDocumentsByContent :many
SELECT id, workspace_id, path, title, description,
       ts_rank(to_tsvector('simple', content), plainto_tsquery('simple', $2)) AS rank
FROM workspace_document
WHERE workspace_id = $1
  AND archived_at IS NULL
  AND to_tsvector('simple', content) @@ plainto_tsquery('simple', $2)
ORDER BY rank DESC
LIMIT $3;

-- name: ArchiveWorkspaceDocument :exec
UPDATE workspace_document SET archived_at = now(), updated_at = now() WHERE id = $1;

-- name: RenameWorkspaceDocument :exec
UPDATE workspace_document SET path = $2, updated_at = now() WHERE id = $1;

-- name: SetWorkspaceDocumentPinned :exec
UPDATE workspace_document SET pinned = $2, updated_at = now() WHERE id = $1;

-- Revision CRUD

-- name: InsertWorkspaceDocumentRevision :one
INSERT INTO workspace_document_revision
    (document_id, revision_number, parent_revision, title, description, content, tags,
     author_type, author_id, task_id, operation, change_summary)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListWorkspaceDocumentRevisions :many
SELECT id, revision_number, author_type, author_id, task_id, operation,
       change_summary, created_at
FROM workspace_document_revision
WHERE document_id = $1
ORDER BY revision_number DESC;

-- name: GetWorkspaceDocumentRevision :one
SELECT * FROM workspace_document_revision
WHERE document_id = $1 AND revision_number = $2;

-- name: GetMaxRevisionNumber :one
SELECT COALESCE(MAX(revision_number), 0)::int FROM workspace_document_revision
WHERE document_id = $1;

-- name: UpdateWorkspaceDocumentRevisionContent :exec
-- Collapse a recent revision in-place (within the collapseRevisionWindow).
-- Author/operation/parent are preserved; created_at is bumped so the row
-- reflects the latest save and the collapse window is anchored to "now".
UPDATE workspace_document_revision
SET content = $2,
    title = $3,
    description = $4,
    tags = $5,
    change_summary = $6,
    created_at = now()
WHERE id = $1;

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
WITH v AS (DELETE FROM doc_version WHERE doc_version.doc_id = @id AND doc_version.workspace_id = @workspace_id),
c AS (DELETE FROM doc_comment WHERE doc_comment.doc_id = @id AND doc_comment.workspace_id = @workspace_id),
s AS (DELETE FROM doc_collab_state WHERE doc_collab_state.doc_id = @id AND doc_collab_state.workspace_id = @workspace_id),
r AS (DELETE FROM doc_revision WHERE doc_revision.doc_id = @id)
DELETE FROM doc WHERE doc.id = @id AND doc.workspace_id = @workspace_id;

-- name: GetDocVersion :one
SELECT * FROM doc_version
WHERE doc_id = $1 AND number = $2;

-- name: ListDocVersions :many
-- An edit an agent made while answering a discussion names that discussion
-- (through the run that made it), so history can link back to it.
SELECT
    v.doc_id, v.workspace_id, v.number, v.title, v.author_type, v.author_id, v.message, v.action,
    v.action_ref, v.task_id, v.issue_id, v.contributors, v.created_at, v.updated_at,
    c.id AS doc_thread_id, COALESCE(c.anchor_quote, '')::text AS doc_thread_quote
FROM doc_version v
LEFT JOIN agent_task_queue t ON t.id = v.task_id
LEFT JOIN doc_comment c ON c.id = t.doc_thread_id AND c.doc_id = v.doc_id AND c.parent_id IS NULL
WHERE v.doc_id = $1
ORDER BY v.number DESC
LIMIT $2;

-- name: MergeDocEditIntoVersion :execrows
-- Folds an edit into the latest snapshot while the same author keeps editing:
-- an unlabeled edit from the same author and task within 30 minutes of the
-- snapshot's start. Creates, restores and labeled edits always stand alone.
UPDATE doc_version SET
    title = @title,
    content = @content,
    updated_at = now()
WHERE doc_id = @doc_id
  AND number = @number
  AND action = 'edit'
  AND message = ''
  AND author_type = @author_type
  AND author_id = @author_id
  AND task_id IS NOT DISTINCT FROM sqlc.narg('task_id')
  AND created_at > now() - interval '30 minutes';

-- name: InsertDocVersion :one
INSERT INTO doc_version (
    doc_id, workspace_id, number, title, content, author_type, author_id,
    message, action, action_ref, task_id, issue_id, contributors
) VALUES (
    @doc_id, @workspace_id, @number, @title, @content, @author_type, @author_id,
    @message, @action, sqlc.narg('action_ref'), sqlc.narg('task_id'), sqlc.narg('issue_id'),
    @contributors::jsonb
)
RETURNING *;

-- name: DeleteWorkspaceDocs :exec
WITH v AS (DELETE FROM doc_version WHERE doc_version.workspace_id = @workspace_id),
s AS (DELETE FROM doc_collab_state WHERE doc_collab_state.doc_id IN (SELECT id FROM doc WHERE doc.workspace_id = @workspace_id)),
r AS (DELETE FROM doc_revision WHERE doc_revision.doc_id IN (SELECT id FROM doc WHERE doc.workspace_id = @workspace_id)),
c AS (DELETE FROM doc_comment WHERE doc_comment.workspace_id = @workspace_id),
f AS (DELETE FROM doc_folder WHERE doc_folder.workspace_id = @workspace_id)
DELETE FROM doc WHERE doc.workspace_id = @workspace_id;

-- name: ListDocComments :many
SELECT * FROM doc_comment
WHERE doc_id = @doc_id AND workspace_id = @workspace_id
ORDER BY created_at, id;

-- name: GetDocComment :one
SELECT * FROM doc_comment
WHERE id = @id AND doc_id = @doc_id AND workspace_id = @workspace_id;

-- name: CreateDocComment :one
INSERT INTO doc_comment (
    id, workspace_id, doc_id, parent_id, author_type, author_id, content,
    anchor_quote, anchor_prefix, anchor_suffix, source_task_id
) VALUES (
    @id, @workspace_id, @doc_id, sqlc.narg('parent_id'), @author_type, @author_id, @content,
    @anchor_quote, @anchor_prefix, @anchor_suffix, sqlc.narg('source_task_id')
)
RETURNING *;

-- name: SetDocCommentResolution :one
-- Resolves (or, with null fields, reopens) a discussion. Roots only.
UPDATE doc_comment SET
    resolved_at = sqlc.narg('resolved_at'),
    resolved_by_type = sqlc.narg('resolved_by_type'),
    resolved_by_id = sqlc.narg('resolved_by_id'),
    updated_at = now()
WHERE id = @id AND doc_id = @doc_id AND workspace_id = @workspace_id AND parent_id IS NULL
RETURNING *;

-- name: DeleteDocCommentThread :exec
-- Deletes a comment; deleting a root takes its replies with it.
DELETE FROM doc_comment
WHERE workspace_id = @workspace_id AND doc_id = @doc_id
  AND (id = @id OR parent_id = @id);

-- name: ListDocThreadParticipants :many
-- Members who wrote in a discussion: its root author and every reply author.
SELECT DISTINCT author_id FROM doc_comment
WHERE workspace_id = @workspace_id AND doc_id = @doc_id
  AND (id = @root_id OR parent_id = @root_id)
  AND author_type = 'member';

-- name: GetDocForCollab :one
-- The collaboration service's view of a doc: stored content plus the Yjs
-- state last saved with it, which is only reusable at the same revision.
SELECT d.workspace_id, d.title, d.content, d.revision,
    s.ystate AS ystate, s.revision AS ystate_revision
FROM doc d
LEFT JOIN doc_collab_state s ON s.doc_id = d.id
WHERE d.id = $1;

-- name: GetDocByIDForUpdate :one
SELECT * FROM doc WHERE id = $1 FOR UPDATE;

-- name: UpsertDocCollabState :exec
INSERT INTO doc_collab_state (doc_id, workspace_id, ystate, revision)
VALUES (@doc_id, @workspace_id, @ystate, @revision)
ON CONFLICT (doc_id) DO UPDATE SET
    ystate = EXCLUDED.ystate,
    revision = EXCLUDED.revision,
    updated_at = now();

-- name: InsertDocRevision :exec
INSERT INTO doc_revision (doc_id, revision, content)
VALUES (@doc_id, @revision, @content)
ON CONFLICT (doc_id, revision) DO UPDATE SET content = EXCLUDED.content;

-- name: PruneDocRevisions :exec
DELETE FROM doc_revision
WHERE doc_id = @doc_id AND revision < @keep_from;

-- name: GetDocRevisionContent :one
SELECT content FROM doc_revision
WHERE doc_id = @doc_id AND revision = @revision;

-- name: MergeCollabEditIntoVersion :execrows
-- Folds a save from people editing together into the latest snapshot while
-- the session lasts: an unlabeled member edit within 30 minutes of the
-- snapshot's start. Everyone who joined is added to its contributors.
UPDATE doc_version SET
    title = @title,
    content = @content,
    contributors = (
        SELECT COALESCE(jsonb_agg(DISTINCT e), '[]'::jsonb)
        FROM jsonb_array_elements(doc_version.contributors || @contributors::jsonb) AS e
    ),
    updated_at = now()
WHERE doc_id = @doc_id
  AND number = @number
  AND action = 'edit'
  AND message = ''
  AND author_type = 'member'
  AND task_id IS NULL
  AND created_at > now() - interval '30 minutes';

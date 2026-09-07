-- ListProjectNotes deliberately projects octet_length(body_md) instead of the
-- body itself. SELECT * would pull every note's full markdown across the
-- connection and allocate it in Go only for the handler to discard it, so the
-- "listing is cheap" property would hold at the HTTP boundary but not in the
-- database or the server's heap. octet_length matches Go's len() on the same
-- string: both count UTF-8 bytes.
-- name: ListProjectNotes :many
SELECT id,
       project_id,
       workspace_id,
       title,
       octet_length(body_md)::int AS body_size,
       position,
       created_by,
       created_at,
       updated_at
FROM project_note
WHERE project_id = $1
ORDER BY position ASC, created_at ASC;

-- name: GetProjectNote :one
SELECT * FROM project_note
WHERE id = $1;

-- name: GetProjectNoteInWorkspace :one
SELECT * FROM project_note
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProjectNote :one
INSERT INTO project_note (
    project_id, workspace_id, title, body_md, position, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateProjectNote :one
UPDATE project_note
SET title      = $2,
    body_md    = $3,
    position   = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- AppendProjectNote concatenates in SQL rather than read-modify-write so two
-- concurrent appends (an agent journalling while a human edits) cannot lose one
-- another's text. The separator is applied only when there is existing body, so
-- appending to a fresh note does not leave a leading blank line.
-- name: AppendProjectNote :one
UPDATE project_note
SET body_md = CASE
        WHEN body_md = '' THEN sqlc.arg('body_md')::text
        ELSE body_md || sqlc.arg('separator')::text || sqlc.arg('body_md')::text
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteProjectNote :exec
DELETE FROM project_note WHERE id = $1;

-- Application-level cascade: no FK exists, so project deletion must clear notes
-- explicitly inside the same transaction as the project delete.
-- name: DeleteProjectNotesByProject :exec
DELETE FROM project_note WHERE project_id = $1;

-- name: MaxProjectNotePosition :one
SELECT COALESCE(max(position), -1)::int AS max_position
FROM project_note
WHERE project_id = $1;

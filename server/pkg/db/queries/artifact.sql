-- name: CreateArtifact :one
INSERT INTO artifact (
    id, workspace_id, project_id, issue_id, kind, title, body, metadata,
    author_type, author_id
)
VALUES (
    $1, $2, sqlc.narg(project_id), sqlc.narg(issue_id), $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetArtifact :one
SELECT * FROM artifact
WHERE id = $1 AND workspace_id = $2;

-- name: ListArtifactsByIssue :many
SELECT * FROM artifact
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC;

-- name: ListArtifactsByProject :many
SELECT * FROM artifact
WHERE project_id = $1 AND workspace_id = $2
ORDER BY created_at DESC;

-- name: ListArtifactsByWorkspace :many
SELECT * FROM artifact
WHERE workspace_id = $1 AND project_id IS NULL AND issue_id IS NULL
ORDER BY created_at DESC;

-- name: UpdateArtifact :one
UPDATE artifact SET
    title = $2,
    body = $3,
    metadata = $4,
    updated_at = now()
WHERE id = $1 AND workspace_id = $5
RETURNING *;

-- name: DeleteArtifact :exec
DELETE FROM artifact WHERE id = $1 AND workspace_id = $2;

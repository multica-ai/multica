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

-- name: SearchArtifactsInWorkspace :many
-- Cross-scope search across the entire workspace. All filter parameters are
-- nullable; pass NULL to skip the filter. The scope filter values are:
-- 'workspace' (no project_id, no issue_id), 'project' (project_id IS NOT NULL),
-- 'issue' (issue_id IS NOT NULL), or NULL/'all' (no scope filter).
SELECT * FROM artifact
WHERE workspace_id = $1
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind'))
  AND (
        sqlc.narg('scope')::text IS NULL
     OR sqlc.narg('scope')::text = 'all'
     OR (sqlc.narg('scope')::text = 'workspace' AND project_id IS NULL AND issue_id IS NULL)
     OR (sqlc.narg('scope')::text = 'project'   AND project_id IS NOT NULL)
     OR (sqlc.narg('scope')::text = 'issue'     AND issue_id IS NOT NULL)
  )
  AND (
        sqlc.narg('q')::text IS NULL
     OR title ILIKE '%' || sqlc.narg('q')::text || '%'
     OR body  ILIKE '%' || sqlc.narg('q')::text || '%'
  )
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateArtifactScope :one
-- Re-scope an artifact (workspace ↔ project ↔ issue). At most one of
-- project_id/issue_id may be non-null; both null = workspace scope.
UPDATE artifact SET
    project_id = sqlc.narg('project_id'),
    issue_id   = sqlc.narg('issue_id'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

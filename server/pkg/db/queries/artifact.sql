-- name: CreateArtifact :one
INSERT INTO artifact (
    id, workspace_id, project_id, issue_id, folder_id, origin_issue_id,
    kind, format, title, body, file_url, file_size_bytes, metadata,
    author_type, author_id, requester_user_id
)
VALUES (
    $1, $2,
    sqlc.narg(project_id), sqlc.narg(issue_id), sqlc.narg(folder_id),
    sqlc.narg(origin_issue_id),
    $3, $4, $5, $6,
    sqlc.narg(file_url), sqlc.narg(file_size_bytes),
    $7, $8, $9,
    sqlc.narg(requester_user_id)
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

-- name: ListArtifactsByFolder :many
-- Lists artifacts inside a specific folder (or root when folder_id is NULL).
-- Includes artifacts of any scope (workspace/project/issue) — folders are
-- orthogonal to scope.
SELECT * FROM artifact
WHERE workspace_id = $1
  AND (
        (sqlc.narg('folder_id')::uuid IS NULL AND folder_id IS NULL)
     OR (folder_id = sqlc.narg('folder_id'))
  )
ORDER BY title ASC;

-- name: UpdateArtifact :one
UPDATE artifact SET
    title = $2,
    body = $3,
    metadata = $4,
    file_url = sqlc.narg(file_url),
    file_size_bytes = sqlc.narg(file_size_bytes),
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
-- The author_type filter narrows by 'member', 'agent', or NULL/'all'.
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
        sqlc.narg('author_type')::text IS NULL
     OR sqlc.narg('author_type')::text = 'all'
     OR author_type = sqlc.narg('author_type')
  )
  AND (sqlc.narg('author_id')::uuid IS NULL OR author_id = sqlc.narg('author_id'))
  AND (
        sqlc.narg('origin_issue_id')::uuid IS NULL
     OR origin_issue_id = sqlc.narg('origin_issue_id')
     OR issue_id = sqlc.narg('origin_issue_id')
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

-- name: MoveArtifactToFolder :one
-- Move an artifact to a different folder (or to root when folder_id is NULL).
UPDATE artifact SET
    folder_id  = sqlc.narg('folder_id'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

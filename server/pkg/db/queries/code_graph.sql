-- name: CreateCodeGraphRepository :one
INSERT INTO code_graph_repository (workspace_id, repo_url, repo_name, branch, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCodeGraphRepositoryByIdentity :one
SELECT * FROM code_graph_repository
WHERE workspace_id = $1 AND repo_url = $2 AND branch = $3;

-- name: GetCodeGraphRepository :one
SELECT * FROM code_graph_repository
WHERE id = $1 AND workspace_id = $2;

-- name: ListCodeGraphRepositories :many
SELECT * FROM code_graph_repository
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (
    sqlc.arg('status')::text = ''
    OR status = sqlc.arg('status')::text
  )
  AND (
    sqlc.arg('search')::text = ''
    OR repo_name ILIKE '%' || sqlc.arg('search')::text || '%'
    OR branch ILIKE '%' || sqlc.arg('search')::text || '%'
    OR repo_url ILIKE '%' || sqlc.arg('search')::text || '%'
    OR id::text = sqlc.arg('search')::text
  )
ORDER BY updated_at DESC, repo_name ASC;

-- name: CountCodeGraphRepositoriesByStatus :many
SELECT status, count(*)::int AS count
FROM code_graph_repository
WHERE workspace_id = $1
GROUP BY status;

-- name: MarkCodeGraphRepositoryProcessing :one
UPDATE code_graph_repository
SET status = 'processing',
    internal_status = $3,
    sync_error = NULL,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: MarkCodeGraphRepositoryReady :one
UPDATE code_graph_repository
SET status = 'ready',
    internal_status = $3,
    commit_hash = $4,
    file_count = $5,
    sync_error = NULL,
    last_indexed_at = now(),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: MarkCodeGraphRepositoryFailed :one
UPDATE code_graph_repository
SET status = 'failed',
    internal_status = $3,
    sync_error = $4,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteCodeGraphRepository :execrows
DELETE FROM code_graph_repository
WHERE id = $1 AND workspace_id = $2;

-- CEREBRO-PATCH(sqlc-artifact-folder): cerebro modification of upstream file
-- name: CreateArtifactFolder :one
INSERT INTO artifact_folder (id, workspace_id, parent_id, name)
VALUES ($1, $2, sqlc.narg(parent_id), $3)
RETURNING *;

-- name: GetArtifactFolder :one
SELECT * FROM artifact_folder
WHERE id = $1 AND workspace_id = $2;

-- name: ListArtifactFoldersByWorkspace :many
SELECT * FROM artifact_folder
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: ListArtifactFoldersByParent :many
SELECT * FROM artifact_folder
WHERE workspace_id = $1
  AND (
        (sqlc.narg('parent_id')::uuid IS NULL AND parent_id IS NULL)
     OR (parent_id = sqlc.narg('parent_id'))
  )
ORDER BY name ASC;

-- name: UpdateArtifactFolder :one
UPDATE artifact_folder SET
    name = $2,
    parent_id = sqlc.narg(parent_id),
    updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: DeleteArtifactFolder :exec
-- ON DELETE CASCADE on parent_id removes nested folders. Artifacts use
-- ON DELETE SET NULL so they fall back to root.
DELETE FROM artifact_folder
WHERE id = $1 AND workspace_id = $2;

-- CEREBRO-PATCH(sqlc-artifact-folder): cerebro modification of upstream file
-- name: CreateArtifactFolder :one
-- CEREBRO-PATCH(folder-access-owner): FIR-1590 — record the creator as owner so
-- the folder can be made private/shared. NULL owner = legacy/workspace folder.
INSERT INTO artifact_folder (id, workspace_id, parent_id, name, kind, owner_id)
VALUES ($1, $2, sqlc.narg(parent_id), $3, $4, sqlc.narg(owner_id))
RETURNING *;

-- name: GetArtifactFolder :one
SELECT * FROM artifact_folder
WHERE id = $1 AND workspace_id = $2;

-- name: ListArtifactFoldersByWorkspace :many
-- CEREBRO-PATCH(artifact-folder-kind): TECH-3637 — optional kind filter scopes
-- the folder list to one surface so notes and documents don't mix.
-- CEREBRO-PATCH(folder-access-list): FIR-1590 — only return folders the user is
-- allowed to see (own + workspace + shared-with-them), gated up the tree.
SELECT * FROM artifact_folder
WHERE workspace_id = $1
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind'))
  AND cerebro_artifact_folder_visible(id, sqlc.arg('user_id')::uuid)
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

-- CEREBRO-PATCH(folder-access-queries): FIR-1590 — folder-level access control.
-- name: SetArtifactFolderVisibility :exec
-- Legacy folders predate ownership (owner_id NULL); the first member to set
-- access on one claims ownership so future changes are gated to them.
UPDATE artifact_folder
SET visibility = $2,
    owner_id = COALESCE(owner_id, sqlc.arg('owner_id')),
    updated_at = now()
WHERE id = $1 AND workspace_id = $3;

-- name: AddArtifactFolderShare :exec
INSERT INTO cerebro_artifact_folder_share (folder_id, user_id)
VALUES ($1, $2)
ON CONFLICT (folder_id, user_id) DO NOTHING;

-- name: RemoveArtifactFolderShare :exec
DELETE FROM cerebro_artifact_folder_share WHERE folder_id = $1 AND user_id = $2;

-- name: ReplaceArtifactFolderShares :exec
-- Clears the share list for a folder; callers re-add the desired users.
DELETE FROM cerebro_artifact_folder_share WHERE folder_id = $1;

-- name: ListArtifactFolderShares :many
SELECT user_id, created_at
FROM cerebro_artifact_folder_share
WHERE folder_id = $1
ORDER BY created_at;

-- name: CanUserSeeArtifactFolder :one
-- True when the user may see this folder under the access rule (gated up the
-- whole folder chain). Used for single-folder checks in handlers.
SELECT cerebro_artifact_folder_visible($1, $2::uuid) AS allowed;

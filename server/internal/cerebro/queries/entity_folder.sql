-- FIR-1412: Folders + membership for the Skills and Autopilots interfaces.
-- Every query is workspace-scoped and most are kind-scoped ('skill' or
-- 'autopilot') so the two surfaces share one table without mixing trees.

-- name: CreateCerebroEntityFolder :one
INSERT INTO cerebro_entity_folder (workspace_id, kind, name, parent_id)
VALUES ($1, $2, $3, sqlc.narg(parent_id))
RETURNING *;

-- name: GetCerebroEntityFolder :one
SELECT * FROM cerebro_entity_folder
WHERE id = $1 AND workspace_id = $2;

-- name: ListCerebroEntityFoldersByWorkspace :many
SELECT * FROM cerebro_entity_folder
WHERE workspace_id = $1 AND kind = $2
ORDER BY name ASC;

-- name: UpdateCerebroEntityFolder :one
UPDATE cerebro_entity_folder
SET name = $3,
    parent_id = sqlc.narg(parent_id),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteCerebroEntityFolder :exec
-- ON DELETE CASCADE on parent_id removes nested sub-folders; ON DELETE CASCADE
-- on the membership table drops memberships so the items fall back to root.
DELETE FROM cerebro_entity_folder
WHERE id = $1 AND workspace_id = $2;

-- name: ListCerebroEntityFolderItemsByWorkspace :many
-- The folder membership for every filed item of this kind in the workspace.
SELECT * FROM cerebro_entity_folder_item
WHERE workspace_id = $1 AND kind = $2;

-- name: SetCerebroEntityFolderItem :exec
-- File an item into a folder (move). Upsert keeps an item in at most one folder.
INSERT INTO cerebro_entity_folder_item (workspace_id, kind, item_id, folder_id, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (kind, item_id)
DO UPDATE SET folder_id = EXCLUDED.folder_id,
              workspace_id = EXCLUDED.workspace_id,
              updated_at = now();

-- name: DeleteCerebroEntityFolderItem :exec
-- Unfile an item (move back to root).
DELETE FROM cerebro_entity_folder_item
WHERE workspace_id = $1 AND kind = $2 AND item_id = $3;

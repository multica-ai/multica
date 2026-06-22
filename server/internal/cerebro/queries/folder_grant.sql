-- Collections, Phase 1 — per-folder access grants. surface is 'artifact'
-- (Documents/Notes) or 'entity' (Autopilots/Skills); the grant table spans both
-- folder trees. The access editor reads Direct (Valgt her) + Effective
-- (Valgt her + Arvet) and writes via Upsert/Remove.

-- name: ListCerebroFolderDirectGrants :many
-- Grants set directly on this folder — the "Valgt her" tab.
SELECT * FROM cerebro_folder_grant
WHERE surface = $1 AND folder_id = $2
ORDER BY grantee_type, created_at;

-- name: ListCerebroArtifactFolderEffectiveGrants :many
-- Direct + inherited grants for a Documents/Notes folder. Walks the folder's
-- ancestors; is_direct = false means the grant comes from an ancestor (the
-- "Arvet" tab) and source_folder_id is where it lives. depth 0 = the folder
-- itself, so the caller can pick the nearest grant per grantee.
WITH RECURSIVE chain AS (
    SELECT f.id, f.parent_id, 0 AS depth
    FROM artifact_folder f
    WHERE f.id = $1
    UNION ALL
    SELECT f.id, f.parent_id, c.depth + 1
    FROM artifact_folder f
    JOIN chain c ON f.id = c.parent_id
)
SELECT g.grantee_type, g.grantee_id, g.role,
       g.folder_id AS source_folder_id,
       (c.depth = 0) AS is_direct,
       c.depth AS depth
FROM chain c
JOIN cerebro_folder_grant g ON g.surface = 'artifact' AND g.folder_id = c.id;

-- name: ListCerebroEntityFolderEffectiveGrants :many
-- Same as above for an Autopilots/Skills folder (cerebro_entity_folder tree).
WITH RECURSIVE chain AS (
    SELECT f.id, f.parent_id, 0 AS depth
    FROM cerebro_entity_folder f
    WHERE f.id = $1
    UNION ALL
    SELECT f.id, f.parent_id, c.depth + 1
    FROM cerebro_entity_folder f
    JOIN chain c ON f.id = c.parent_id
)
SELECT g.grantee_type, g.grantee_id, g.role,
       g.folder_id AS source_folder_id,
       (c.depth = 0) AS is_direct,
       c.depth AS depth
FROM chain c
JOIN cerebro_folder_grant g ON g.surface = 'entity' AND g.folder_id = c.id;

-- name: UpsertCerebroFolderGrant :one
-- Add or change a grant on a folder. The conflict target matches the partial
-- expression unique index (workspace grant folds its NULL id into the zero uuid).
INSERT INTO cerebro_folder_grant (surface, folder_id, grantee_type, grantee_id, role, created_by)
VALUES ($1, $2, $3, sqlc.narg(grantee_id), $4, sqlc.narg(created_by))
ON CONFLICT (surface, folder_id, grantee_type,
             COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO UPDATE SET role = EXCLUDED.role
RETURNING *;

-- name: RemoveCerebroFolderGrant :exec
-- Remove a single direct grant from a folder.
DELETE FROM cerebro_folder_grant
WHERE surface = $1 AND folder_id = $2 AND grantee_type = $3
  AND COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid)
    = COALESCE(sqlc.narg(grantee_id), '00000000-0000-0000-0000-000000000000'::uuid);

-- name: DeleteAllCerebroFolderGrants :exec
-- Drop every direct grant on a folder (used when a folder is deleted, since the
-- grant table has no FK across the two physical folder trees).
DELETE FROM cerebro_folder_grant
WHERE surface = $1 AND folder_id = $2;

-- name: GetCerebroArtifactFolderWorkspace :one
-- Used to confirm an artifact (Documents/Notes) folder belongs to the caller's
-- workspace before listing or changing its grants.
SELECT workspace_id FROM artifact_folder WHERE id = $1;

-- name: GetCerebroEntityFolderWorkspace :one
-- Same ownership check for an Autopilots/Skills folder.
SELECT workspace_id FROM cerebro_entity_folder WHERE id = $1;

-- FIR-1659 Fase 2 + 3: saved filters and their sharing. All queries live in the
-- cerebro queries package so they generate into cerebrodb alongside the other
-- cerebro-only tables (tables: cerebro_saved_filters migration 9091,
-- cerebro_saved_filter_shares migration 9093).

-- name: ListCerebroSavedFiltersByOwner :many
SELECT id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at, visibility
FROM cerebro_saved_filters
WHERE workspace_id = $1 AND owner_id = $2
ORDER BY position ASC, created_at ASC;

-- ListCerebroSavedFiltersVisibleToUser returns every filter the member may use
-- in the workspace: their own (any visibility), every 'workspace' filter, and
-- every 'shared' filter targeting them directly or via a group they belong to.
-- owner_name rides along so the UI can label "shared by …" without a second
-- round-trip.
-- name: ListCerebroSavedFiltersVisibleToUser :many
SELECT sf.id, sf.workspace_id, sf.owner_id, u.name AS owner_name,
       sf.name, sf.surface, sf.filter_state, sf.position, sf.visibility,
       sf.created_at, sf.updated_at
FROM cerebro_saved_filters sf
JOIN "user" u ON u.id = sf.owner_id
WHERE sf.workspace_id = $1
  AND (
    sf.owner_id = $2
    OR sf.visibility = 'workspace'
    OR (sf.visibility = 'shared' AND EXISTS (
        SELECT 1
        FROM cerebro_saved_filter_shares sh
        WHERE sh.saved_filter_id = sf.id
          AND (
            (sh.target_type = 'member' AND sh.target_id = $2)
            OR (sh.target_type = 'group' AND sh.target_id IN (
                SELECT gm.group_id FROM cerebro_group_member gm WHERE gm.user_id = $2
            ))
          )
    ))
  )
ORDER BY sf.position ASC, sf.created_at ASC;

-- name: GetCerebroSavedFilter :one
SELECT id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at, visibility
FROM cerebro_saved_filters
WHERE id = $1;

-- name: CreateCerebroSavedFilter :one
INSERT INTO cerebro_saved_filters (workspace_id, owner_id, name, surface, filter_state, position, visibility)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at, visibility;

-- name: UpdateCerebroSavedFilter :one
UPDATE cerebro_saved_filters
SET name = $2, filter_state = $3, position = $4, visibility = $5, updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at, visibility;

-- name: DeleteCerebroSavedFilter :exec
DELETE FROM cerebro_saved_filters
WHERE id = $1;

-- Fase 3: share rows. Sharing is replace-all — the handler clears the existing
-- rows then inserts the new target set inside one request.

-- name: ListCerebroSavedFilterShares :many
SELECT saved_filter_id, target_type, target_id, created_by, created_at
FROM cerebro_saved_filter_shares
WHERE saved_filter_id = $1
ORDER BY created_at ASC;

-- name: DeleteCerebroSavedFilterShares :exec
DELETE FROM cerebro_saved_filter_shares
WHERE saved_filter_id = $1;

-- name: CreateCerebroSavedFilterShare :exec
INSERT INTO cerebro_saved_filter_shares (saved_filter_id, target_type, target_id, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (saved_filter_id, target_type, target_id) DO NOTHING;

-- Fase 4: the "may create shared filters" gate ORs the group capability with an
-- admin/owner override. GetCerebroMemberRole reads the upstream membership row
-- so the handler (which only holds cerebroQueries) can resolve that override.
-- name: GetCerebroMemberRole :one
SELECT role
FROM member
WHERE workspace_id = $1 AND user_id = $2;

-- FIR-1659 Fase 2: personal saved filters. All queries live in the cerebro
-- queries package so they generate into cerebrodb alongside the other
-- cerebro-only tables (table: cerebro_saved_filters, migration 9091).

-- name: ListCerebroSavedFiltersByOwner :many
SELECT id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at
FROM cerebro_saved_filters
WHERE workspace_id = $1 AND owner_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetCerebroSavedFilter :one
SELECT id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at
FROM cerebro_saved_filters
WHERE id = $1;

-- name: CreateCerebroSavedFilter :one
INSERT INTO cerebro_saved_filters (workspace_id, owner_id, name, surface, filter_state, position)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at;

-- name: UpdateCerebroSavedFilter :one
UPDATE cerebro_saved_filters
SET name = $2, filter_state = $3, position = $4, updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, owner_id, name, surface, filter_state, position, created_at, updated_at;

-- name: DeleteCerebroSavedFilter :exec
DELETE FROM cerebro_saved_filters
WHERE id = $1;

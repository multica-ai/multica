-- name: ListGlobalModelTierMap :many
SELECT workspace_id, tier, concrete, fallback_concrete FROM model_tier_map WHERE workspace_id IS NULL;

-- name: ListWorkspaceModelTierMap :many
SELECT workspace_id, tier, concrete, fallback_concrete FROM model_tier_map WHERE workspace_id = $1;

-- name: GetWorkspaceModelTier :one
SELECT workspace_id, tier, concrete, fallback_concrete FROM model_tier_map WHERE workspace_id = $1 AND tier = $2;

-- name: GetGlobalModelTier :one
SELECT workspace_id, tier, concrete, fallback_concrete FROM model_tier_map WHERE workspace_id IS NULL AND tier = $1;

-- name: UpsertWorkspaceModelTier :one
INSERT INTO model_tier_map (workspace_id, tier, concrete, fallback_concrete) VALUES ($1, $2, $3, COALESCE(sqlc.narg('fallback_concrete')::text[], '{}'::text[]))
ON CONFLICT (workspace_id, tier) DO UPDATE SET concrete = EXCLUDED.concrete, fallback_concrete = COALESCE(sqlc.narg('fallback_concrete')::text[], model_tier_map.fallback_concrete)
RETURNING workspace_id, tier, concrete, fallback_concrete;

-- name: UpsertGlobalModelTier :one
INSERT INTO model_tier_map (workspace_id, tier, concrete, fallback_concrete) VALUES (NULL, $1, $2, COALESCE(sqlc.narg('fallback_concrete')::text[], '{}'::text[]))
ON CONFLICT (workspace_id, tier) DO UPDATE SET concrete = EXCLUDED.concrete, fallback_concrete = COALESCE(sqlc.narg('fallback_concrete')::text[], model_tier_map.fallback_concrete)
RETURNING workspace_id, tier, concrete, fallback_concrete;

-- name: DeleteWorkspaceModelTier :exec
DELETE FROM model_tier_map WHERE workspace_id = $1 AND tier = $2;

-- name: DeleteGlobalModelTier :exec
DELETE FROM model_tier_map WHERE workspace_id IS NULL AND tier = $1;

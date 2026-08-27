-- name: ListGlobalModelTierMap :many
SELECT workspace_id, tier, concrete FROM model_tier_map WHERE workspace_id IS NULL;

-- name: ListWorkspaceModelTierMap :many
SELECT workspace_id, tier, concrete FROM model_tier_map WHERE workspace_id = $1;

-- name: GetWorkspaceModelTier :one
SELECT workspace_id, tier, concrete FROM model_tier_map WHERE workspace_id = $1 AND tier = $2;

-- name: GetGlobalModelTier :one
SELECT workspace_id, tier, concrete FROM model_tier_map WHERE workspace_id IS NULL AND tier = $1;

-- name: UpsertWorkspaceModelTier :one
INSERT INTO model_tier_map (workspace_id, tier, concrete) VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, tier) DO UPDATE SET concrete = EXCLUDED.concrete
RETURNING workspace_id, tier, concrete;

-- name: UpsertGlobalModelTier :one
INSERT INTO model_tier_map (workspace_id, tier, concrete) VALUES (NULL, $1, $2)
ON CONFLICT (workspace_id, tier) DO UPDATE SET concrete = EXCLUDED.concrete
RETURNING workspace_id, tier, concrete;

-- name: DeleteWorkspaceModelTier :exec
DELETE FROM model_tier_map WHERE workspace_id = $1 AND tier = $2;

-- name: DeleteGlobalModelTier :exec
DELETE FROM model_tier_map WHERE workspace_id IS NULL AND tier = $1;

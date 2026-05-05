-- name: GetCerebroFeatureFlag :one
SELECT enabled FROM cerebro_feature_flags
WHERE workspace_id = $1 AND user_id = $2 AND flag_key = $3;

-- name: ListCerebroFeatureFlags :many
SELECT flag_key, enabled FROM cerebro_feature_flags
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertCerebroFeatureFlag :exec
INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE
SET enabled = EXCLUDED.enabled, updated_at = NOW();

-- name: DeleteCerebroFeatureFlag :exec
DELETE FROM cerebro_feature_flags
WHERE workspace_id = $1 AND user_id = $2 AND flag_key = $3;

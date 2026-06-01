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

-- Workspace-level overrides live in the same table under the all-zero
-- sentinel user_id. `locked` decides whether members may still override.
--
-- LANDMINE: this sentinel scheme depends on cerebro_feature_flags.user_id
-- having NO foreign key to "user" (see migration 9014 — composite PK only).
-- Do NOT add a user_id FK later, or the all-zero workspace row stops inserting.
-- The literal '00000000-0000-0000-0000-000000000000' is mirrored in the three
-- queries below (and the feature_flags handler/test); keep them in sync.

-- name: ListCerebroWorkspaceFeatureFlags :many
SELECT flag_key, enabled, locked FROM cerebro_feature_flags
WHERE workspace_id = $1 AND user_id = '00000000-0000-0000-0000-000000000000';

-- name: UpsertCerebroWorkspaceFeatureFlag :exec
INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled, locked)
VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3, $4)
ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE
SET enabled = EXCLUDED.enabled, locked = EXCLUDED.locked, updated_at = NOW();

-- name: DeleteCerebroWorkspaceFeatureFlag :exec
DELETE FROM cerebro_feature_flags
WHERE workspace_id = $1 AND user_id = '00000000-0000-0000-0000-000000000000' AND flag_key = $2;

-- name: GetCerebroWorkspaceAuthSettings :one
SELECT * FROM cerebro_workspace_auth_settings
WHERE workspace_id = $1;

-- name: UpsertCerebroWorkspaceAuthSettings :one
INSERT INTO cerebro_workspace_auth_settings (
    workspace_id, google_signup_domains, default_role,
    google_workspace_sync_enabled, updated_by_user_id, updated_at
)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (workspace_id) DO UPDATE
SET google_signup_domains         = EXCLUDED.google_signup_domains,
    default_role                  = EXCLUDED.default_role,
    google_workspace_sync_enabled = EXCLUDED.google_workspace_sync_enabled,
    updated_by_user_id            = EXCLUDED.updated_by_user_id,
    updated_at                    = NOW()
RETURNING *;

-- name: ListWorkspacesWithGoogleWorkspaceSync :many
-- Every workspace that has opted the BigQuery group sync ON. The sync worker
-- iterates this list once per tick.
SELECT workspace_id FROM cerebro_workspace_auth_settings
WHERE google_workspace_sync_enabled = true;

-- name: ListCerebroWorkspaceAuthSettingsForDomain :many
-- Returns every workspace that has the given lowercased email domain in its
-- google_signup_domains array. Used by the auto-provisioning hook to fan a
-- single new user out across all matching workspaces.
--
-- The `sqlc.arg(domain)::text` cast is load-bearing: without an explicit
-- type, sqlc infers `text[]` from the `= ANY(text[])` shape and the
-- generated signature becomes `[]string`, which pgx then fails to encode
-- into the single-text parameter at runtime (`OID 25: cannot find encode
-- plan`). Casting forces a `text` parameter and a `string` Go signature.
SELECT * FROM cerebro_workspace_auth_settings
WHERE sqlc.arg(domain)::text = ANY (google_signup_domains);

-- name: DeleteCerebroWorkspaceAuthSettings :exec
DELETE FROM cerebro_workspace_auth_settings
WHERE workspace_id = $1;

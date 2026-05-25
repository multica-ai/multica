-- name: GetUserInfisicalIdentity :one
SELECT id, workspace_id, user_id, identity_id, client_id, client_secret_ciphertext, scoped_paths_hash, created_at, updated_at
FROM cerebro_user_infisical_identity
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertUserInfisicalIdentity :one
INSERT INTO cerebro_user_infisical_identity (
    workspace_id,
    user_id,
    identity_id,
    client_id,
    client_secret_ciphertext,
    scoped_paths_hash
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET
    identity_id = EXCLUDED.identity_id,
    client_id = EXCLUDED.client_id,
    client_secret_ciphertext = EXCLUDED.client_secret_ciphertext,
    scoped_paths_hash = EXCLUDED.scoped_paths_hash,
    updated_at = now()
RETURNING id, workspace_id, user_id, identity_id, client_id, client_secret_ciphertext, scoped_paths_hash, created_at, updated_at;

-- name: UpdateUserInfisicalIdentityScope :exec
UPDATE cerebro_user_infisical_identity
SET scoped_paths_hash = $3,
    updated_at = now()
WHERE workspace_id = $1 AND user_id = $2;

-- name: DeleteUserInfisicalIdentity :exec
DELETE FROM cerebro_user_infisical_identity
WHERE workspace_id = $1 AND user_id = $2;

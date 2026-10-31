-- =====================
-- Personal GitHub account (MUL-8433)
--
-- One row per Multica user: the GitHub App user-to-server authorization that
-- PR writes are performed with. Token columns hold secretbox ciphertext.
-- =====================

-- name: GetGitHubUserConnection :one
SELECT * FROM github_user_connection WHERE user_id = $1;

-- name: LockGitHubUserConnection :one
-- GitHub rotates the refresh token on every use, so two refreshes racing on
-- the same token would leave one of them holding a revoked token. Refreshes
-- run under this row lock and re-check expiry once they hold it.
SELECT * FROM github_user_connection WHERE user_id = $1 FOR UPDATE;

-- name: UpsertGitHubUserConnection :one
INSERT INTO github_user_connection (
    user_id, github_user_id, login, avatar_url,
    access_token_encrypted, access_token_expires_at,
    refresh_token_encrypted, refresh_token_expires_at
) VALUES (
    $1, $2, $3, sqlc.narg('avatar_url'),
    $4, sqlc.narg('access_token_expires_at'),
    sqlc.narg('refresh_token_encrypted'), sqlc.narg('refresh_token_expires_at')
)
ON CONFLICT (user_id) DO UPDATE SET
    github_user_id = EXCLUDED.github_user_id,
    login = EXCLUDED.login,
    avatar_url = EXCLUDED.avatar_url,
    access_token_encrypted = EXCLUDED.access_token_encrypted,
    access_token_expires_at = EXCLUDED.access_token_expires_at,
    refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
    refresh_token_expires_at = EXCLUDED.refresh_token_expires_at,
    updated_at = now()
RETURNING *;

-- name: UpdateGitHubUserConnectionTokens :exec
UPDATE github_user_connection SET
    access_token_encrypted = $2,
    access_token_expires_at = sqlc.narg('access_token_expires_at'),
    refresh_token_encrypted = sqlc.narg('refresh_token_encrypted'),
    refresh_token_expires_at = sqlc.narg('refresh_token_expires_at'),
    updated_at = now()
WHERE user_id = $1;

-- name: DeleteGitHubUserConnection :exec
DELETE FROM github_user_connection WHERE user_id = $1;

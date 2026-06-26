-- =====================
-- User Composio Connection
-- =====================

-- name: UpsertUserComposioConnection :one
-- Idempotent on (user_id, connected_account_id): a duplicate callback for the
-- same connected account re-activates the row instead of inserting a second
-- one. connected_at is preserved on conflict (first-connect time); updated_at
-- moves so the reactivation is observable.
INSERT INTO user_composio_connection (
    user_id, toolkit_slug, auth_config_id, connected_account_id, composio_user_id, status
) VALUES (
    $1, $2, $3, $4, $5, 'active'
)
ON CONFLICT (user_id, connected_account_id) DO UPDATE SET
    toolkit_slug     = EXCLUDED.toolkit_slug,
    auth_config_id   = EXCLUDED.auth_config_id,
    composio_user_id = EXCLUDED.composio_user_id,
    status           = 'active',
    updated_at       = now()
RETURNING *;

-- name: ListActiveUserComposioConnections :many
SELECT * FROM user_composio_connection
WHERE user_id = $1 AND status = 'active'
ORDER BY connected_at DESC;

-- name: GetUserComposioConnection :one
-- Owner-scoped lookup: a connection can only be read by the user who owns it,
-- so one user cannot disconnect another's account by guessing the UUID.
SELECT * FROM user_composio_connection
WHERE id = $1 AND user_id = $2;

-- name: MarkUserComposioConnectionRevoked :exec
-- Idempotent: re-running on an already-revoked row is a no-op write. Scoped to
-- the owner for defense-in-depth.
UPDATE user_composio_connection
SET status = 'revoked', updated_at = now()
WHERE id = $1 AND user_id = $2;

-- name: CreateDaemonToken :one
-- owner_id binds the token to the runtime owner it was issued for (MUL-8154);
-- NULL only for Qoder-managed tokens.
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at, owner_id)
VALUES (@token_hash, @workspace_id, @daemon_id, @expires_at, sqlc.narg('owner_id'))
RETURNING *;

-- name: GetDaemonTokenByHash :one
SELECT * FROM daemon_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteDaemonTokensByWorkspaceAndDaemons :many
-- Deletes every daemon_token row matching the (workspace_id, daemon_id)
-- pairs implied by `daemon_ids`. Used by the member-revocation flow to
-- nuke tokens for all runtimes a leaving member owned in one shot.
-- Returns token_hash so the caller can invalidate auth.DaemonTokenCache
-- before the 10-minute TTL expires — without that invalidate, a daemon
-- can keep using its stale token until cache eviction even though the
-- DB row is gone.
DELETE FROM daemon_token
WHERE workspace_id = @workspace_id
  AND daemon_id = ANY(@daemon_ids::text[])
RETURNING token_hash;

-- name: DeleteExpiredDaemonTokens :exec
DELETE FROM daemon_token
WHERE expires_at <= now();

-- name: ListLegacyDaemonTokenOwners :many
-- Compatibility for Remote MCP daemon tokens minted before migration 588
-- (owner_id NULL). Up to v0.6.5 FinalizeTaskClaim inserted that token and the
-- task's task_token in one transaction, so both rows carry the same
-- created_at (now() is the transaction start). task_token.user_id is the
-- runtime owner the claim was authorized for, i.e. the token's issuing owner.
-- The task must still be live (task_token not deleted/expired) and bound to a
-- runtime of the token's daemon. Callers accept the result only when exactly
-- one owner matches; LIMIT 2 is enough to detect ambiguity.
SELECT DISTINCT tt.user_id
FROM task_token tt
JOIN agent_task_queue q ON q.id = tt.task_id
JOIN agent_runtime r ON r.id = q.runtime_id
WHERE tt.workspace_id = @workspace_id
  AND tt.created_at = @created_at
  AND tt.expires_at > now()
  AND r.workspace_id = @workspace_id
  AND r.daemon_id = @daemon_id::text
LIMIT 2;

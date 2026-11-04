-- name: CreateMetricWebhookToken :one
INSERT INTO metric_webhook_token (id, workspace_id, name, token_hash, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListMetricWebhookTokens :many
SELECT * FROM metric_webhook_token
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY created_at DESC, id;

-- name: GetActiveMetricWebhookTokenByHash :one
SELECT * FROM metric_webhook_token
WHERE token_hash = sqlc.arg('token_hash')::text AND revoked_at IS NULL;

-- name: RevokeMetricWebhookToken :one
UPDATE metric_webhook_token SET revoked_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND revoked_at IS NULL
RETURNING *;

-- name: TouchMetricWebhookToken :exec
UPDATE metric_webhook_token SET last_used_at = now() WHERE id = $1;

-- name: DeleteWorkspaceMetricTokens :exec
-- Workspace teardown only (spec 4.9 and the table rules): tokens are otherwise
-- kept as history, revoked rather than deleted.
DELETE FROM metric_webhook_token WHERE workspace_id = $1;

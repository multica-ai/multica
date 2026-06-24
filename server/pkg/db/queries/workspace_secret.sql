-- name: UpsertWorkspaceSecret :exec
INSERT INTO workspace_secret (workspace_id, name, encrypted_value, created_by, updated_at)
VALUES (sqlc.arg('workspace_id'), sqlc.arg('name'), sqlc.arg('encrypted_value'), sqlc.arg('created_by'), now())
ON CONFLICT (workspace_id, name)
DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value, updated_at = now();

-- name: GetWorkspaceSecret :one
SELECT *
FROM workspace_secret
WHERE workspace_id = sqlc.arg('workspace_id') AND name = sqlc.arg('name');

-- name: DeleteWorkspaceSecret :exec
DELETE FROM workspace_secret
WHERE workspace_id = sqlc.arg('workspace_id') AND name = sqlc.arg('name');

-- name: ListWorkspaceSecretNames :many
SELECT name, created_by, created_at, updated_at
FROM workspace_secret
WHERE workspace_id = sqlc.arg('workspace_id')
ORDER BY name;

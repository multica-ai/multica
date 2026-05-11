-- name: ListCerebroAccounts :many
SELECT id, workspace_id, provider, login_identity, created_at, updated_at
FROM cerebro_account
WHERE workspace_id = $1
ORDER BY provider ASC, lower(login_identity) ASC, created_at ASC;

-- name: GetCerebroAccount :one
SELECT id, workspace_id, provider, login_identity, created_at, updated_at
FROM cerebro_account
WHERE id = $1;

-- name: CreateCerebroAccount :one
INSERT INTO cerebro_account (workspace_id, provider, login_identity)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, provider, login_identity, created_at, updated_at;

-- name: DeleteCerebroAccount :exec
DELETE FROM cerebro_account
WHERE id = $1;

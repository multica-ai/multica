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

-- name: UpsertCerebroAccount :one
INSERT INTO cerebro_account (workspace_id, provider, login_identity)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, provider, login_identity)
DO UPDATE SET updated_at = now()
RETURNING id, workspace_id, provider, login_identity, created_at, updated_at;

-- name: DeleteCerebroAccount :exec
DELETE FROM cerebro_account
WHERE id = $1;

-- name: ListCerebroAccountsWithAvailability :many
SELECT
    ca.id,
    ca.workspace_id,
    ca.provider,
    ca.login_identity,
    ca.created_at,
    ca.updated_at,
    COUNT(ar.id)::int                                                                         AS runtime_count,
    COUNT(ar.id) FILTER (WHERE ar.paused_at IS NULL)::int                                    AS available_runtime_count,
    (MIN(ar.unpause_at) FILTER (WHERE ar.paused_at IS NOT NULL AND ar.unpause_at IS NOT NULL))::timestamptz  AS nearest_unpause_at
FROM cerebro_account ca
LEFT JOIN agent_runtime ar
    ON  ar.current_account_id = ca.id
    AND ar.workspace_id       = ca.workspace_id
WHERE ca.workspace_id = $1
GROUP BY ca.id
ORDER BY
    COUNT(ar.id) FILTER (WHERE ar.paused_at IS NULL) DESC,
    ca.provider ASC,
    lower(ca.login_identity) ASC;

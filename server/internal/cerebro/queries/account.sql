-- name: ListCerebroAccounts :many
SELECT id, workspace_id, provider, login_identity,
       usage_window_pct, throttled_until, extra_spend_on, paused_manual,
       created_at, updated_at
FROM cerebro_account
WHERE workspace_id = $1
ORDER BY provider ASC, lower(login_identity) ASC, created_at ASC;

-- name: GetCerebroAccount :one
SELECT id, workspace_id, provider, login_identity,
       usage_window_pct, throttled_until, extra_spend_on, paused_manual,
       created_at, updated_at
FROM cerebro_account
WHERE id = $1;

-- name: CreateCerebroAccount :one
INSERT INTO cerebro_account (workspace_id, provider, login_identity)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, provider, login_identity,
          usage_window_pct, throttled_until, extra_spend_on, paused_manual,
          created_at, updated_at;

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

-- name: UpdateCerebroAccountUsage :one
-- Daemon-driven usage telemetry. Each field is updated only when the
-- matching $..._set flag is true so the daemon can patch a single signal
-- (e.g. just throttled_until on a 429) without clobbering the other.
UPDATE cerebro_account
SET usage_window_pct = CASE WHEN sqlc.arg('usage_window_pct_set')::boolean
                            THEN sqlc.narg('usage_window_pct')::real
                            ELSE usage_window_pct END,
    throttled_until  = CASE WHEN sqlc.arg('throttled_until_set')::boolean
                            THEN sqlc.narg('throttled_until')::timestamptz
                            ELSE throttled_until END,
    updated_at       = now()
WHERE id = $1
RETURNING id, workspace_id, provider, login_identity,
          usage_window_pct, throttled_until, extra_spend_on, paused_manual,
          created_at, updated_at;

-- name: UpdateCerebroAccountControls :one
-- UI-driven control toggles. Same partial-update pattern as usage above.
UPDATE cerebro_account
SET extra_spend_on = CASE WHEN sqlc.arg('extra_spend_on_set')::boolean
                          THEN sqlc.arg('extra_spend_on')::boolean
                          ELSE extra_spend_on END,
    paused_manual  = CASE WHEN sqlc.arg('paused_manual_set')::boolean
                          THEN sqlc.arg('paused_manual')::boolean
                          ELSE paused_manual END,
    updated_at     = now()
WHERE id = $1
RETURNING id, workspace_id, provider, login_identity,
          usage_window_pct, throttled_until, extra_spend_on, paused_manual,
          created_at, updated_at;

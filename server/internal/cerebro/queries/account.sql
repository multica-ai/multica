-- name: ListCerebroAccounts :many
SELECT ca.id, ca.workspace_id, ca.provider, ca.login_identity,
       ca.usage_window_pct, ca.throttled_until, ca.extra_spend_on, ca.paused_manual,
       ca.created_at, ca.updated_at,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = ca.id
             AND catu.created_at >= now() - interval '5 hours'
       ) AS tokens_5h,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = ca.id
             AND catu.created_at >= now() - interval '7 days'
       ) AS tokens_7d
FROM cerebro_account ca
WHERE ca.workspace_id = $1
ORDER BY ca.provider ASC, lower(ca.login_identity) ASC, ca.created_at ASC;

-- name: GetCerebroAccount :one
SELECT ca.id, ca.workspace_id, ca.provider, ca.login_identity,
       ca.usage_window_pct, ca.throttled_until, ca.extra_spend_on, ca.paused_manual,
       ca.created_at, ca.updated_at,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = ca.id
             AND catu.created_at >= now() - interval '5 hours'
       ) AS tokens_5h,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = ca.id
             AND catu.created_at >= now() - interval '7 days'
       ) AS tokens_7d
FROM cerebro_account ca
WHERE ca.id = $1;

-- name: CreateCerebroAccount :one
INSERT INTO cerebro_account (workspace_id, provider, login_identity)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, provider, login_identity,
          usage_window_pct, throttled_until, extra_spend_on, paused_manual,
          created_at, updated_at,
          0::bigint AS tokens_5h,
          0::bigint AS tokens_7d;

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
    ca.usage_window_pct,
    ca.throttled_until,
    ca.extra_spend_on,
    ca.paused_manual,
    ca.created_at,
    ca.updated_at,
    COALESCE(t5.tokens, 0)::bigint                                                              AS tokens_5h,
    COALESCE(t7.tokens, 0)::bigint                                                              AS tokens_7d,
    COUNT(ar.id)::int                                                                         AS runtime_count,
    COUNT(ar.id) FILTER (WHERE ar.paused_at IS NULL)::int                                    AS available_runtime_count,
    (MIN(ar.unpause_at) FILTER (WHERE ar.paused_at IS NOT NULL AND ar.unpause_at IS NOT NULL))::timestamptz  AS nearest_unpause_at
FROM cerebro_account ca
LEFT JOIN LATERAL (
    SELECT SUM(catu.tokens)::bigint AS tokens
    FROM cerebro_account_token_usage catu
    WHERE catu.account_id = ca.id
      AND catu.created_at >= now() - interval '5 hours'
) t5 ON true
LEFT JOIN LATERAL (
    SELECT SUM(catu.tokens)::bigint AS tokens
    FROM cerebro_account_token_usage catu
    WHERE catu.account_id = ca.id
      AND catu.created_at >= now() - interval '7 days'
) t7 ON true
LEFT JOIN agent_runtime ar
    ON  ar.current_account_id = ca.id
    AND ar.workspace_id       = ca.workspace_id
WHERE ca.workspace_id = $1
GROUP BY ca.id, COALESCE(t5.tokens, 0), COALESCE(t7.tokens, 0)
ORDER BY
    COUNT(ar.id) FILTER (WHERE ar.paused_at IS NULL) DESC,
    ca.provider ASC,
    lower(ca.login_identity) ASC;

-- name: UpdateCerebroAccountUsage :one
-- Daemon-driven usage telemetry. Each field is updated only when the
-- matching $..._set flag is true so the daemon can patch a single signal
-- (e.g. just throttled_until on a 429) without clobbering the other.
WITH updated AS (
UPDATE cerebro_account
SET usage_window_pct = CASE WHEN sqlc.arg('usage_window_pct_set')::boolean
                            THEN sqlc.narg('usage_window_pct')::real
                            ELSE usage_window_pct END,
    throttled_until  = CASE WHEN sqlc.arg('throttled_until_set')::boolean
                            THEN sqlc.narg('throttled_until')::timestamptz
                            ELSE throttled_until END,
    updated_at       = now()
WHERE cerebro_account.id = $1
RETURNING id, workspace_id, provider, login_identity,
          usage_window_pct, throttled_until, extra_spend_on, paused_manual,
          created_at, updated_at
)
SELECT updated.*,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = updated.id
             AND catu.created_at >= now() - interval '5 hours'
       ) AS tokens_5h,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = updated.id
             AND catu.created_at >= now() - interval '7 days'
       ) AS tokens_7d
FROM updated;

-- name: InsertCerebroAccountTokenUsage :exec
INSERT INTO cerebro_account_token_usage (account_id, workspace_id, tokens)
VALUES ($1, $2, $3);

-- name: DeleteOldCerebroAccountTokenUsage :execrows
DELETE FROM cerebro_account_token_usage
WHERE created_at < now() - interval '8 days';

-- name: UpdateCerebroAccountControls :one
-- UI-driven control toggles. Same partial-update pattern as usage above.
WITH updated AS (
UPDATE cerebro_account
SET extra_spend_on = CASE WHEN sqlc.arg('extra_spend_on_set')::boolean
                          THEN sqlc.arg('extra_spend_on')::boolean
                          ELSE extra_spend_on END,
    paused_manual  = CASE WHEN sqlc.arg('paused_manual_set')::boolean
                          THEN sqlc.arg('paused_manual')::boolean
                          ELSE paused_manual END,
    updated_at     = now()
WHERE cerebro_account.id = $1
RETURNING id, workspace_id, provider, login_identity,
          usage_window_pct, throttled_until, extra_spend_on, paused_manual,
          created_at, updated_at
)
SELECT updated.*,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = updated.id
             AND catu.created_at >= now() - interval '5 hours'
       ) AS tokens_5h,
       (
           SELECT COALESCE(SUM(catu.tokens), 0)::bigint
           FROM cerebro_account_token_usage catu
           WHERE catu.account_id = updated.id
             AND catu.created_at >= now() - interval '7 days'
       ) AS tokens_7d
FROM updated;

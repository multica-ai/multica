-- name: UpsertCerebroExchangeRate :exec
-- Idempotent per (base, target): the daily fetch job overwrites the existing
-- rate rather than inserting a duplicate. fetched_at is supplied by the source;
-- updated_at is stamped on every write.
INSERT INTO cerebro_exchange_rates (base_currency, target_currency, rate, fetched_at, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (base_currency, target_currency) DO UPDATE
SET rate = EXCLUDED.rate,
    fetched_at = EXCLUDED.fetched_at,
    updated_at = now();

-- name: GetCerebroExchangeRate :one
SELECT base_currency, target_currency, rate, fetched_at, updated_at
FROM cerebro_exchange_rates
WHERE base_currency = $1 AND target_currency = $2;

-- name: ListCerebroExchangeRates :many
-- All cached rates for a base currency (USD), one row per target. Display reads
-- this once and converts client-side.
SELECT base_currency, target_currency, rate, fetched_at, updated_at
FROM cerebro_exchange_rates
WHERE base_currency = $1
ORDER BY target_currency ASC;

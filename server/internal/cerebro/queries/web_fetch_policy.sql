-- name: GetCerebroWebFetchPolicy :one
-- The per-workspace web_fetch policy. A missing row -> the caller applies the
-- seeded default (allow-list with the legacy hardcoded hosts), so pgx.ErrNoRows
-- is treated as "use default", not an error.
SELECT workspace_id, mode, host_rules, updated_at, updated_by
FROM cerebro_web_fetch_policy
WHERE workspace_id = $1;

-- name: UpsertCerebroWebFetchPolicy :one
-- Create or replace the workspace policy. One row per workspace; the whole
-- policy (mode + host_rules) is replaced on save.
INSERT INTO cerebro_web_fetch_policy (workspace_id, mode, host_rules, updated_at, updated_by)
VALUES ($1, $2, $3, now(), $4)
ON CONFLICT (workspace_id) DO UPDATE
SET mode = EXCLUDED.mode,
    host_rules = EXCLUDED.host_rules,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by
RETURNING workspace_id, mode, host_rules, updated_at, updated_by;

-- name: GetCerebroWorkspaceSettings :one
-- The per-workspace settings row. Missing row -> the handler applies the
-- default (USD), so callers treat pgx.ErrNoRows as "use default", not an error.
SELECT workspace_id, display_currency, updated_at, updated_by
FROM cerebro_workspace_settings
WHERE workspace_id = $1;

-- name: UpsertCerebroWorkspaceDisplayCurrency :exec
-- Set (or change) the workspace display currency. One row per workspace.
INSERT INTO cerebro_workspace_settings (workspace_id, display_currency, updated_at, updated_by)
VALUES ($1, $2, now(), $3)
ON CONFLICT (workspace_id) DO UPDATE
SET display_currency = EXCLUDED.display_currency,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by;

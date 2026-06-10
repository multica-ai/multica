-- name: GetCerebroWorkspaceSettings :one
-- The per-workspace settings row. Missing row -> the handler applies the
-- default (USD + the wakeup-limit defaults), so callers treat pgx.ErrNoRows as
-- "use default", not an error.
SELECT workspace_id, display_currency, wakeup_max_self_per_issue,
       wakeup_min_interval_minutes, updated_at, updated_by
FROM cerebro_workspace_settings
WHERE workspace_id = $1;

-- name: UpsertCerebroWorkspaceWakeupLimits :exec
-- TECH-3298: set (or change) the per-workspace self-wakeup limits. Only the two
-- wakeup columns are touched on conflict so an existing display_currency choice
-- is preserved.
INSERT INTO cerebro_workspace_settings (
    workspace_id, wakeup_max_self_per_issue, wakeup_min_interval_minutes,
    updated_at, updated_by
)
VALUES ($1, $2, $3, now(), $4)
ON CONFLICT (workspace_id) DO UPDATE
SET wakeup_max_self_per_issue = EXCLUDED.wakeup_max_self_per_issue,
    wakeup_min_interval_minutes = EXCLUDED.wakeup_min_interval_minutes,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by;

-- name: UpsertCerebroWorkspaceDisplayCurrency :exec
-- Set (or change) the workspace display currency. One row per workspace.
INSERT INTO cerebro_workspace_settings (workspace_id, display_currency, updated_at, updated_by)
VALUES ($1, $2, now(), $3)
ON CONFLICT (workspace_id) DO UPDATE
SET display_currency = EXCLUDED.display_currency,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by;

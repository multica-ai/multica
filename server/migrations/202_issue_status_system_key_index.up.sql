-- One row per (workspace, built-in system_key). This is the explicit arbiter
-- for the idempotent ON CONFLICT (workspace_id, system_key) DO NOTHING seed in
-- internal/issuestatus.Ensure, which makes seeding safe to call at workspace
-- creation and repeatedly during a rolling deploy. Naming the arbiter means any
-- OTHER unique violation surfaces loudly instead of silently dropping a
-- built-in. Custom statuses (system_key IS NULL) are excluded by the partial
-- predicate. Single-statement migration (CONCURRENTLY).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_status_workspace_system_key_uidx
    ON issue_status (workspace_id, system_key)
    WHERE system_key IS NOT NULL;

-- Disabled runtime profiles are retained for audit and migration, but only
-- enabled profiles should reserve a user-facing name. Build the replacement
-- before dropping the legacy full-table constraint in migration 343.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS runtime_profile_workspace_enabled_display_name_key
    ON runtime_profile (workspace_id, display_name)
    WHERE enabled = true;

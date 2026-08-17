-- Rebuild the legacy full uniqueness guard without blocking writes. Migration
-- 342's down direction attaches this index as the original constraint.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS runtime_profile_workspace_display_name_rollback_uidx
    ON runtime_profile (workspace_id, display_name);

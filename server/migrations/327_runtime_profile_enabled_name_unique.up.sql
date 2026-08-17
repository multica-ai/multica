-- Disabled runtime profiles are retained for audit/migration and must not keep
-- reserving the user-facing name needed by their enabled replacement.
ALTER TABLE runtime_profile
    DROP CONSTRAINT IF EXISTS runtime_profile_workspace_id_display_name_key;

CREATE UNIQUE INDEX runtime_profile_workspace_enabled_display_name_key
    ON runtime_profile (workspace_id, display_name)
    WHERE enabled = true;

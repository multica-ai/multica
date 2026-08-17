DROP INDEX IF EXISTS runtime_profile_workspace_enabled_display_name_key;

ALTER TABLE runtime_profile
    ADD CONSTRAINT runtime_profile_workspace_id_display_name_key
    UNIQUE (workspace_id, display_name);

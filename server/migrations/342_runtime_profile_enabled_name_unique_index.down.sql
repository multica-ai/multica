-- Migration 343's down direction rebuilt the legacy full unique index. Attach
-- it before removing the enabled-only replacement so rollback stays fail-closed.
ALTER TABLE runtime_profile
    ADD CONSTRAINT runtime_profile_workspace_id_display_name_key
    UNIQUE USING INDEX runtime_profile_workspace_display_name_rollback_uidx;

DROP INDEX IF EXISTS runtime_profile_workspace_enabled_display_name_key;

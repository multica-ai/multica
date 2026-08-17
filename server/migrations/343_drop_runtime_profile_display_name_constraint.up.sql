-- Migration 342 already installed the enabled-only uniqueness guard.
ALTER TABLE runtime_profile
    DROP CONSTRAINT IF EXISTS runtime_profile_workspace_id_display_name_key;

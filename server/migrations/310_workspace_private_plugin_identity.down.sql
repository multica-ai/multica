ALTER TABLE plugin_identity
    DROP CONSTRAINT IF EXISTS plugin_identity_scope_check,
    DROP COLUMN IF EXISTS owner_workspace_id;

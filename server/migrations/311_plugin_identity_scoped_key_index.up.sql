CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_identity_scoped_key
    ON plugin_identity (COALESCE(owner_workspace_id, '00000000-0000-0000-0000-000000000000'::uuid), plugin_key);

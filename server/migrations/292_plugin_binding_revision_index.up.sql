CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_binding_revision
    ON plugin_binding (installation_id, scope_type, scope_id, binding_revision);

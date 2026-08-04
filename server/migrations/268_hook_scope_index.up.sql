CREATE INDEX CONCURRENTLY idx_hook_scope_active ON hook (workspace_id, scope_type, scope_id) WHERE archived_at IS NULL;

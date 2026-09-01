CREATE INDEX CONCURRENTLY idx_marketplace_template_workspace ON marketplace_template (source_workspace_id, visibility, updated_at DESC);

CREATE INDEX CONCURRENTLY client_usage_daily_workspace_idx ON client_usage_daily (workspace_id) WHERE workspace_id IS NOT NULL;

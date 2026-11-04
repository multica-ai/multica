CREATE INDEX CONCURRENTLY IF NOT EXISTS metric_webhook_token_workspace_idx ON metric_webhook_token (workspace_id, created_at);

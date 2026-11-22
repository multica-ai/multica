CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_key_alias_key ON issue_key_alias (source_workspace_id,prefix,number);

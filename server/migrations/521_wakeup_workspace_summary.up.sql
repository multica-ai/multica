CREATE INDEX CONCURRENTLY idx_wakeup_workspace_enabled ON issue_wakeup(workspace_id,issue_id) WHERE enabled;

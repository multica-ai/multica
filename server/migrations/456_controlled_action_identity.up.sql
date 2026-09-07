CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS controlled_action_identity ON controlled_run (workspace_id, issue_id, action_id, (manifest->>'attempt'));

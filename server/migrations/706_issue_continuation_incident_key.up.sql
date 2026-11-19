CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_continuation_incident_key ON issue_continuation_incident (workspace_id, issue_id, chain_id, remainder_id, generation);

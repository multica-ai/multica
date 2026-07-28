-- Back reverse lookups ("relations where this issue is the target", i.e. the
-- derived "blocked by" view) and the delete-time cleanup that matches on
-- target_issue_id. Lead with workspace_id to stay covering for the
-- workspace-scoped queries. Single-statement migration: CREATE INDEX
-- CONCURRENTLY cannot share a file with other statements.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_relation_target
    ON issue_relation (workspace_id, target_issue_id);

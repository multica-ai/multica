-- The latest floor briefs of a workspace, and a floor's own briefs.
CREATE INDEX CONCURRENTLY IF NOT EXISTS brief_scope_created_idx ON brief (workspace_id, scope_type, scope_id, created_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_proposal_workspace_state_created ON proposal (workspace_id, state, created_at DESC);

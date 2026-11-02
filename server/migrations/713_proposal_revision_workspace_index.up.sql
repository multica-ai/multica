-- Reads of one proposal's history, scoped to its workspace.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_proposal_revision_workspace_proposal ON proposal_revision (workspace_id, proposal_id);

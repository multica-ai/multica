CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_proposal_squad_state ON proposal (squad_id, state);

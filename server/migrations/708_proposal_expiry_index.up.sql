-- The daily expiry job reads pending proposals whose expires_at has passed.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_proposal_pending_expires ON proposal (expires_at) WHERE state = 'pending';

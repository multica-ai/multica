-- Creation opportunistically prunes old sessions. The expiry index keeps that
-- cleanup bounded as the table grows. Single-statement + CONCURRENTLY follows
-- the production migration contract.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_setup_token_expiry
    ON setup_token (expires_at);

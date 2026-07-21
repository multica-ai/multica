-- Token redemption is a hash lookup. Keep this as the migration's only
-- statement so PostgreSQL can build the uniqueness guarantee without blocking
-- writes on a deployment that already accumulated setup sessions.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_setup_token_hash
    ON setup_token (token_hash);

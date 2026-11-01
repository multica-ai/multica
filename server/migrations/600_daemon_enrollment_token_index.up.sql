CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS daemon_enrollment_token_hash_idx ON daemon_enrollment (token_hash);

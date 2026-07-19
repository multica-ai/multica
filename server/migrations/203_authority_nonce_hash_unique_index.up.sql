CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_authority_nonce_nonce_hash ON authority_nonce(nonce_hash);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_qianwen_invocation_nonce_installation_nonce
    ON qianwen_invocation_nonce (installation_id, nonce_digest);

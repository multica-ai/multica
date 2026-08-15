CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_qianwen_pairing_attempt_installation_time
    ON qianwen_pairing_attempt (installation_id, attempted_at DESC, identity_digest);

-- Single statement: a spoken code must identify exactly one pending Multica
-- user within an installation. The service retries the negligible random
-- collision instead of allowing an ambiguous redeem later.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_qianwen_pairing_code_installation_code
    ON qianwen_pairing_code (installation_id, code_digest);

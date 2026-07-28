-- Intentionally deferred to migration 232's table rollback. On a clean install
-- dropping provider_failover_handoff removes this index; on HCX the legacy
-- 225/226 ledger owns the existing index and 232 preserves the table.
SELECT 1;

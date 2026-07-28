-- Intentionally deferred to migration 237's table rollback. On a clean install
-- dropping control_plane_effect_ledger removes this index; on HCX the legacy
-- 229/230 ledger owns the existing index and 237 preserves the table.
SELECT 1;

-- Backfill is intentionally irreversible: setting onboarded_at back to NULL
-- would re-introduce the dirty state we just cleaned up. This down migration
-- is a no-op so the migration can be ratcheted forward only.
SELECT 1;

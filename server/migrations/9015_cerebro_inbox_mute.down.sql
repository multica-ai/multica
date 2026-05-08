DROP INDEX IF EXISTS idx_inbox_muted_until;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS muted_until;

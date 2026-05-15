-- CEREBRO-PATCH(migration-085-squad-archive): make upstream migration idempotent for fork deploy retries.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ;
ALTER TABLE squad ADD COLUMN IF NOT EXISTS archived_by UUID;

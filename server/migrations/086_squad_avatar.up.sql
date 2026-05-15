-- CEREBRO-PATCH(migration-086-squad-avatar): make upstream migration idempotent for fork deploy retries.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS avatar_url TEXT;

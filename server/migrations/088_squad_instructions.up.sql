-- CEREBRO-PATCH(migration-088-squad-instructions): make upstream migration idempotent for fork deploy retries.
ALTER TABLE squad ADD COLUMN IF NOT EXISTS instructions TEXT NOT NULL DEFAULT '';

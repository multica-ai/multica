-- CEREBRO-PATCH(migration-idempotent-021-agent-instructions): cerebro modification of upstream file
ALTER TABLE agent ADD COLUMN IF NOT EXISTS instructions TEXT NOT NULL DEFAULT '';

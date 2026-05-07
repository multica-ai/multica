-- CEREBRO-PATCH(migration-idempotent-006-workspace-context): cerebro modification of upstream file
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS context TEXT;

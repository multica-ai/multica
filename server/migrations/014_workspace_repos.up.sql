-- CEREBRO-PATCH(migration-idempotent-014-workspace-repos): cerebro modification of upstream file
ALTER TABLE workspace ADD COLUMN IF NOT EXISTS repos JSONB NOT NULL DEFAULT '[]';

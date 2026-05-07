-- CEREBRO-PATCH(migration-idempotent-050-project-color): cerebro modification of upstream file
ALTER TABLE project ADD COLUMN IF NOT EXISTS color TEXT;

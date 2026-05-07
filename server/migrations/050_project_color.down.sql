-- CEREBRO-PATCH(migration-idempotent-050-project-color): cerebro modification of upstream file
ALTER TABLE project DROP COLUMN IF EXISTS color;

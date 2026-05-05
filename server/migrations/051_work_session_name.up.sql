-- CEREBRO-PATCH(migration-idempotent-051-work-session-name): cerebro modification of upstream file
ALTER TABLE work_session ADD COLUMN IF NOT EXISTS name TEXT;
ALTER TABLE work_session ADD COLUMN IF NOT EXISTS branch TEXT;

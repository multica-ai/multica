-- CEREBRO-PATCH(migration-idempotent-051-work-session-name): cerebro modification of upstream file
ALTER TABLE work_session DROP COLUMN IF EXISTS name;
ALTER TABLE work_session DROP COLUMN IF EXISTS branch;

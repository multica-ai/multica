DROP TABLE IF EXISTS cerebro_task_mandate;
ALTER TABLE cerebro_role_assignment DROP COLUMN IF EXISTS expires_at;
ALTER TABLE cerebro_role DROP COLUMN IF EXISTS permissions;
ALTER TABLE cerebro_role DROP COLUMN IF EXISTS version;

-- Deliberately do not recreate retired access paths. Rollback restores the
-- schema additions only; resurrecting a bypass must require a reviewed migration.

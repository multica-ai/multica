DROP INDEX IF EXISTS idx_cerebro_role_workspace_active;

ALTER TABLE cerebro_role
    DROP COLUMN IF EXISTS archived_at;

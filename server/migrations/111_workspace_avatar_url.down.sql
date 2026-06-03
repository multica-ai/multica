-- CEREBRO-PATCH(workspace-avatar-url-migration): FIR-2580 rollback.
ALTER TABLE workspace DROP COLUMN IF EXISTS avatar_url;

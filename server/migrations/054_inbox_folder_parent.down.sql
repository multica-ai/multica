-- CEREBRO-PATCH(migration-idempotent-054-inbox-folder-parent): cerebro modification of upstream file
DROP INDEX IF EXISTS idx_inbox_folder_parent;
ALTER TABLE inbox_folder DROP COLUMN IF EXISTS parent_id;

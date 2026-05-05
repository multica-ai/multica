-- CEREBRO-PATCH(migration-idempotent-053-inbox-folders): cerebro modification of upstream file
DROP INDEX IF EXISTS idx_inbox_folder_membership_item;
DROP TABLE IF EXISTS inbox_folder_membership;
DROP INDEX IF EXISTS idx_inbox_folder_user_ws;
DROP TABLE IF EXISTS inbox_folder;

DROP INDEX IF EXISTS idx_cerebro_app_folder_id;
ALTER TABLE cerebro_app DROP COLUMN IF EXISTS folder_id;
DROP TABLE IF EXISTS cerebro_app_folder;


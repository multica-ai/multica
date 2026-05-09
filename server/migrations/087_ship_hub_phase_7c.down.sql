-- Reverse Phase 7c. Drop indexes before columns so the drops succeed
-- when no rows reference the indexed columns.
DROP INDEX IF EXISTS idx_ship_release_smoke_run_id;
DROP INDEX IF EXISTS idx_ship_release_merged_main_sha;

ALTER TABLE ship_release DROP COLUMN IF EXISTS merged_main_sha;
ALTER TABLE ship_release DROP COLUMN IF EXISTS qa_verified_by;
ALTER TABLE ship_release DROP COLUMN IF EXISTS qa_verified_at;
ALTER TABLE ship_release DROP COLUMN IF EXISTS smoke_completed_at;
ALTER TABLE ship_release DROP COLUMN IF EXISTS smoke_status;
ALTER TABLE ship_release DROP COLUMN IF EXISTS smoke_run_url;
ALTER TABLE ship_release DROP COLUMN IF EXISTS smoke_run_id;

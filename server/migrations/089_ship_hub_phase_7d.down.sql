-- Reverse Phase 7d. Drop indexes before columns / tables so the drops
-- succeed cleanly.
DROP INDEX IF EXISTS idx_ship_release_production_main_sha;
DROP INDEX IF EXISTS idx_ship_release_health_status_workspace;

DROP TABLE IF EXISTS ship_release_health;

ALTER TABLE ship_release_pull_request DROP COLUMN IF EXISTS revert_error;
ALTER TABLE ship_release_pull_request DROP COLUMN IF EXISTS revert_pr_url;
ALTER TABLE ship_release_pull_request DROP COLUMN IF EXISTS revert_pr_number;
ALTER TABLE ship_release_pull_request DROP COLUMN IF EXISTS revert_state;
DROP TYPE IF EXISTS pr_revert_state;

ALTER TABLE ship_release DROP COLUMN IF EXISTS rolled_back_completed_at;
ALTER TABLE ship_release DROP COLUMN IF EXISTS rolled_back_by;
ALTER TABLE ship_release DROP COLUMN IF EXISTS production_main_sha;
ALTER TABLE ship_release DROP COLUMN IF EXISTS promoted_by;

-- Reverse 086_ship_hub_phase_7b.up.sql. Drop in dependency order: index
-- (depends on column), column (depends on enum), enum, then release-side
-- columns.
DROP INDEX IF EXISTS idx_ship_release_pr_queued;

ALTER TABLE ship_release_pull_request DROP COLUMN IF EXISTS merge_state;

DROP TYPE IF EXISTS pr_merge_state;

ALTER TABLE ship_release DROP CONSTRAINT IF EXISTS ship_release_merge_method_check;
ALTER TABLE ship_release DROP COLUMN IF EXISTS merge_method;

ALTER TABLE ship_release DROP COLUMN IF EXISTS merge_paused;

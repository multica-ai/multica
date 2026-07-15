DROP INDEX IF EXISTS idx_autopilot_paused_by_space;
DROP INDEX IF EXISTS idx_squad_archived_by_space;
ALTER TABLE autopilot
    DROP COLUMN IF EXISTS status_before_space_archive,
    DROP COLUMN IF EXISTS paused_by_space_at;
ALTER TABLE squad DROP COLUMN IF EXISTS archived_by_space_at;

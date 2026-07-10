DROP INDEX IF EXISTS idx_squad_workspace_space;
DROP TRIGGER IF EXISTS trg_squad_default_space ON squad;
DROP FUNCTION IF EXISTS set_squad_default_space();
ALTER TABLE squad DROP CONSTRAINT IF EXISTS squad_workspace_space_fk;
ALTER TABLE squad DROP CONSTRAINT IF EXISTS squad_workspace_id_id_unique;
-- Split clones intentionally remain ordinary Workspace Squads on rollback;
-- merging them would discard valid Issue/Autopilot assignments.
ALTER TABLE squad DROP COLUMN IF EXISTS space_id;
